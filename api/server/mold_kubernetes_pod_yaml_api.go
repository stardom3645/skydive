package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	auth "github.com/abbot/go-http-auth"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/rbac"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

const kubernetesYAMLByteLimit = 2 * 1024 * 1024
const kubernetesYAMLRedacted = "[REDACTED]"

type kubernetesYAMLResponse struct {
	ClusterID string    `json:"clusterId"`
	UID       string    `json:"uid"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	YAML      string    `json:"yaml"`
	Redacted  bool      `json:"redacted"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// Reuse the authenticated topology-read policy; there is no write route.
func RegisterKubernetesPodYAMLAPI(server *shttp.Server, backend shttp.AuthenticationBackend) {
	server.RegisterRoutes([]shttp.Route{{Name: "KubernetesPodYAML", Method: "GET", Path: "/api/mold/kubernetes-clusters/pods/yaml", HandlerFunc: handleKubernetesPodYAML}}, backend)
}

func writeKubernetesYAMLError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"code": code, "message": message})
}

func handleKubernetesPodYAML(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	w.Header().Set("Cache-Control", "no-store")
	if !rbac.Enforce(r.Username, "topology", "read") {
		writeKubernetesYAMLError(w, http.StatusForbidden, "YAML_PERMISSION_DENIED", "리소스 YAML 조회 권한이 없습니다.")
		return
	}
	q := r.URL.Query()
	for _, key := range []string{"id", "uid", "namespace", "name"} {
		if strings.TrimSpace(q.Get(key)) == "" {
			writeKubernetesYAMLError(w, http.StatusBadRequest, "INVALID_REQUEST", "클러스터, 파드 UID, 네임스페이스와 이름을 지정하세요.")
			return
		}
	}
	_, client, ctx, cancel, ok := kubernetesDetailRequest(w, &r.Request)
	if !ok {
		return
	}
	defer cancel()
	serveKubernetesPodYAML(w, r.Request.WithContext(ctx), client)
}

func serveKubernetesPodYAML(w http.ResponseWriter, r *http.Request, client kubernetes.Interface) {
	w.Header().Set("Cache-Control", "no-store")
	q := r.URL.Query()
	pod, err := client.CoreV1().Pods(q.Get("namespace")).Get(r.Context(), q.Get("name"), metav1.GetOptions{})
	if err != nil {
		writeKubernetesYAMLRequestError(w, err)
		return
	}
	if string(pod.UID) != q.Get("uid") || pod.Namespace != q.Get("namespace") || pod.Name != q.Get("name") {
		writeKubernetesYAMLError(w, http.StatusNotFound, "POD_NOT_FOUND", "파드가 삭제되었거나 같은 이름의 다른 파드로 교체되었습니다.")
		return
	}
	// Only redact a deep copy. Never mutate the API object or shared informer data.
	current := pod.DeepCopy()
	current.APIVersion, current.Kind = "v1", "Pod"
	current.ManagedFields = nil // kubectl's default YAML also omits field-manager bookkeeping.
	encoded, err := json.Marshal(current)
	var object map[string]interface{}
	if err == nil {
		err = json.Unmarshal(encoded, &object)
	}
	if err != nil {
		writeKubernetesYAMLError(w, 502, "YAML_UNAVAILABLE", "현재 리소스 정보를 변환하지 못했습니다.")
		return
	}
	redacted := redactKubernetesYAML(object)
	encoded, err = json.Marshal(object)
	if err == nil {
		encoded, err = yaml.JSONToYAML(encoded)
	}
	if err != nil {
		writeKubernetesYAMLError(w, 502, "YAML_UNAVAILABLE", "YAML을 변환하지 못했습니다.")
		return
	}
	if len(encoded) > kubernetesYAMLByteLimit {
		writeKubernetesYAMLError(w, http.StatusRequestEntityTooLarge, "YAML_TOO_LARGE", "리소스 YAML이 표시 가능한 크기(2MiB)를 초과했습니다.")
		return
	}
	writeJSON(w, kubernetesYAMLResponse{ClusterID: q.Get("id"), UID: string(pod.UID), Namespace: pod.Namespace,
		Name: pod.Name, YAML: string(encoded), Redacted: redacted, FetchedAt: time.Now().UTC()})
}

// A read-only inspection failure does not change Pod or collection verdicts.
func writeKubernetesYAMLRequestError(w http.ResponseWriter, err error) {
	switch {
	case apierrors.IsForbidden(err):
		writeKubernetesYAMLError(w, 403, "YAML_PERMISSION_DENIED", "파드 YAML 조회 권한이 없습니다. pods 읽기 권한을 확인하세요.")
	case apierrors.IsUnauthorized(err):
		writeKubernetesYAMLError(w, 401, "KUBERNETES_AUTHENTICATION_FAILED", "Kubernetes 인증에 실패했습니다. 클러스터 연동을 확인하세요.")
	case apierrors.IsNotFound(err):
		writeKubernetesYAMLError(w, 404, "POD_NOT_FOUND", "현재 파드를 찾을 수 없습니다. 삭제 또는 교체 여부를 확인하세요.")
	case errors.Is(err, context.DeadlineExceeded), apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		writeKubernetesYAMLError(w, 504, "YAML_TIMEOUT", "YAML 조회 시간이 초과되었습니다. 잠시 후 다시 조회하세요.")
	default:
		writeKubernetesYAMLError(w, 502, "YAML_UNAVAILABLE", "현재 리소스 YAML을 조회하지 못했습니다. 잠시 후 다시 조회하세요.")
	}
}
