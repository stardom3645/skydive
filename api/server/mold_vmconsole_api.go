package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
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

	consoleURL, err := getMoldVMConsoleURL(nodeID, vmID)
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

func getMoldVMConsoleURL(nodeID, vmID string) (string, error) {
	resolvedVMID, err := resolveVMID(nodeID, vmID)
	if err != nil {
		return "", err
	}

	return requestMoldConsoleURL(resolvedVMID)
}

func resolveVMID(nodeID, vmID string) (string, error) {
	if vmID != "" {
		return vmID, nil
	}

	if nodeID == "" {
		return "", fmt.Errorf("nodeId or vmId is required")
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

func requestMoldConsoleURL(vmID string) (string, error) {
	apiCfg := common.GetMoldAPIConfig()
	if apiCfg.Endpoint == "" {
		return "", fmt.Errorf("mold.api.endpoint is empty")
	}

	apiKey, secretKey, err := common.GetMoldAdminKeys()
	if err != nil {
		return "", fmt.Errorf("failed to load mold admin keys")
	}

	params := map[string]string{
		"apikey":           apiKey,
		"command":          "getVirtualMachineConsoleProxyURL",
		"response":         "json",
		"virtualmachineid": vmID,
	}

	signedURL, err := buildSignedMoldAPIURL(apiCfg.Endpoint, params, secretKey)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(signedURL)
	if err != nil {
		return "", fmt.Errorf("failed to call mold api")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("mold api returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read mold api response")
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
	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse mold api response")
	}

	for _, value := range response {
		obj, ok := value.(map[string]interface{})
		if !ok {
			continue
		}

		if urlValue, ok := obj["consoleproxyurl"]; ok {
			if s, ok := urlValue.(string); ok && s != "" {
				return s, nil
			}
		}

		if urlValue, ok := obj["url"]; ok {
			if s, ok := urlValue.(string); ok && s != "" {
				return s, nil
			}
		}
	}

	return "", fmt.Errorf("console url not found in mold api response")
}

func buildSignedMoldAPIURL(endpoint string, params map[string]string, secretKey string) (string, error) {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return strings.ToLower(keys[i]) < strings.ToLower(keys[j])
	})

	queryPairs := make([]string, 0, len(keys))
	signPairs := make([]string, 0, len(keys))
	for _, key := range keys {
		val := params[key]
		queryPairs = append(queryPairs, fmt.Sprintf("%s=%s", key, url.QueryEscape(val)))
		signPairs = append(signPairs, fmt.Sprintf("%s=%s", strings.ToLower(key), strings.ToLower(url.QueryEscape(val))))
	}

	signature := createMoldSignature(strings.Join(signPairs, "&"), secretKey)
	query := strings.Join(queryPairs, "&") + "&signature=" + url.QueryEscape(signature)

	return endpoint + "?" + query, nil
}

func createMoldSignature(payload, secretKey string) string {
	mac := hmac.New(sha1.New, []byte(secretKey))
	mac.Write([]byte(payload))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func RegisterMoldVMConsoleAPI(httpServer *shttp.Server) {
	httpServer.Router.HandleFunc("/api/mold/vmconsole", handleMoldVMConsole).Methods("GET")
}
