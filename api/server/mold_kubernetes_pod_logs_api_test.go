package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestKubernetesPodLogOptionsRejectInvalidRequests(t *testing.T) {
	for _, query := range []string{
		"id=c&uid=u", "uid=u&container=app", "id=c&container=app", "id=c&uid=u&container=app&name=web",
		"id=c&uid=u&container=app&tailLines=0", "id=c&uid=u&container=app&tailLines=2001",
		"id=c&uid=u&container=app&tailLines=-1", "id=c&uid=u&container=app&tailLines=NaN",
		"id=c&uid=u&container=app&previous=maybe",
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/mold/kubernetes-clusters/pods/logs?"+query, nil)
		w := httptest.NewRecorder()
		handleMoldKubernetesPodLogs(w, r)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_REQUEST") {
			t.Fatalf("query=%s status=%d body=%s", query, w.Code, w.Body)
		}
	}
	options, err := kubernetesPodLogOptions(httptest.NewRequest("GET", "/?id=c&uid=u&container=app", nil))
	if err != nil || *options.TailLines != 500 || *options.LimitBytes != kubernetesPodLogByteLimit || options.Follow || !options.Timestamps {
		t.Fatalf("unexpected defaults: %#v %v", options, err)
	}
}

func TestKubernetesPodLogsReadCurrentInitAndPrevious(t *testing.T) {
	for _, tc := range []struct {
		container string
		previous  bool
	}{{"app", false}, {"prepare", false}, {"debug", false}, {"app", true}} {
		t.Run(tc.container+map[bool]string{true: "-previous", false: "-current"}[tc.previous], func(t *testing.T) {
			var logRequests int
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/namespaces/workloads/pods/web" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "workloads", UID: types.UID("pod-uid")},
						Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}, InitContainers: []corev1.Container{{Name: "prepare"}},
							EphemeralContainers: []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug"}}}}})
					return
				}
				if r.URL.Path != "/api/v1/namespaces/workloads/pods/web/log" {
					t.Errorf("unexpected API path %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				logRequests++
				q := r.URL.Query()
				if q.Get("container") != tc.container || q.Get("tailLines") != "100" || q.Get("limitBytes") != "1048576" || q.Get("timestamps") != "true" || (q.Get("previous") == "true") != tc.previous || q.Get("follow") == "true" {
					t.Errorf("unexpected log options %s", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte("2026-10-14T10:00:00Z 실제 출력 <script>alert(1)</script>\n"))
			}))
			defer api.Close()
			client, err := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
			if err != nil {
				t.Fatal(err)
			}
			previous := map[bool]string{true: "true", false: "false"}[tc.previous]
			r := httptest.NewRequest("GET", "/?id=c&uid=pod-uid&namespace=workloads&name=web&container="+tc.container+"&previous="+previous+"&tailLines=100", nil)
			options, _ := kubernetesPodLogOptions(r)
			w := httptest.NewRecorder()
			serveKubernetesPodLogs(w, r, client, options)
			var result kubernetesPodLogsResponse
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || logRequests != 1 || result.Container != tc.container || result.Previous != tc.previous || result.UID != "pod-uid" || result.TailLines != 100 || !strings.Contains(result.Logs, "<script>") || result.FetchedAt.IsZero() || result.Truncated {
				t.Fatalf("unexpected logs status=%d requests=%d response=%#v", w.Code, logRequests, result)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("logs must not be cached")
			}
		})
	}
}

func TestKubernetesPodLogsRejectReplacedPodAndMissingContainer(t *testing.T) {
	for _, tc := range []struct{ uid, container, code string }{{"old-uid", "app", "POD_NOT_FOUND"}, {"current-uid", "missing", "CONTAINER_NOT_FOUND"}} {
		t.Run(tc.code, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/log") {
					t.Error("must not fetch logs for a mismatched Pod/container")
					http.Error(w, "unexpected log request", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "workloads", UID: types.UID("current-uid")}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}})
			}))
			defer api.Close()
			client, _ := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
			r := httptest.NewRequest("GET", "/?id=c&uid="+tc.uid+"&namespace=workloads&name=web&container="+tc.container, nil)
			options, _ := kubernetesPodLogOptions(r)
			w := httptest.NewRecorder()
			serveKubernetesPodLogs(w, r, client, options)
			if !strings.Contains(w.Body.String(), tc.code) || w.Code == 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
		})
	}
}

func TestKubernetesPodLogsFailuresAndBoundedResponse(t *testing.T) {
	for _, tc := range []struct {
		status   int
		reason   metav1.StatusReason
		previous bool
		code     string
		large    bool
	}{
		{403, metav1.StatusReasonForbidden, false, "LOG_PERMISSION_DENIED", false},
		{401, metav1.StatusReasonUnauthorized, false, "KUBERNETES_AUTHENTICATION_FAILED", false},
		{400, metav1.StatusReasonBadRequest, true, "PREVIOUS_LOG_UNAVAILABLE", false},
		{400, metav1.StatusReasonBadRequest, false, "CONTAINER_LOG_UNAVAILABLE", false},
		{404, metav1.StatusReasonNotFound, false, "LOG_NOT_FOUND", false},
		{504, metav1.StatusReasonTimeout, false, "LOG_TIMEOUT", false},
		{200, "", false, "", true},
	} {
		t.Run(tc.code+map[bool]string{true: "bounded", false: ""}[tc.large], func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/log") {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "workloads", UID: types.UID("uid")}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}})
					return
				}
				if tc.large {
					_, _ = w.Write([]byte(strings.Repeat("x", int(kubernetesPodLogByteLimit)+100)))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Status: "Failure", Code: int32(tc.status), Reason: tc.reason, Message: "upstream-secret-must-not-leak"})
			}))
			defer api.Close()
			client, _ := kubernetes.NewForConfig(&rest.Config{Host: api.URL})
			r := httptest.NewRequest("GET", "/?id=c&uid=uid&namespace=workloads&name=web&container=app&previous="+map[bool]string{true: "true", false: "false"}[tc.previous], nil)
			options, _ := kubernetesPodLogOptions(r)
			w := httptest.NewRecorder()
			serveKubernetesPodLogs(w, r, client, options)
			if tc.large {
				var result kubernetesPodLogsResponse
				_ = json.Unmarshal(w.Body.Bytes(), &result)
				if len(result.Logs) != int(kubernetesPodLogByteLimit) || !result.Truncated {
					t.Fatal("large logs must be bounded and marked truncated")
				}
			} else if w.Code < 400 || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "upstream-secret") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
		})
	}
}
