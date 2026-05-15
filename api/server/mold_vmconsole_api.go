package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/skydive-project/skydive/common"
	shttp "github.com/skydive-project/skydive/graffiti/http"
)

type moldVMConsoleResponse struct {
	URL string `json:"url"`
}

func handleMoldVMConsole(w http.ResponseWriter, r *http.Request) {
	if !common.IsMoldConsoleEnabled() {
		http.Error(w, "mold console disabled", http.StatusNotFound)
		return
	}

	nodeID := r.URL.Query().Get("nodeId")
	vmID := r.URL.Query().Get("vmId")
	instanceName := r.URL.Query().Get("instanceName")
	mock := r.URL.Query().Get("mock")

	consoleURL, err := getMoldVMConsoleURL(nodeID, vmID, instanceName, mock)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(moldVMConsoleResponse{URL: consoleURL}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func getMoldVMConsoleURL(nodeID, vmID, instanceName, mock string) (string, error) {
	resolvedVMID, err := resolveVMID(nodeID, vmID, instanceName)
	if err != nil {
		return "", err
	}

	return requestMoldConsoleURL(nodeID, resolvedVMID, mock)
}

func resolveVMID(nodeID, vmID, instanceName string) (string, error) {
	if vmID != "" {
		return vmID, nil
	}

	if instanceName != "" {
		resolvedVMID, err := common.ResolveVMIDFromInstanceName(instanceName)
		if err != nil {
			return "", fmt.Errorf("failed to resolve vmId from instanceName")
		}
		if resolvedVMID == "" {
			return "", fmt.Errorf("vmId not found from instanceName")
		}
		return resolvedVMID, nil
	}

	if nodeID == "" {
		return "", fmt.Errorf("nodeId, instanceName or vmId is required")
	}

	resolvedVMID, err := common.ResolveVMIDFromNodeID(nodeID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve vmId from nodeId")
	}
	if resolvedVMID == "" {
		return "", fmt.Errorf("vmId not found from nodeId")
	}

	return resolvedVMID, nil
}

func requestMoldConsoleURL(nodeID, vmID, mock string) (string, error) {
	endpoint := common.GetMoldConsoleAPIEndpoint()
	if endpoint == "" {
		return "", fmt.Errorf("mold.console.apiEndpoint is empty")
	}

	baseURL, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid mold console api endpoint")
	}

	params := baseURL.Query()
	params.Set("vmId", vmID)
	if nodeID != "" {
		params.Set("nodeId", nodeID)
	}
	if mock == "true" {
		params.Set("mock", "true")
	}
	baseURL.RawQuery = params.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL.String())
	if err != nil {
		return "", fmt.Errorf("failed to call mold console api")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("mold console api returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read mold console api response")
	}

	consoleURL, err := parseMoldConsoleURL(body)
	if err != nil {
		return "", err
	}
	if consoleURL == "" {
		return "", fmt.Errorf("console url not found")
	}

	return consoleURL, nil
}

func parseMoldConsoleURL(body []byte) (string, error) {
	var simpleResponse struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &simpleResponse); err == nil && simpleResponse.URL != "" {
		return simpleResponse.URL, nil
	}

	var legacyResponse map[string]interface{}
	if err := json.Unmarshal(body, &legacyResponse); err != nil {
		return "", fmt.Errorf("failed to parse mold console api response")
	}
	for _, value := range legacyResponse {
		obj, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		if urlValue, ok := obj["url"]; ok {
			if s, ok := urlValue.(string); ok && s != "" {
				return s, nil
			}
		}
	}
	return "", fmt.Errorf("console url not found in mold console api response")
}

func RegisterMoldVMConsoleAPI(httpServer *shttp.Server) {
	httpServer.Router.HandleFunc("/api/mold/vmconsole", handleMoldVMConsole).Methods("GET")
}
