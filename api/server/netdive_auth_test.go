package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/skydive-project/skydive/common"
	"github.com/skydive-project/skydive/config"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	etcd "go.etcd.io/etcd/client/v2"
)

// RBAC is process-global and has no reset API. Isolate the real Casbin model
// and bundled policies from existing handler tests that run without RBAC.
func TestNetdiveAPIAuthentication(t *testing.T) {
	const childEnvironment = "NETDIVE_API_AUTH_TEST_CHILD"
	if os.Getenv(childEnvironment) != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestNetdiveAPIAuthentication$", "-test.v")
		command.Env = append(os.Environ(), childEnvironment+"=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("authentication/RBAC route tests failed: %v\n%s", err, output)
		}
		t.Logf("%s", output)
		return
	}

	config.Set("rbac.policy", []string{
		"g, auth-admin, admin", "g, auth-reader, guest",
		"p, auth-operator, mold-credentials, write, allow",
		"p, auth-console, node, write, allow",
	})
	if err := config.InitRBAC(netdiveAuthPolicyStore{}); err != nil {
		t.Fatal(err)
	}
	provider := shttp.NewHtpasswdMapProvider(map[string]string{
		"auth-admin": "password", "auth-reader": "password", "auth-denied": "password",
		"auth-operator": "password", "auth-console": "password",
	})
	backend, err := shttp.NewBasicAuthenticationBackend("basic", provider.SecretProvider(), "guest")
	if err != nil {
		t.Fatal(err)
	}
	var upstreamCalls int32
	mold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("command") == moldKubernetesListCommand {
			_, _ = w.Write([]byte(`{"listkubernetesclustersresponse":{"count":0,"kubernetescluster":[]}}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer mold.Close()
	directory := t.TempDir()
	config.Set("mold.api.endpoint", mold.URL)
	config.Set("mold.db.propertiesFile", filepath.Join(directory, "missing-db.properties"))
	config.Set("mold.console.enabled", false)
	config.Set("analyzer.topology.k8s.config_file", filepath.Join(directory, "selected.kubeconfig"))
	config.Set("mold.kubernetes.stateFile", filepath.Join(directory, "selected-cluster.json"))
	common.SetMoldAPICredentialsStore(kubernetesListTestCredentials{})
	defer common.SetMoldAPICredentialsStore(nil)

	starts, stops := 0, 0
	registerAPIs := func(backend shttp.AuthenticationBackend) *shttp.Server {
		server := &shttp.Server{Router: mux.NewRouter()}
		RegisterMoldKubernetesAPI(server, backend, func() error { starts++; return nil }, func() { stops++ }, func() bool { return false })
		RegisterVmNameMapAPI(server, backend, func() map[string]string { return map[string]string{"vm-id": "VM name"} })
		RegisterVMNetworkMapAPI(server, backend, func() map[string][]common.VMNetworkInfo { return map[string][]common.VMNetworkInfo{} }, 10*time.Second)
		RegisterVMDetailMapAPI(server, backend, func() map[string]common.VMDetailInfo { return map[string]common.VMDetailInfo{} }, 10*time.Second)
		RegisterMoldHostDetailAPI(server, backend)
		RegisterMoldManagementServerAPI(server, backend)
		RegisterMoldVMConsoleAPI(server, backend)
		RegisterWallHostTrendAPI(server, backend)
		return server
	}
	server := registerAPIs(backend)

	type endpoint struct {
		method, path, permission string
		allowedStatus            int
	}
	endpoints := []endpoint{
		{"GET", "/api/mold/kubernetes-clusters", "read", 200},
		{"POST", "/api/mold/kubernetes-clusters/select", "control", 400},
		{"POST", "/api/mold/kubernetes-clusters/disable", "control", 200},
		{"POST", "/api/mold/kubernetes-clusters/test", "control", 200},
		{"GET", "/api/mold/kubernetes-clusters/summary", "read", 400},
		{"GET", "/api/mold/kubernetes-clusters/nodes/detail", "read", 400},
		{"GET", "/api/mold/kubernetes-clusters/namespaces/detail", "read", 400},
		{"GET", "/api/mold/kubernetes-clusters/pods/detail", "read", 400},
		{"GET", "/api/mold/kubernetes-clusters/pods/logs", "read", 400},
		{"GET", "/api/mold/kubernetes-clusters/services/detail", "read", 400},
		{"GET", "/api/vm-name-map", "read", 200},
		{"GET", "/api/vm-network-map", "read", 200},
		{"GET", "/api/vm-detail-map", "read", 200},
		{"GET", "/api/mold/hosts/detail", "read", 400},
		{"GET", "/api/mold/system-vms/detail", "read", 400},
		{"GET", "/api/mold/management-servers", "read", 200},
		{"GET", "/api/mold/vmconsole", "console", 404},
		{"GET", "/api/wall/hosts/trend", "read", 400},
		{"GET", "/api/wall/vms/trend", "read", 400},
	}
	type credentials struct {
		name, username, transport, permission string
		status                                int
	}
	cases := []credentials{
		{name: "anonymous", status: 401},
		{name: "invalid token", transport: "invalid-token", status: 401},
		{name: "invalid cookie", transport: "invalid-cookie", status: 401},
		{name: "invalid password", transport: "invalid-password", status: 401},
		{name: "malformed Basic", transport: "malformed-basic", status: 401},
		{name: "authenticated without permission", username: "auth-denied", transport: "token", status: 403},
		{name: "reader token without cookie", username: "auth-reader", transport: "token", permission: "read"},
		{name: "collection operator", username: "auth-operator", transport: "basic", permission: "control"},
		{name: "console operator", username: "auth-console", transport: "token", permission: "console"},
		{name: "administrator cookie", username: "auth-admin", transport: "cookie", permission: "all"},
	}
	for _, endpoint := range endpoints {
		for _, test := range cases {
			t.Run(endpoint.method+" "+endpoint.path+"/"+test.name, func(t *testing.T) {
				request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`))
				token := base64.StdEncoding.EncodeToString([]byte(test.username + ":password"))
				switch test.transport {
				case "token":
					request.Header.Set("X-Auth-Token", token)
				case "basic":
					request.SetBasicAuth(test.username, "password")
				case "cookie":
					request.AddCookie(shttp.AuthCookie(token, "/"))
				case "invalid-token":
					request.Header.Set("X-Auth-Token", "not-a-valid-token")
				case "invalid-cookie":
					request.AddCookie(shttp.AuthCookie("not-a-valid-token", "/"))
				case "invalid-password":
					request.SetBasicAuth("auth-admin", "wrong-password")
				case "malformed-basic":
					request.Header.Set("Authorization", "Basic")
				}
				want := test.status
				if want == 0 {
					want = http.StatusForbidden
					if test.permission == "all" || test.permission == endpoint.permission {
						want = endpoint.allowedStatus
					}
				}
				beforeCalls, beforeStarts, beforeStops := atomic.LoadInt32(&upstreamCalls), starts, stops
				beforeState, _ := os.ReadFile(moldKubernetesStatePath())
				w := httptest.NewRecorder()
				server.Router.ServeHTTP(w, request)
				if w.Code != want {
					t.Fatalf("status=%d, want=%d, body=%s", w.Code, want, w.Body.String())
				}
				if want == 401 || want == 403 {
					afterState, _ := os.ReadFile(moldKubernetesStatePath())
					if atomic.LoadInt32(&upstreamCalls) != beforeCalls || starts != beforeStarts || stops != beforeStops || string(beforeState) != string(afterState) {
						t.Fatal("rejected request reached collection state/probe or upstream Mold API")
					}
				}
				if endpoint.path == "/api/vm-name-map" && want == 200 && strings.TrimSpace(w.Body.String()) != `{"vm-id":"VM name"}` {
					t.Fatalf("successful payload changed: %s", w.Body.String())
				}
			})
		}
	}

	t.Run("cookie issued by existing login authenticates unchanged browser requests", func(t *testing.T) {
		login := httptest.NewRecorder()
		if _, _, err := shttp.Authenticate(backend, login, "auth-reader", "password"); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("GET", "/api/vm-name-map", nil)
		for _, cookie := range login.Result().Cookies() {
			request.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.Router.ServeHTTP(w, request)
		if w.Code != 200 || request.Header.Get("X-Auth-Token") != "" {
			t.Fatalf("existing cookie-only login failed: %d", w.Code)
		}
	})

	t.Run("nil backend still fails closed", func(t *testing.T) {
		server := &shttp.Server{Router: mux.NewRouter()}
		called := false
		RegisterVmNameMapAPI(server, nil, func() map[string]string { called = true; return nil })
		for _, token := range []string{"", base64.StdEncoding.EncodeToString([]byte("auth-admin:password")), "fake-token"} {
			request := httptest.NewRequest("GET", "/api/vm-name-map", nil)
			request.Header.Set("X-Auth-Token", token)
			w := httptest.NewRecorder()
			server.Router.ServeHTTP(w, request)
			if w.Code != 401 || called {
				t.Fatalf("unconfigured authentication accepted: status=%d, handler=%t", w.Code, called)
			}
		}
	})

	t.Run("configured noauth preserves all existing login-free routes", func(t *testing.T) {
		server := registerAPIs(shttp.NewNoAuthenticationBackend())
		for _, endpoint := range endpoints {
			t.Run(endpoint.method+" "+endpoint.path, func(t *testing.T) {
				request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`))
				w := httptest.NewRecorder()
				server.Router.ServeHTTP(w, request)
				if w.Code != endpoint.allowedStatus {
					t.Fatalf("existing login-free request changed: status=%d, want=%d, body=%s", w.Code, endpoint.allowedStatus, w.Body.String())
				}
			})
		}
	})

	t.Run("route methods do not change", func(t *testing.T) {
		for _, endpoint := range endpoints {
			method := "DELETE"
			w := httptest.NewRecorder()
			server.Router.ServeHTTP(w, httptest.NewRequest(method, endpoint.path, nil))
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s returned %d", method, endpoint.path, w.Code)
			}
		}
	})

	t.Run("authorized collection selection keeps existing saved config and probe behavior", func(t *testing.T) {
		selectionMold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Query().Get("command") {
			case moldKubernetesListCommand:
				_, _ = w.Write([]byte(`{"listkubernetesclustersresponse":{"kubernetescluster":[{"id":"cluster","name":"Cluster","state":"Running"}]}}`))
			case moldKubernetesConfigCommand:
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"getkubernetesclusterconfigresponse": map[string]string{"configdata": "existing-kubeconfig-content"}})
			default:
				t.Errorf("unexpected upstream command")
			}
		}))
		defer selectionMold.Close()
		config.Set("mold.api.endpoint", selectionMold.URL)
		request := httptest.NewRequest("POST", "/api/mold/kubernetes-clusters/select", strings.NewReader(`{"id":"cluster"}`))
		request.SetBasicAuth("auth-operator", "password")
		w := httptest.NewRecorder()
		server.Router.ServeHTTP(w, request)
		var result moldKubernetesStatusResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || !result.OK || !result.ProbeRunning || starts != 1 {
			t.Fatalf("authorized selection changed: status=%d body=%s error=%v starts=%d", w.Code, w.Body.String(), err, starts)
		}
		data, err := os.ReadFile(moldKubernetesKubeconfigPath())
		if err != nil || string(data) != "existing-kubeconfig-content" {
			t.Fatal("existing kubeconfig save flow changed", err)
		}
		selection, err := readMoldKubernetesSelection()
		if err != nil || !selection.Enabled || selection.ClusterID != "cluster" {
			t.Fatal("existing selection state changed", err)
		}
	})
}

// Only the policy API is faked; token validation and Casbin enforcement use
// the production authentication implementation, model and bundled policies.
type netdiveAuthPolicyStore struct{ etcd.KeysAPI }

func (netdiveAuthPolicyStore) Get(context.Context, string, *etcd.GetOptions) (*etcd.Response, error) {
	return &etcd.Response{Node: &etcd.Node{Value: ""}}, nil
}

func (netdiveAuthPolicyStore) Watcher(string, *etcd.WatcherOptions) etcd.Watcher {
	return netdiveAuthPolicyWatcher{}
}

type netdiveAuthPolicyWatcher struct{}

func (netdiveAuthPolicyWatcher) Next(context.Context) (*etcd.Response, error) {
	return nil, errors.New("test policy store does not watch external etcd")
}
