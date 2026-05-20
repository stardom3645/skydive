package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

type vmConsoleAPIError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *vmConsoleAPIError) Error() string {
	return e.Message
}

func (e *vmConsoleAPIError) Unwrap() error {
	return e.Err
}

func newVMConsoleAPIError(code int, message string, err error) error {
	return &vmConsoleAPIError{
		StatusCode: code,
		Message:    message,
		Err:        err,
	}
}

func writeVMConsoleError(w http.ResponseWriter, err error) {
	apiErr := &vmConsoleAPIError{
		StatusCode: http.StatusBadGateway,
		Message:    "콘솔 URL 요청 실패",
		Err:        err,
	}
	if errors.As(err, &apiErr) {
		http.Error(w, apiErr.Message, apiErr.StatusCode)
		return
	}
	http.Error(w, apiErr.Message, apiErr.StatusCode)
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
		writeVMConsoleError(w, err)
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

	return requestMoldConsoleURL(resolvedVMID, mock)
}

func resolveVMID(nodeID, vmID, instanceName string) (string, error) {
	if vmID != "" {
		return vmID, nil
	}

	if instanceName != "" {
		resolvedVMID, err := common.ResolveVMIDFromInstanceName(instanceName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", newVMConsoleAPIError(http.StatusNotFound, "VM을 찾을 수 없습니다.", err)
			}
			return "", newVMConsoleAPIError(http.StatusBadGateway, "VM 조회(DB) 실패", err)
		}
		if resolvedVMID == "" {
			return "", newVMConsoleAPIError(http.StatusNotFound, "VM을 찾을 수 없습니다.", nil)
		}
		return resolvedVMID, nil
	}

	if nodeID == "" {
		return "", newVMConsoleAPIError(http.StatusBadRequest, "nodeId, instanceName or vmId is required", nil)
	}

	resolvedVMID, err := common.ResolveVMIDFromNodeID(nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", newVMConsoleAPIError(http.StatusNotFound, "VM을 찾을 수 없습니다.", err)
		}
		return "", newVMConsoleAPIError(http.StatusBadGateway, "VM 조회(DB) 실패", err)
	}
	if resolvedVMID == "" {
		return "", newVMConsoleAPIError(http.StatusNotFound, "VM을 찾을 수 없습니다.", nil)
	}

	return resolvedVMID, nil
}

func requestMoldConsoleURL(vmID, mock string) (string, error) {
	if mock == "true" && common.IsMoldConsoleMockAllowed() {
		return buildMockMoldConsoleURL(), nil
	}

	apiCfg := common.GetMoldAPIConfig()
	if apiCfg.Endpoint == "" {
		return "", newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API endpoint 설정이 비어 있습니다.", nil)
	}

	apiKey, secretKey, err := common.ReadMoldAPIKeys()
	if err != nil {
		secretErr := &common.SecretFileError{}
		if errors.As(err, &secretErr) {
			switch secretErr.KeyName {
			case "mold.api.apiKeyFile":
				switch secretErr.Reason {
				case common.SecretFileMissing:
					return "", newVMConsoleAPIError(http.StatusServiceUnavailable, "API Key 파일이 없습니다.", err)
				case common.SecretFileEmpty:
					return "", newVMConsoleAPIError(http.StatusServiceUnavailable, "API Key 파일이 비어 있습니다.", err)
				default:
					return "", newVMConsoleAPIError(http.StatusServiceUnavailable, "API Key 파일 읽기 실패", err)
				}
			case "mold.api.secretKeyFile":
				switch secretErr.Reason {
				case common.SecretFileMissing:
					return "", newVMConsoleAPIError(http.StatusServiceUnavailable, "Secret Key 파일이 없습니다.", err)
				case common.SecretFileEmpty:
					return "", newVMConsoleAPIError(http.StatusServiceUnavailable, "Secret Key 파일이 비어 있습니다.", err)
				default:
					return "", newVMConsoleAPIError(http.StatusServiceUnavailable, "Secret Key 파일 읽기 실패", err)
				}
			}
		}
		return "", newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API Key/Secret 로드 실패", err)
	}

	baseURL, err := url.Parse(apiCfg.Endpoint)
	if err != nil {
		return "", newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API endpoint 설정이 올바르지 않습니다.", err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		},
	}

	if err := verifyMoldAPIListCapabilities(client, baseURL, apiKey, secretKey); err != nil {
		return "", err
	}

	if url, _, err := callMoldAPI(client, baseURL, apiCfg.Command, apiKey, secretKey, []apiParam{
		{Key: "command", Value: apiCfg.Command},
		{Key: "response", Value: "json"},
		{Key: "apikey", Value: apiKey},
		{Key: "virtualmachineid", Value: vmID},
	}); err == nil {
		return url, nil
	} else {
		return "", err
	}
}

type apiParam struct {
	Key   string
	Value string
}

func verifyMoldAPIListCapabilities(client *http.Client, baseURL *url.URL, apiKey, secretKey string) error {
	_, status, err := callMoldAPI(client, baseURL, "listCapabilities", apiKey, secretKey, []apiParam{
		{Key: "command", Value: "listCapabilities"},
		{Key: "response", Value: "json"},
		{Key: "apikey", Value: apiKey},
	})
	if err != nil {
		switch status {
		case http.StatusUnauthorized:
			return newVMConsoleAPIError(http.StatusUnauthorized, "API Key/Secret 인증 실패", err)
		case http.StatusForbidden:
			return newVMConsoleAPIError(http.StatusForbidden, "콘솔 접근 권한 없음", err)
		default:
			return newVMConsoleAPIError(http.StatusBadGateway, "Mold API 호출 실패", err)
		}
	}
	return nil
}

func callMoldAPI(client *http.Client, baseURL *url.URL, command, apiKey, secretKey string, reqParams []apiParam) (string, int, error) {
	u := *baseURL
	u.RawQuery = buildSignedRawQuery(reqParams, secretKey)

	resp, err := client.Get(u.String())
	if err != nil {
		return "", 0, newVMConsoleAPIError(http.StatusBadGateway, "Mold API 호출 실패", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, newVMConsoleAPIError(http.StatusBadGateway, "Mold API 응답 읽기 실패", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized {
			return "", resp.StatusCode, newVMConsoleAPIError(http.StatusUnauthorized, "API Key/Secret 인증 실패", nil)
		}
		if resp.StatusCode == http.StatusForbidden {
			return "", resp.StatusCode, newVMConsoleAPIError(http.StatusForbidden, "콘솔 접근 권한 없음", nil)
		}
		return "", resp.StatusCode, newVMConsoleAPIError(http.StatusBadGateway, "Mold API 호출 실패", nil)
	}
	if command == "listCapabilities" {
		return "ok", resp.StatusCode, nil
	}

	consoleURL, err := parseMoldConsoleURL(body)
	if err != nil {
		return "", resp.StatusCode, newVMConsoleAPIError(http.StatusBadGateway, "Mold API 응답 파싱 실패", err)
	}
	if consoleURL == "" {
		return "", resp.StatusCode, newVMConsoleAPIError(http.StatusBadGateway, "콘솔 URL이 응답에 없습니다.", nil)
	}
	return consoleURL, resp.StatusCode, nil
}

func parseMoldConsoleURL(body []byte) (string, error) {
	var simpleResponse struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &simpleResponse); err == nil && simpleResponse.URL != "" {
		return simpleResponse.URL, nil
	}

	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("failed to parse mold api response")
	}
	if found := findConsoleURL(payload); found != "" {
		return found, nil
	}
	return "", fmt.Errorf("console url not found in mold api response")
}

func buildSignedRawQuery(reqParams []apiParam, secretKey string) string {
	requestParts := make([]string, 0, len(reqParams))
	reqMap := make(map[string]string, len(reqParams))
	for _, p := range reqParams {
		requestParts = append(requestParts, p.Key+"="+quotePlus(p.Value))
		reqMap[p.Key] = p.Value
	}
	requestStr := strings.Join(requestParts, "&")
	signature := buildMoldAPISignature(reqMap, secretKey)
	return requestStr + "&signature=" + quotePlus(signature)
}

func buildMoldAPISignature(reqMap map[string]string, secretKey string) string {
	keys := make([]string, 0, len(reqMap))
	for k := range reqMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(reqMap))
	for _, k := range keys {
		key := strings.ToLower(k)
		value := strings.ToLower(quotePlus(reqMap[k]))
		value = strings.ReplaceAll(value, "+", "%20")
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	toSign := strings.Join(parts, "&")
	h := hmac.New(sha256.New, []byte(secretKey))
	_, _ = h.Write([]byte(toSign))
	return strings.TrimSpace(base64.StdEncoding.EncodeToString(h.Sum(nil)))
}

func quotePlus(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "%20", "+")
}

func buildMockMoldConsoleURL() string {
	return "http://127.0.0.1/resource/noVNC/vnc.html?autoconnect=true&show_dot=true&port=8080&token=mock"
}

func findConsoleURL(v interface{}) string {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			if strings.EqualFold(k, "url") || strings.Contains(strings.ToLower(k), "consoleproxyurl") {
				if s, ok := vv.(string); ok && s != "" {
					return s
				}
			}
			if s := findConsoleURL(vv); s != "" {
				return s
			}
		}
	case []interface{}:
		for _, item := range t {
			if s := findConsoleURL(item); s != "" {
				return s
			}
		}
	case string:
		if strings.Contains(t, "/resource/noVNC/") || strings.Contains(strings.ToLower(t), "vnc.html") {
			return t
		}
	}
	return ""
}

func RegisterMoldVMConsoleAPI(httpServer *shttp.Server) {
	httpServer.Router.HandleFunc("/api/mold/vmconsole", handleMoldVMConsole).Methods("GET")
}
