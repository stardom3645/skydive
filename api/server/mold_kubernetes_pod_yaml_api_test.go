package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	auth "github.com/abbot/go-http-auth"
	"github.com/gorilla/mux"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

func TestKubernetesPodYAMLCurrentIdentityRedactionAndNoMutation(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "workloads", UID: types.UID("pod-uid"), ResourceVersion: "10",
		Labels: map[string]string{"app": "web"}, Annotations: map[string]string{"kubectl.kubernetes.io/last-applied-configuration": `{"token":"annotation-secret"}`},
		ManagedFields: []metav1.ManagedFieldsEntry{{Manager: "test-field-manager", APIVersion: "v1"}}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "example/web:1",
			Env:  []corev1.EnvVar{{Name: "PRIVATE_ENV", Value: "literal-secret"}, {Name: "REFERENCE_ENV", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "private-secret-ref"}, Key: "private-secret-key"}}}},
			Args: []string{"--password=argument-secret"}, LivenessProbe: &corev1.Probe{Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{HTTPHeaders: []corev1.HTTPHeader{{Name: "Authorization", Value: "header-secret"}}}}}}},
			InitContainers:      []corev1.Container{{Name: "init", Env: []corev1.EnvVar{{Name: "INIT", Value: "init-secret"}}}},
			EphemeralContainers: []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug", Command: []string{"ephemeral-secret"}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	original, _ := json.Marshal(pod)
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/v1/namespaces/workloads/pods/web" {
			t.Errorf("unexpected operation %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pod)
	}))
	defer api.Close()
	client, _ := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
	request := httptest.NewRequest("GET", "/?id=cluster&uid=pod-uid&namespace=workloads&name=web", nil)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		serveKubernetesPodYAML(w, request, client)
		var result kubernetesYAMLResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || result.UID != "pod-uid" || !result.Redacted || result.FetchedAt.IsZero() || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("unexpected response %d %#v", w.Code, result)
		}
		for _, secret := range []string{"literal-secret", "annotation-secret", "argument-secret", "header-secret", "init-secret", "ephemeral-secret"} {
			if strings.Contains(result.YAML, secret) {
				t.Fatalf("leaked %s", secret)
			}
		}
		for _, reference := range []string{"PRIVATE_ENV", "REFERENCE_ENV", "private-secret-ref", "private-secret-key"} {
			if !strings.Contains(result.YAML, reference) {
				t.Fatalf("reference or environment name hidden: %s", reference)
			}
		}
		if strings.Contains(result.YAML, "managedFields") || strings.Contains(result.YAML, "test-field-manager") {
			t.Fatal("managedFields bookkeeping exposed")
		}
		var decoded map[string]interface{}
		if err := yaml.Unmarshal([]byte(result.YAML), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["kind"] != "Pod" || decoded["apiVersion"] != "v1" || !strings.Contains(result.YAML, "phase: Running") || !strings.Contains(result.YAML, "image: example/web:1") || !strings.Contains(result.YAML, "[REDACTED]") {
			t.Fatal("lost current resource structure")
		}
	}
	after, _ := json.Marshal(pod)
	if string(original) != string(after) || calls != 2 {
		t.Fatalf("mutated source or cached request: calls=%d", calls)
	}
}

type kubernetesYAMLDenyAuthentication struct {
	shttp.NoAuthenticationBackend
	calls int
}

func (backend *kubernetesYAMLDenyAuthentication) Wrap(_ auth.AuthenticatedHandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		backend.calls++
		http.Error(w, "authentication required", http.StatusUnauthorized)
	}
}

func TestKubernetesPodYAMLRouteRequiresConfiguredAuthenticationAndIsReadOnly(t *testing.T) {
	server := &shttp.Server{Router: mux.NewRouter()}
	backend := &kubernetesYAMLDenyAuthentication{}
	RegisterKubernetesPodYAMLAPI(server, backend)
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		w := httptest.NewRecorder()
		server.Router.ServeHTTP(w, httptest.NewRequest(method, "/api/mold/kubernetes-clusters/pods/yaml", nil))
		expected := http.StatusMethodNotAllowed
		if method == "GET" {
			expected = http.StatusUnauthorized
		}
		if w.Code != expected {
			t.Fatalf("%s: expected %d, got %d", method, expected, w.Code)
		}
	}
	if backend.calls != 1 {
		t.Fatalf("authentication wrapper was bypassed: calls=%d", backend.calls)
	}
}

func TestKubernetesPodYAMLRejectsRecreatedPod(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "workloads", UID: types.UID("replacement")}})
	}))
	defer api.Close()
	client, _ := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
	w := httptest.NewRecorder()
	serveKubernetesPodYAML(w, httptest.NewRequest("GET", "/?id=c&uid=original&namespace=workloads&name=web", nil), client)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "POD_NOT_FOUND") || strings.Contains(w.Body.String(), "yaml") {
		t.Fatalf("wrong pod returned: %s", w.Body)
	}
}

func TestKubernetesPodYAMLValidationAndFailureClassification(t *testing.T) {
	for _, query := range []string{"", "id=c&uid=u", "id=c&uid=u&namespace=n", "uid=u&namespace=n&name=p"} {
		w := httptest.NewRecorder()
		handleKubernetesPodYAML(w, authenticatedManualPortMappingRequest("GET", "/?"+query, nil))
		if w.Code != 400 {
			t.Fatalf("invalid request accepted: %d", w.Code)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "web", nil), 403, "YAML_PERMISSION_DENIED"},
		{apierrors.NewUnauthorized("test"), 401, "KUBERNETES_AUTHENTICATION_FAILED"},
		{apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "web"), 404, "POD_NOT_FOUND"},
		{context.DeadlineExceeded, 504, "YAML_TIMEOUT"},
	} {
		w := httptest.NewRecorder()
		writeKubernetesYAMLRequestError(w, tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
			t.Fatalf("unexpected error %d %s", w.Code, w.Body)
		}
	}
}

func TestKubernetesPodYAMLRejectsOversizedDocumentAndPropagatesForbidden(t *testing.T) {
	for _, forbidden := range []bool{false, true} {
		t.Run(map[bool]string{false: "oversized", true: "forbidden"}[forbidden], func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if forbidden {
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "web", nil).ErrStatus)
					return
				}
				_ = json.NewEncoder(w).Encode(corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "workloads", UID: types.UID("pod-uid")},
					Status:     corev1.PodStatus{Message: strings.Repeat("x", kubernetesYAMLByteLimit+1)},
				})
			}))
			defer api.Close()
			client, _ := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
			w := httptest.NewRecorder()
			serveKubernetesPodYAML(w, httptest.NewRequest("GET", "/?id=c&uid=pod-uid&namespace=workloads&name=web", nil), client)
			expectedStatus, expectedCode := http.StatusRequestEntityTooLarge, "YAML_TOO_LARGE"
			if forbidden {
				expectedStatus, expectedCode = http.StatusForbidden, "YAML_PERMISSION_DENIED"
			}
			if w.Code != expectedStatus || !strings.Contains(w.Body.String(), expectedCode) || strings.Contains(w.Body.String(), `"yaml"`) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected error response: %d %s", w.Code, w.Body)
			}
		})
	}
}
