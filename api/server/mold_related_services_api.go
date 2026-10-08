package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	auth "github.com/abbot/go-http-auth"
	"github.com/skydive-project/skydive/common"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/rbac"
)

// Only non-secret portal settings may leave the analyzer.
var moldPortalSettingNames = []string{
	"endpoint.url", "monitoring.wall.portal.protocol", "monitoring.wall.portal.domain",
	"monitoring.wall.portal.port", "cube.portal.port",
}

type moldRelatedService struct {
	URL     string `json:"url,omitempty"`
	Message string `json:"message,omitempty"`
}

type moldCubePortal struct {
	Port    int    `json:"port,omitempty"`
	Message string `json:"message,omitempty"`
}

type moldRelatedServices struct {
	Mold moldRelatedService `json:"mold"`
	Wall moldRelatedService `json:"wall"`
	Cube moldCubePortal     `json:"cube"`
}

type moldCubeHost struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ManagementIP string `json:"managementIp"`
}

func RegisterMoldRelatedServicesAPI(httpServer *shttp.Server, authBackend shttp.AuthenticationBackend) {
	httpServer.RegisterRoutes([]shttp.Route{
		{Name: "MoldRelatedServices", Method: "GET", Path: "/api/mold/related-services", HandlerFunc: handleMoldRelatedServices},
		{Name: "MoldCubeHosts", Method: "GET", Path: "/api/mold/hosts", HandlerFunc: handleMoldCubeHosts},
	}, authBackend)
}

func allowMoldPortalRead(w http.ResponseWriter, r *auth.AuthenticatedRequest) bool {
	if !rbac.Enforce(r.Username, moldCredentialsObject, "read") {
		writeMoldCredentialsJSON(w, http.StatusForbidden, map[string]string{"message": "Mold 연동 정보를 조회할 권한이 없습니다."})
		return false
	}
	return true
}

func handleMoldRelatedServices(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !allowMoldPortalRead(w, r) {
		return
	}
	settings, err := loadMoldPortalSettings(r.Context())
	if err != nil {
		writeMoldCredentialsJSON(w, http.StatusBadGateway, map[string]string{"message": "Mold 서비스 설정을 조회하지 못했습니다."})
		return
	}
	writeMoldCredentialsJSON(w, http.StatusOK, buildMoldRelatedServices(settings, common.GetMoldAccountUserURL()))
}

func loadMoldPortalSettings(ctx context.Context) (map[string]string, error) {
	settings, err := loadMoldPortalSettingsFromDB(ctx)
	if err == nil {
		return settings, nil
	}
	// Reuse the signed Mold API when direct DB access is unavailable. Each
	// request is restricted to one known setting, never a generic DB proxy.
	settings = make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var failed bool
	for _, name := range moldPortalSettingNames {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			items, requestErr := requestMoldPortalItems("listConfigurations", "configuration", []apiParam{{Key: "name", Value: name}})
			mu.Lock()
			defer mu.Unlock()
			if requestErr != nil {
				failed = true
				return
			}
			for _, item := range items {
				if itemName := moldHostValueAsString(item["name"]); itemName == name {
					settings[name] = strings.TrimSpace(moldHostValueAsString(item["value"]))
				}
			}
		}(name)
	}
	wg.Wait()
	if failed && len(settings) == 0 {
		return nil, fmt.Errorf("Mold portal configuration unavailable")
	}
	return settings, nil
}

func loadMoldPortalSettingsFromDB(ctx context.Context) (map[string]string, error) {
	db, err := common.OpenMoldDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT name, value FROM configuration WHERE name IN (?, ?, ?, ?, ?)`,
		moldPortalSettingNames[0], moldPortalSettingNames[1], moldPortalSettingNames[2], moldPortalSettingNames[3], moldPortalSettingNames[4])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := make(map[string]string)
	for rows.Next() {
		var name string
		var value sql.NullString
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}
		settings[name] = strings.TrimSpace(value.String)
	}
	return settings, rows.Err()
}

func validMoldPortalPort(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 || value > 65535 {
		return 0
	}
	return value
}

func moldBrowserURL(raw string) *url.URL {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return nil
	}
	if u.Port() != "" && validMoldPortalPort(u.Port()) == 0 {
		return nil
	}
	return u
}

func buildMoldRelatedServices(settings map[string]string, configuredUIURL string) moldRelatedServices {
	result := moldRelatedServices{
		Mold: moldRelatedService{Message: "Mold 관리 페이지 URL이 설정되지 않았거나 유효하지 않습니다."},
		Wall: moldRelatedService{Message: "Wall 포털 설정이 없거나 유효하지 않습니다."},
		Cube: moldCubePortal{Port: validMoldPortalPort(settings["cube.portal.port"])},
	}
	if result.Cube.Port == 0 {
		result.Cube.Message = "Cube 포트 설정이 없거나 유효하지 않습니다. 호스트에서 접속 포트를 선택하세요."
	}
	if endpoint := moldBrowserURL(settings["endpoint.url"]); endpoint != nil {
		// Mold's default endpoint may be localhost. Reuse the existing listener
		// resolver for its externally reachable host, protocol and port.
		ip := net.ParseIP(endpoint.Hostname())
		if strings.EqualFold(endpoint.Hostname(), "localhost") || (ip != nil && ip.IsLoopback()) {
			endpoint = moldBrowserURL(configuredUIURL)
		}
		if endpoint != nil {
			endpoint.Path = strings.TrimSuffix(strings.TrimRight(endpoint.Path, "/"), "/api") + "/"
			endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "", "", ""
			result.Mold = moldRelatedService{URL: endpoint.String()}
		}
	}
	protocol := strings.ToLower(strings.TrimSpace(settings["monitoring.wall.portal.protocol"]))
	domain := strings.TrimSpace(settings["monitoring.wall.portal.domain"])
	if domain == "" {
		if mold := moldBrowserURL(result.Mold.URL); mold != nil {
			domain = mold.Hostname()
		}
	}
	port := validMoldPortalPort(settings["monitoring.wall.portal.port"])
	if domain != "" && port != 0 && !strings.ContainsAny(domain, "/@?# \\") {
		domain = strings.Trim(domain, "[]")
		u := moldBrowserURL(protocol + "://" + net.JoinHostPort(domain, strconv.Itoa(port)))
		if u != nil {
			result.Wall = moldRelatedService{URL: u.String()}
		}
	}
	return result
}

// Error envelopes sometimes arrive with HTTP 200. Treat them as failures,
// rather than a successful empty host/configuration list.
func requestMoldPortalItems(command, itemKey string, extra []apiParam) ([]map[string]interface{}, error) {
	items, _, err := requestMoldPortalPage(command, itemKey, extra)
	return items, err
}

func requestMoldPortalPage(command, itemKey string, extra []apiParam) ([]map[string]interface{}, int, error) {
	params := append([]apiParam{{Key: "command", Value: command}, {Key: "response", Value: "json"}}, extra...)
	body, _, err := requestMoldAPI(command, params)
	if err != nil {
		return nil, -1, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, -1, err
	}
	response, ok := envelope[strings.ToLower(command)+"response"]
	if !ok {
		return nil, -1, fmt.Errorf("invalid Mold %s response", command)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(response, &payload); err != nil {
		return nil, -1, err
	}
	if _, exists := payload["errorcode"]; exists {
		return nil, -1, fmt.Errorf("Mold %s rejected", command)
	}
	items := []map[string]interface{}{}
	if raw, exists := payload[itemKey]; exists {
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, -1, err
		}
	}
	count := -1
	if raw, exists := payload["count"]; exists {
		_ = json.Unmarshal(raw, &count)
	}
	return items, count, nil
}

func handleMoldCubeHosts(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !allowMoldPortalRead(w, r) {
		return
	}
	hosts, err := listMoldCubeHosts(r.Context())
	if err != nil {
		writeMoldCredentialsJSON(w, http.StatusBadGateway, map[string]string{"message": "Mold 호스트 목록을 조회하지 못했습니다."})
		return
	}
	writeMoldCredentialsJSON(w, http.StatusOK, map[string]interface{}{"hosts": hosts})
}

func listMoldCubeHosts(ctx context.Context) ([]moldCubeHost, error) {
	const pageSize = 500
	result := []moldCubeHost{}
	seen := make(map[string]bool)
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		items, count, err := requestMoldPortalPage(moldHostListCommand, "host", []apiParam{
			{Key: "type", Value: "Routing"}, {Key: "page", Value: strconv.Itoa(page)}, {Key: "pagesize", Value: strconv.Itoa(pageSize)},
		})
		if err != nil {
			return nil, err
		}
		// Use the existing host parser's management-IP mapping, not public or storage IP.
		body, _ := json.Marshal(map[string]interface{}{"listhostsresponse": map[string]interface{}{"host": items}})
		hosts, err := parseMoldHostDetails(body)
		if err != nil {
			return nil, err
		}
		added := 0
		for _, host := range hosts {
			if host.Type != "" && !strings.EqualFold(host.Type, "Routing") {
				continue
			}
			id := firstNonEmptyString(host.UUID, host.ID, host.Name, host.ManagementIP)
			if seen[id] {
				continue
			}
			seen[id] = true
			result = append(result, moldCubeHost{ID: id, Name: firstNonEmptyString(host.Name, id), ManagementIP: strings.TrimSpace(host.ManagementIP)})
			added++
		}
		// Mold may cap pages below our requested size. Follow the reported
		// total (or continue until empty for older responses without count).
		if len(items) == 0 || (count >= 0 && len(seen) >= count) {
			break
		}
		if added == 0 {
			return nil, fmt.Errorf("Mold host pagination made no progress")
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result, nil
}
