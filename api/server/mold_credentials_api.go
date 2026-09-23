package server

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	auth "github.com/abbot/go-http-auth"

	"github.com/skydive-project/skydive/common"
	"github.com/skydive-project/skydive/config"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/rbac"
)

const (
	moldCredentialsObject = "mold-credentials"
	maxCredentialLength   = 4096
)

type moldCredentialsRequest struct {
	APIKey     string `json:"apiKey"`
	SecretKey  string `json:"secretKey"`
	DBPassword string `json:"dbPassword"`
}

type moldCredentialsStatus struct {
	Configured           bool   `json:"configured"`
	APIConfigured        bool   `json:"apiConfigured"`
	DBPasswordConfigured bool   `json:"dbPasswordConfigured"`
	Message              string `json:"message,omitempty"`
}

func writeMoldCredentialsJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func decodeMoldCredentialsRequest(w http.ResponseWriter, r *auth.AuthenticatedRequest) (moldCredentialsRequest, error) {
	request := moldCredentialsRequest{}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, errors.New("요청 형식이 올바르지 않습니다.")
	}
	request.APIKey = strings.TrimSpace(request.APIKey)
	request.SecretKey = strings.TrimSpace(request.SecretKey)
	request.DBPassword = strings.TrimSpace(request.DBPassword)
	if request.APIKey == "" || request.SecretKey == "" {
		return request, errors.New("API Key와 Secret Key를 모두 입력해 주세요.")
	}
	if len(request.APIKey) > maxCredentialLength || len(request.SecretKey) > maxCredentialLength || len(request.DBPassword) > maxCredentialLength {
		return request, errors.New("입력한 키가 허용된 길이를 초과했습니다.")
	}
	return request, nil
}

func testMoldCredentials(apiKey, secretKey string) error {
	endpoint := strings.TrimSpace(config.GetString("mold.api.endpoint"))
	baseURL, err := url.Parse(endpoint)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API endpoint 설정이 올바르지 않습니다.", err)
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		},
	}
	return verifyMoldAPIListCapabilities(client, baseURL, apiKey, secretKey)
}

func moldCredentialsErrorStatus(err error) int {
	apiErr := &vmConsoleAPIError{}
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return http.StatusInternalServerError
}

func handleMoldCredentialsGet(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, moldCredentialsObject, "read") {
		writeMoldCredentialsJSON(w, http.StatusForbidden, moldCredentialsStatus{Message: "관리 권한이 필요합니다."})
		return
	}
	apiConfigured, dbConfigured, err := common.MoldCredentialsConfigured()
	if err != nil {
		writeMoldCredentialsJSON(w, http.StatusInternalServerError, moldCredentialsStatus{Message: "저장된 연동 정보를 확인할 수 없습니다."})
		return
	}
	writeMoldCredentialsJSON(w, http.StatusOK, moldCredentialsStatus{
		Configured: apiConfigured && dbConfigured, APIConfigured: apiConfigured, DBPasswordConfigured: dbConfigured,
	})
}

func handleMoldCredentialsTest(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, moldCredentialsObject, "write") {
		writeMoldCredentialsJSON(w, http.StatusForbidden, moldCredentialsStatus{Message: "관리 권한이 필요합니다."})
		return
	}
	request, err := decodeMoldCredentialsRequest(w, r)
	if err != nil {
		writeMoldCredentialsJSON(w, http.StatusBadRequest, moldCredentialsStatus{Message: err.Error()})
		return
	}
	if err := testMoldCredentials(request.APIKey, request.SecretKey); err != nil {
		writeMoldCredentialsJSON(w, moldCredentialsErrorStatus(err), moldCredentialsStatus{Message: err.Error()})
		return
	}
	writeMoldCredentialsJSON(w, http.StatusOK, moldCredentialsStatus{Message: "Mold API 연결에 성공했습니다."})
}

func handleMoldCredentialsPut(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, moldCredentialsObject, "write") {
		writeMoldCredentialsJSON(w, http.StatusForbidden, moldCredentialsStatus{Message: "관리 권한이 필요합니다."})
		return
	}
	request, err := decodeMoldCredentialsRequest(w, r)
	if err != nil {
		writeMoldCredentialsJSON(w, http.StatusBadRequest, moldCredentialsStatus{Message: err.Error()})
		return
	}
	if err := testMoldCredentials(request.APIKey, request.SecretKey); err != nil {
		writeMoldCredentialsJSON(w, moldCredentialsErrorStatus(err), moldCredentialsStatus{Message: err.Error()})
		return
	}
	_, dbConfigured, err := common.MoldCredentialsConfigured()
	if err != nil {
		writeMoldCredentialsJSON(w, http.StatusInternalServerError, moldCredentialsStatus{Message: "저장된 연동 정보를 확인할 수 없습니다."})
		return
	}
	if request.DBPassword == "" && !dbConfigured {
		writeMoldCredentialsJSON(w, http.StatusBadRequest, moldCredentialsStatus{Message: "Mold DB 비밀번호를 입력해 주세요."})
		return
	}
	if err := common.WriteMoldCredentials(request.APIKey, request.SecretKey, request.DBPassword); err != nil {
		writeMoldCredentialsJSON(w, http.StatusInternalServerError, moldCredentialsStatus{Message: "연동 정보를 저장하지 못했습니다."})
		return
	}
	writeMoldCredentialsJSON(w, http.StatusOK, moldCredentialsStatus{
		Configured: true, APIConfigured: true, DBPasswordConfigured: true,
		Message: "Mold 연동 정보를 암호화하여 저장했습니다.",
	})
}

// RegisterMoldCredentialsAPI registers authenticated endpoints for testing and
// replacing the Mold API credential pair. Credential values are never returned.
func RegisterMoldCredentialsAPI(httpServer *shttp.Server, authBackend shttp.AuthenticationBackend) {
	httpServer.RegisterRoutes([]shttp.Route{
		{Name: "MoldCredentialsStatus", Method: "GET", Path: "/api/mold/credentials", HandlerFunc: handleMoldCredentialsGet},
		{Name: "MoldCredentialsTest", Method: "POST", Path: "/api/mold/credentials/test", HandlerFunc: handleMoldCredentialsTest},
		{Name: "MoldCredentialsUpdate", Method: "PUT", Path: "/api/mold/credentials", HandlerFunc: handleMoldCredentialsPut},
	}, authBackend)
}
