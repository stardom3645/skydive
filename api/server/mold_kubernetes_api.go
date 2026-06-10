package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/skydive-project/skydive/common"
	"github.com/skydive-project/skydive/config"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	moldKubernetesListCommand   = "listKubernetesClusters"
	moldKubernetesConfigCommand = "getKubernetesClusterConfig"
)

type moldKubernetesCluster struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	State             string `json:"state"`
	APIServer         string `json:"apiServer"`
	CollectionEnabled bool   `json:"collectionEnabled"`
	CollectionRunning bool   `json:"collectionRunning"`
}

type moldKubernetesSelection struct {
	Enabled     bool      `json:"enabled"`
	ClusterID   string    `json:"clusterId,omitempty"`
	ClusterName string    `json:"clusterName,omitempty"`
	APIServer   string    `json:"apiServer,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`
}

type moldKubernetesClustersResponse struct {
	Clusters       []moldKubernetesCluster `json:"clusters"`
	SelectedID     string                  `json:"selectedId,omitempty"`
	KubeconfigPath string                  `json:"kubeconfigPath"`
	RestartNeeded  bool                    `json:"restartNeeded"`
	ProbeRunning   bool                    `json:"probeRunning"`
}

type moldKubernetesSelectRequest struct {
	ID string `json:"id"`
}

type moldKubernetesTestRequest struct {
	ID string `json:"id"`
}

type moldKubernetesCheckResult struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	OK      bool   `json:"ok"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type moldKubernetesTestResponse struct {
	OK      bool                        `json:"ok"`
	Message string                      `json:"message,omitempty"`
	Checks  []moldKubernetesCheckResult `json:"checks"`
}

type moldKubernetesStatusResponse struct {
	OK             bool   `json:"ok"`
	Message        string `json:"message,omitempty"`
	KubeconfigPath string `json:"kubeconfigPath,omitempty"`
	ProbeRunning   bool   `json:"probeRunning"`
}

func handleMoldKubernetesClusters(w http.ResponseWriter, r *http.Request, isProbeRunning moldKubernetesProbeStatus, stopProbe moldKubernetesProbeStopper) {
	clusters, err := listMoldKubernetesClusters()
	if err != nil {
		writeMoldKubernetesError(w, err)
		return
	}

	selection, _ := readMoldKubernetesSelection()
	selectedID := ""
	if selection.Enabled && isMoldKubernetesSelectionCurrent(selection, clusters) {
		selectedID = selection.ClusterID
	} else if selection.Enabled {
		staleSelection := selection
		selection = moldKubernetesSelection{Enabled: false, UpdatedAt: time.Now().UTC()}
		_ = writeMoldKubernetesSelection(selection)
		_ = removeMoldKubernetesManagedKubeconfigs(staleSelection)
		if stopProbe != nil {
			stopProbe()
		}
	}
	probeRunning := false
	if isProbeRunning != nil {
		probeRunning = isProbeRunning()
	}
	for i := range clusters {
		clusters[i].CollectionEnabled = selection.Enabled && clusters[i].ID == selection.ClusterID
		clusters[i].CollectionRunning = clusters[i].CollectionEnabled && probeRunning
	}

	writeJSON(w, moldKubernetesClustersResponse{
		Clusters:       clusters,
		SelectedID:     selectedID,
		KubeconfigPath: moldKubernetesKubeconfigPath(),
		RestartNeeded:  false,
		ProbeRunning:   probeRunning,
	})
}

type moldKubernetesProbeStarter func() error
type moldKubernetesProbeStopper func()
type moldKubernetesProbeStatus func() bool

func handleMoldKubernetesSelect(w http.ResponseWriter, r *http.Request, startProbe moldKubernetesProbeStarter) {
	var req moldKubernetesSelectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		http.Error(w, "cluster id is required", http.StatusBadRequest)
		return
	}

	clusters, err := listMoldKubernetesClusters()
	if err != nil {
		writeMoldKubernetesError(w, err)
		return
	}
	var selected *moldKubernetesCluster
	for i := range clusters {
		if clusters[i].ID == req.ID {
			selected = &clusters[i]
			break
		}
	}
	if selected == nil {
		http.Error(w, "cluster not found", http.StatusNotFound)
		return
	}

	kubeconfig, err := getMoldKubernetesConfig(req.ID)
	if err != nil {
		writeMoldKubernetesError(w, err)
		return
	}
	clusterPath := moldKubernetesClusterKubeconfigPath(req.ID)
	if err := writeSecureFile(clusterPath, []byte(kubeconfig)); err != nil {
		http.Error(w, "failed to save kubeconfig", http.StatusInternalServerError)
		return
	}
	path := moldKubernetesKubeconfigPath()
	if path != clusterPath {
		if err := writeSecureFile(path, []byte(kubeconfig)); err != nil {
			http.Error(w, "failed to save active kubeconfig", http.StatusInternalServerError)
			return
		}
	}

	selection := moldKubernetesSelection{
		Enabled:     true,
		ClusterID:   selected.ID,
		ClusterName: selected.Name,
		APIServer:   selected.APIServer,
		UpdatedAt:   time.Now().UTC(),
	}
	if err := writeMoldKubernetesSelection(selection); err != nil {
		http.Error(w, "failed to save collection state", http.StatusInternalServerError)
		return
	}

	message := "kubeconfig saved. k8s probe started."
	probeRunning := false
	if startProbe != nil {
		if err := startProbe(); err != nil {
			message = "kubeconfig saved, but k8s probe start failed. check analyzer logs."
		} else {
			probeRunning = true
		}
	} else {
		message = "kubeconfig saved. restart analyzer to apply k8s probe if it is not running."
	}

	writeJSON(w, moldKubernetesStatusResponse{
		OK:             probeRunning || startProbe == nil,
		Message:        message,
		KubeconfigPath: path,
		ProbeRunning:   probeRunning,
	})
}

func handleMoldKubernetesDisable(w http.ResponseWriter, r *http.Request, stopProbe moldKubernetesProbeStopper) {
	previousSelection, _ := readMoldKubernetesSelection()
	selection := moldKubernetesSelection{Enabled: false, UpdatedAt: time.Now().UTC()}
	if err := writeMoldKubernetesSelection(selection); err != nil {
		http.Error(w, "failed to save collection state", http.StatusInternalServerError)
		return
	}
	if err := removeMoldKubernetesManagedKubeconfigs(previousSelection); err != nil {
		http.Error(w, "failed to remove kubeconfig", http.StatusInternalServerError)
		return
	}
	if stopProbe != nil {
		stopProbe()
	}
	writeJSON(w, moldKubernetesStatusResponse{OK: true, Message: "kubernetes collection disabled."})
}

func handleMoldKubernetesTest(w http.ResponseWriter, r *http.Request) {
	var req moldKubernetesTestRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	checks := make([]moldKubernetesCheckResult, 0, 8)
	addCheck := func(key, label string, ok bool, reason, message string) {
		checks = append(checks, moldKubernetesCheckResult{Key: key, Label: label, OK: ok, Reason: reason, Message: message})
	}
	fail := func(key, label string, err error, message string) {
		addCheck(key, label, false, sanitizeKubernetesTestError(err), message)
	}

	var kubeconfigData []byte
	if strings.TrimSpace(req.ID) != "" {
		configText, err := getMoldKubernetesConfig(req.ID)
		if err != nil {
			fail("kubeconfig", "kubeconfig 생성/읽기", err, "Mold에서 kubeconfig를 가져오지 못했습니다.")
			writeJSON(w, moldKubernetesTestResponse{OK: false, Message: "connection test failed", Checks: checks})
			return
		}
		kubeconfigData = []byte(configText)
	} else {
		path := moldKubernetesKubeconfigPath()
		data, err := os.ReadFile(path)
		if err != nil {
			fail("kubeconfig", "kubeconfig 생성/읽기", err, "저장된 kubeconfig를 읽지 못했습니다.")
			writeJSON(w, moldKubernetesTestResponse{OK: false, Message: "connection test failed", Checks: checks})
			return
		}
		kubeconfigData = data
	}

	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		fail("kubeconfig", "kubeconfig 생성/읽기", err, "kubeconfig 형식이 올바르지 않습니다.")
		writeJSON(w, moldKubernetesTestResponse{OK: false, Message: "connection test failed", Checks: checks})
		return
	}
	clientConfig.Timeout = 10 * time.Second
	addCheck("kubeconfig", "kubeconfig 생성/읽기", true, "", "kubeconfig를 읽었습니다.")
	addCheck("apiserver", "API Server 접근", true, "", "API Server 설정을 확인했습니다.")

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		fail("client", "API Server 접근", err, "Kubernetes client를 생성하지 못했습니다.")
		writeJSON(w, moldKubernetesTestResponse{OK: false, Message: "connection test failed", Checks: checks})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if version, err := clientset.Discovery().ServerVersion(); err != nil {
		fail("version", "/version 호출", err, "Kubernetes 버전 정보를 조회하지 못했습니다.")
	} else {
		addCheck("version", "/version 호출", true, "", fmt.Sprintf("%s", version.GitVersion))
	}
	if _, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("namespaces", "namespace 목록 조회", err, "namespace 목록을 조회하지 못했습니다.")
	} else {
		addCheck("namespaces", "namespace 목록 조회", true, "", "namespace 조회 권한을 확인했습니다.")
	}
	if _, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("nodes", "node 목록 조회", err, "node 목록을 조회하지 못했습니다.")
	} else {
		addCheck("nodes", "node 목록 조회", true, "", "node 조회 권한을 확인했습니다.")
	}
	if _, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("pods", "pod 목록 조회", err, "pod 목록을 조회하지 못했습니다.")
	} else {
		addCheck("pods", "pod 목록 조회", true, "", "pod 조회 권한을 확인했습니다.")
	}
	if _, err := clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("services", "service 목록 조회", err, "service 목록을 조회하지 못했습니다.")
	} else {
		addCheck("services", "service 목록 조회", true, "", "service 조회 권한을 확인했습니다.")
	}
	if _, err := clientset.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("networkpolicies", "networkpolicy 목록 조회", err, "networkpolicy 목록을 조회하지 못했습니다.")
	} else {
		addCheck("networkpolicies", "networkpolicy 목록 조회", true, "", "networkpolicy 조회 권한을 확인했습니다.")
	}

	ok := true
	for _, check := range checks {
		if !check.OK {
			ok = false
			break
		}
	}
	message := "connection test succeeded"
	if !ok {
		message = "connection test failed"
	}
	writeJSON(w, moldKubernetesTestResponse{OK: ok, Message: message, Checks: checks})
}

func listMoldKubernetesClusters() ([]moldKubernetesCluster, error) {
	body, _, err := requestMoldAPI(moldKubernetesListCommand, []apiParam{
		{Key: "command", Value: moldKubernetesListCommand},
		{Key: "response", Value: "json"},
	})
	if err != nil {
		return nil, err
	}
	return parseMoldKubernetesClusters(body)
}

func getMoldKubernetesConfig(clusterID string) (string, error) {
	body, _, err := requestMoldAPI(moldKubernetesConfigCommand, []apiParam{
		{Key: "command", Value: moldKubernetesConfigCommand},
		{Key: "response", Value: "json"},
		{Key: "id", Value: clusterID},
	})
	if err != nil {
		return "", err
	}
	configData := findStringByKey(body, "configdata")
	if strings.TrimSpace(configData) == "" {
		return "", newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes kubeconfig 응답이 비어 있습니다.", nil)
	}
	return configData, nil
}

func requestMoldAPI(command string, params []apiParam) ([]byte, int, error) {
	apiCfg := common.GetMoldAPIConfig()
	if apiCfg.Endpoint == "" {
		return nil, 0, newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API endpoint 설정이 비어 있습니다.", nil)
	}
	apiKey, secretKey, err := common.ReadMoldAPIKeys()
	if err != nil {
		return nil, 0, newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API Key/Secret 로드 실패", err)
	}
	baseURL, err := url.Parse(apiCfg.Endpoint)
	if err != nil {
		return nil, 0, newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API endpoint 설정이 올바르지 않습니다.", err)
	}

	reqParams := make([]apiParam, 0, len(params)+1)
	reqParams = append(reqParams, params...)
	reqParams = append(reqParams, apiParam{Key: "apikey", Value: apiKey})

	u := *baseURL
	u.RawQuery = buildSignedRawQuery(reqParams, secretKey)
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext},
	}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, 0, newVMConsoleAPIError(http.StatusBadGateway, "Mold API 호출 실패", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, newVMConsoleAPIError(http.StatusBadGateway, "Mold API 응답 읽기 실패", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, resp.StatusCode, newVMConsoleAPIError(http.StatusBadGateway, fmt.Sprintf("Mold API %s 호출 실패", command), nil)
	}
	return body, resp.StatusCode, nil
}

func parseMoldKubernetesClusters(body []byte) ([]moldKubernetesCluster, error) {
	var payload interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes 클러스터 목록 응답 파싱 실패", err)
	}

	items := findArrayByKey(payload, "kubernetescluster")
	clusters := make([]moldKubernetesCluster, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		clusters = append(clusters, moldKubernetesCluster{
			ID:        valueAsString(m["id"]),
			Name:      valueAsString(m["name"]),
			State:     valueAsString(m["state"]),
			APIServer: firstNonEmpty(valueAsString(m["endpoint"]), valueAsString(m["apiserver"]), valueAsString(m["apiServer"])),
		})
	}
	return clusters, nil
}

func findArrayByKey(v interface{}, key string) []interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			if strings.EqualFold(k, key) {
				if arr, ok := vv.([]interface{}); ok {
					return arr
				}
			}
			if arr := findArrayByKey(vv, key); arr != nil {
				return arr
			}
		}
	case []interface{}:
		for _, item := range t {
			if arr := findArrayByKey(item, key); arr != nil {
				return arr
			}
		}
	}
	return nil
}

func findStringByKey(body []byte, key string) string {
	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return findStringValue(payload, key)
}

func findStringValue(v interface{}, key string) string {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			if strings.EqualFold(k, key) {
				return valueAsString(vv)
			}
			if found := findStringValue(vv, key); found != "" {
				return found
			}
		}
	case []interface{}:
		for _, item := range t {
			if found := findStringValue(item, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func valueAsString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case fmt.Stringer:
		return t.String()
	default:
		if t == nil {
			return ""
		}
		return fmt.Sprintf("%v", t)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func isMoldKubernetesSelectionCurrent(selection moldKubernetesSelection, clusters []moldKubernetesCluster) bool {
	if !selection.Enabled || strings.TrimSpace(selection.ClusterID) == "" {
		return false
	}
	for _, cluster := range clusters {
		if cluster.ID != selection.ClusterID {
			continue
		}
		if strings.TrimSpace(selection.APIServer) != "" && strings.TrimSpace(cluster.APIServer) != "" && selection.APIServer != cluster.APIServer {
			return false
		}
		return true
	}
	return false
}

func moldKubernetesKubeconfigPath() string {
	path := config.GetString("analyzer.topology.k8s.config_file")
	if path != "" {
		return path
	}
	return config.GetString("mold.kubernetes.kubeconfigPath")
}

func moldKubernetesClusterKubeconfigPath(clusterID string) string {
	basePath := moldKubernetesKubeconfigPath()
	safeID := sanitizeKubernetesFileName(clusterID)
	if safeID == "" {
		safeID = "selected"
	}
	return filepath.Join(filepath.Dir(basePath), safeID+".kubeconfig")
}

func removeMoldKubernetesManagedKubeconfigs(selection moldKubernetesSelection) error {
	if !config.GetBool("mold.kubernetes.enforceSelection") {
		return nil
	}
	paths := []string{moldKubernetesKubeconfigPath()}
	if strings.TrimSpace(selection.ClusterID) != "" {
		paths = append(paths, moldKubernetesClusterKubeconfigPath(selection.ClusterID))
	}
	seen := make(map[string]bool)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func sanitizeKubernetesFileName(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func sanitizeKubernetesTestError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	blocked := []string{"token", "client-key-data", "client-certificate-data", "certificate-authority-data", "authorization", "bearer"}
	lower := strings.ToLower(msg)
	for _, keyword := range blocked {
		if strings.Contains(lower, keyword) {
			return "민감정보를 포함할 수 있는 오류입니다. analyzer 로그와 kubeconfig 권한을 확인하세요."
		}
	}
	if len(msg) > 220 {
		msg = msg[:220] + "..."
	}
	return msg
}

func moldKubernetesStatePath() string {
	path := config.GetString("mold.kubernetes.stateFile")
	if path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(moldKubernetesKubeconfigPath()), "selected-cluster.json")
}

func readMoldKubernetesSelection() (moldKubernetesSelection, error) {
	var selection moldKubernetesSelection
	data, err := os.ReadFile(moldKubernetesStatePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return selection, nil
		}
		return selection, err
	}
	if err := json.Unmarshal(data, &selection); err != nil {
		return moldKubernetesSelection{}, err
	}
	return selection, nil
}

func writeMoldKubernetesSelection(selection moldKubernetesSelection) error {
	data, err := json.MarshalIndent(selection, "", "  ")
	if err != nil {
		return err
	}
	return writeSecureFile(moldKubernetesStatePath(), data)
}

func writeSecureFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeMoldKubernetesError(w http.ResponseWriter, err error) {
	apiErr := &vmConsoleAPIError{StatusCode: http.StatusBadGateway, Message: "Kubernetes 클러스터 연동 실패", Err: err}
	if errors.As(err, &apiErr) {
		http.Error(w, apiErr.Message, apiErr.StatusCode)
		return
	}
	http.Error(w, apiErr.Message, apiErr.StatusCode)
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func RegisterMoldKubernetesAPI(httpServer *shttp.Server, startProbe moldKubernetesProbeStarter, stopProbe moldKubernetesProbeStopper, isProbeRunning moldKubernetesProbeStatus) {
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters", func(w http.ResponseWriter, r *http.Request) {
		handleMoldKubernetesClusters(w, r, isProbeRunning, stopProbe)
	}).Methods("GET")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/select", func(w http.ResponseWriter, r *http.Request) {
		handleMoldKubernetesSelect(w, r, startProbe)
	}).Methods("POST")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/disable", func(w http.ResponseWriter, r *http.Request) {
		handleMoldKubernetesDisable(w, r, stopProbe)
	}).Methods("POST")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/test", handleMoldKubernetesTest).Methods("POST")
}
