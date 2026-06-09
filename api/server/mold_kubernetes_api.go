package server

import (
	"bytes"
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
}

type moldKubernetesSelectRequest struct {
	ID string `json:"id"`
}

type moldKubernetesStatusResponse struct {
	OK             bool   `json:"ok"`
	Message        string `json:"message,omitempty"`
	KubeconfigPath string `json:"kubeconfigPath,omitempty"`
}

func handleMoldKubernetesClusters(w http.ResponseWriter, r *http.Request) {
	clusters, err := listMoldKubernetesClusters()
	if err != nil {
		writeMoldKubernetesError(w, err)
		return
	}

	selection, _ := readMoldKubernetesSelection()
	selectedID := ""
	if selection.Enabled {
		selectedID = selection.ClusterID
	}
	for i := range clusters {
		clusters[i].CollectionEnabled = selection.Enabled && clusters[i].ID == selection.ClusterID
	}

	writeJSON(w, moldKubernetesClustersResponse{
		Clusters:       clusters,
		SelectedID:     selectedID,
		KubeconfigPath: moldKubernetesKubeconfigPath(),
		RestartNeeded:  true,
	})
}

func handleMoldKubernetesSelect(w http.ResponseWriter, r *http.Request) {
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
	path := moldKubernetesKubeconfigPath()
	if err := writeSecureFile(path, []byte(kubeconfig)); err != nil {
		http.Error(w, "failed to save kubeconfig", http.StatusInternalServerError)
		return
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

	writeJSON(w, moldKubernetesStatusResponse{
		OK:             true,
		Message:        "kubeconfig saved. restart analyzer to apply k8s probe if it is not running.",
		KubeconfigPath: path,
	})
}

func handleMoldKubernetesDisable(w http.ResponseWriter, r *http.Request) {
	selection := moldKubernetesSelection{Enabled: false, UpdatedAt: time.Now().UTC()}
	if err := writeMoldKubernetesSelection(selection); err != nil {
		http.Error(w, "failed to save collection state", http.StatusInternalServerError)
		return
	}
	writeJSON(w, moldKubernetesStatusResponse{OK: true, Message: "kubernetes collection disabled. restart analyzer to stop an already running k8s probe."})
}

func handleMoldKubernetesTest(w http.ResponseWriter, r *http.Request) {
	path := moldKubernetesKubeconfigPath()
	clientConfig, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		writeJSON(w, moldKubernetesStatusResponse{OK: false, Message: "kubeconfig load failed"})
		return
	}
	clientConfig.Timeout = 10 * time.Second
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		writeJSON(w, moldKubernetesStatusResponse{OK: false, Message: "kubernetes client create failed"})
		return
	}
	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		writeJSON(w, moldKubernetesStatusResponse{OK: false, Message: "connection test failed"})
		return
	}
	writeJSON(w, moldKubernetesStatusResponse{OK: true, Message: fmt.Sprintf("connected: %s", version.GitVersion), KubeconfigPath: path})
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

func moldKubernetesKubeconfigPath() string {
	path := config.GetString("mold.kubernetes.kubeconfigPath")
	if path != "" {
		return path
	}
	return config.GetString("analyzer.topology.k8s.config_file")
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

func RegisterMoldKubernetesAPI(httpServer *shttp.Server) {
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters", handleMoldKubernetesClusters).Methods("GET")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/select", handleMoldKubernetesSelect).Methods("POST")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/disable", handleMoldKubernetesDisable).Methods("POST")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/test", handleMoldKubernetesTest).Methods("POST")
}
