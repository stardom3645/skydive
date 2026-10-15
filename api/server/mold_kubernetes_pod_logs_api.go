package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const kubernetesPodLogByteLimit int64 = 1024 * 1024

type kubernetesPodLogsResponse struct {
	ClusterID string    `json:"clusterId"`
	UID       string    `json:"uid"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Container string    `json:"container"`
	Previous  bool      `json:"previous"`
	TailLines int64     `json:"tailLines"`
	Logs      string    `json:"logs"`
	Truncated bool      `json:"truncated"`
	FetchedAt time.Time `json:"fetchedAt"`
}

func writeKubernetesPodLogError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"code": code, "message": message})
}

func kubernetesPodLogOptions(r *http.Request) (*corev1.PodLogOptions, error) {
	query := r.URL.Query()
	if strings.TrimSpace(query.Get("id")) == "" || strings.TrimSpace(query.Get("uid")) == "" || strings.TrimSpace(query.Get("container")) == "" {
		return nil, errors.New("클러스터, 파드 UID, 컨테이너를 지정하세요.")
	}
	if query.Get("name") != "" && query.Get("namespace") == "" {
		return nil, errors.New("파드 이름과 네임스페이스를 함께 지정하세요.")
	}
	tail := int64(500)
	if value := query.Get("tailLines"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 1 || parsed > 2000 {
			return nil, errors.New("조회 줄 수는 1~2000 사이여야 합니다.")
		}
		tail = parsed
	}
	previous := false
	if value := query.Get("previous"); value != "" {
		if value != "true" && value != "false" {
			return nil, errors.New("이전 로그 조회 옵션이 올바르지 않습니다.")
		}
		previous = value == "true"
	}
	limit := kubernetesPodLogByteLimit
	return &corev1.PodLogOptions{Container: strings.TrimSpace(query.Get("container")), Previous: previous,
		TailLines: &tail, LimitBytes: &limit, Timestamps: true, Follow: false}, nil
}

func handleMoldKubernetesPodLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	options, err := kubernetesPodLogOptions(r)
	if err != nil {
		writeKubernetesPodLogError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	_, client, ctx, cancel, ok := kubernetesDetailRequest(w, r)
	if !ok {
		return
	}
	defer cancel()
	serveKubernetesPodLogs(w, r.WithContext(ctx), client, options)
}

// Query only the currently existing Pod and verify its UID, so a recreated Pod
// with the same name cannot supply logs for an old topology selection.
func serveKubernetesPodLogs(w http.ResponseWriter, r *http.Request, client kubernetes.Interface, options *corev1.PodLogOptions) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	query, ctx := r.URL.Query(), r.Context()
	uid, namespace, name := strings.TrimSpace(query.Get("uid")), strings.TrimSpace(query.Get("namespace")), strings.TrimSpace(query.Get("name"))
	var pod *corev1.Pod
	var err error
	if name != "" {
		pod, err = client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		var pods *corev1.PodList
		pods, err = client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err == nil {
			for i := range pods.Items {
				if string(pods.Items[i].UID) == uid {
					pod = &pods.Items[i]
					break
				}
			}
		}
	}
	if err != nil && !apierrors.IsNotFound(err) {
		writeKubernetesPodLogRequestError(w, err, false)
		return
	}
	if pod == nil || err != nil || string(pod.UID) != uid {
		writeKubernetesPodLogError(w, http.StatusNotFound, "POD_NOT_FOUND", "파드가 삭제되었거나 교체되어 로그를 조회할 수 없습니다.")
		return
	}
	validContainer := false
	for _, container := range append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...) {
		if container.Name == options.Container {
			validContainer = true
			break
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if container.Name == options.Container {
			validContainer = true
		}
	}
	if !validContainer {
		writeKubernetesPodLogError(w, http.StatusBadRequest, "CONTAINER_NOT_FOUND", "선택한 컨테이너가 이 파드에 없습니다.")
		return
	}
	stream, err := client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, options).Stream(ctx)
	if err != nil {
		writeKubernetesPodLogRequestError(w, err, options.Previous)
		return
	}
	defer stream.Close()
	content, err := io.ReadAll(io.LimitReader(stream, kubernetesPodLogByteLimit+1))
	if err != nil {
		writeKubernetesPodLogRequestError(w, err, options.Previous)
		return
	}
	truncated := int64(len(content)) >= kubernetesPodLogByteLimit
	if int64(len(content)) > kubernetesPodLogByteLimit {
		content = content[:kubernetesPodLogByteLimit]
	}
	writeJSON(w, kubernetesPodLogsResponse{ClusterID: query.Get("id"), UID: string(pod.UID), Namespace: pod.Namespace,
		Name: pod.Name, Container: options.Container, Previous: options.Previous, TailLines: *options.TailLines,
		Logs: strings.ToValidUTF8(string(content), "�"), Truncated: truncated, FetchedAt: time.Now().UTC()})
}

// A pods/log denial does not change the topology collection's health: listing
// resources and reading container logs can have different RBAC permissions.
func writeKubernetesPodLogRequestError(w http.ResponseWriter, err error, previous bool) {
	switch {
	case apierrors.IsForbidden(err):
		writeKubernetesPodLogError(w, http.StatusForbidden, "LOG_PERMISSION_DENIED", "컨테이너 로그 조회 권한이 없습니다. pods/log 읽기 권한을 확인하세요.")
	case apierrors.IsUnauthorized(err):
		writeKubernetesPodLogError(w, http.StatusUnauthorized, "KUBERNETES_AUTHENTICATION_FAILED", "Kubernetes 인증에 실패했습니다. 클러스터 연동을 확인하세요.")
	case errors.Is(err, context.DeadlineExceeded), apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		writeKubernetesPodLogError(w, http.StatusGatewayTimeout, "LOG_TIMEOUT", "로그 조회 시간이 초과되었습니다. 다시 조회하세요.")
	case apierrors.IsNotFound(err):
		writeKubernetesPodLogError(w, http.StatusNotFound, "LOG_NOT_FOUND", "현재 컨테이너의 로그를 찾을 수 없습니다.")
	case previous && apierrors.IsBadRequest(err):
		writeKubernetesPodLogError(w, http.StatusConflict, "PREVIOUS_LOG_UNAVAILABLE", "재시작 직전 컨테이너의 로그가 남아 있지 않습니다.")
	case apierrors.IsBadRequest(err):
		writeKubernetesPodLogError(w, http.StatusConflict, "CONTAINER_LOG_UNAVAILABLE", "컨테이너가 아직 시작되지 않았거나 로그를 제공할 수 없습니다.")
	default:
		writeKubernetesPodLogError(w, http.StatusBadGateway, "LOG_UNAVAILABLE", "컨테이너 로그를 조회하지 못했습니다. 잠시 후 다시 조회하세요.")
	}
}
