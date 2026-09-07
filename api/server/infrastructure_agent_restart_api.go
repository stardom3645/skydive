package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	auth "github.com/abbot/go-http-auth"

	"github.com/skydive-project/skydive/config"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/rbac"
)

const infrastructureAgentRestartObject = "infrastructure-agent"

type infrastructureAgentRestartHost struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type infrastructureAgentRestartTargetResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type infrastructureAgentRestartStatus struct {
	Running         bool                                     `json:"running"`
	LastStartedAt   string                                   `json:"lastStartedAt,omitempty"`
	LastCompletedAt string                                   `json:"lastCompletedAt,omitempty"`
	LastResult      string                                   `json:"lastResult"`
	Total           int                                      `json:"total"`
	Succeeded       int                                      `json:"succeeded"`
	Failed          int                                      `json:"failed"`
	Targets         []infrastructureAgentRestartTargetResult `json:"targets,omitempty"`
}

type infrastructureAgentRestartResponse struct {
	Status infrastructureAgentRestartStatus `json:"status"`
	Hosts  []infrastructureAgentRestartHost `json:"hosts,omitempty"`
}

type infrastructureAgentRestartRequest struct {
	HostIDs []string `json:"hostIds"`
}

var infrastructureAgentRestartState = struct {
	sync.RWMutex
	status infrastructureAgentRestartStatus
}{
	status: infrastructureAgentRestartStatus{LastResult: "never"},
}

var runInfrastructureAgentRestart = restartInfrastructureAgent

type infrastructureAgentSSHConfig struct {
	User           string
	Port           int
	PrivateKeyPath string
}

func currentInfrastructureAgentSSHConfig() infrastructureAgentSSHConfig {
	sshConfig := infrastructureAgentSSHConfig{
		User:           strings.TrimSpace(config.GetString("mold.infrastructureAgent.ssh.user")),
		Port:           config.GetInt("mold.infrastructureAgent.ssh.port"),
		PrivateKeyPath: strings.TrimSpace(config.GetString("mold.infrastructureAgent.ssh.privateKeyPath")),
	}
	if sshConfig.User == "" {
		sshConfig.User = "root"
	}
	if sshConfig.Port <= 0 {
		sshConfig.Port = 22
	}
	if sshConfig.PrivateKeyPath == "" {
		sshConfig.PrivateKeyPath = "/root/.ssh/id_rsa"
	}
	return sshConfig
}

func infrastructureAgentSSHArgs(host moldHostDetail, sshConfig infrastructureAgentSSHConfig) ([]string, error) {
	managementIP := strings.TrimSpace(host.ManagementIP)
	if managementIP == "" {
		return nil, fmt.Errorf("관리 IP가 없습니다")
	}
	keyInfo, err := os.Stat(sshConfig.PrivateKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("SSH 개인키를 찾을 수 없습니다: %s", sshConfig.PrivateKeyPath)
		}
		return nil, fmt.Errorf("SSH 개인키를 확인할 수 없습니다: %v", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("SSH 개인키 경로가 일반 파일이 아닙니다: %s", sshConfig.PrivateKeyPath)
	}
	return []string{
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5",
		"-p", strconv.Itoa(sshConfig.Port),
		"-i", sshConfig.PrivateKeyPath,
		sshConfig.User + "@" + managementIP,
		"systemctl reset-failed netdive-agent.service >/dev/null 2>&1 || true; systemctl restart netdive-agent.service",
	}, nil
}

func listInfrastructureAgentHosts() ([]moldHostDetail, error) {
	body, _, err := requestMoldAPI(moldHostListCommand, []apiParam{
		{Key: "command", Value: moldHostListCommand},
		{Key: "response", Value: "json"},
		// CloudStack listHosts also exposes ConsoleProxy and
		// SecondaryStorageVM hosts. Netdive Agent is installed on the routing
		// hypervisor hosts, so constrain the source query before presenting or
		// executing restart targets.
		{Key: "type", Value: "Routing"},
	})
	if err != nil {
		return nil, err
	}
	hosts, err := parseMoldHostDetails(body)
	if err != nil {
		return nil, err
	}
	hosts = filterInfrastructureAgentHosts(hosts)
	sort.SliceStable(hosts, func(i, j int) bool {
		return strings.ToLower(hosts[i].Name) < strings.ToLower(hosts[j].Name)
	})
	return hosts, nil
}

func filterInfrastructureAgentHosts(hosts []moldHostDetail) []moldHostDetail {
	filtered := make([]moldHostDetail, 0, len(hosts))
	seen := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		hostType := strings.ToLower(strings.TrimSpace(host.Type))
		if hostType != "routing" {
			// Some older Mold responses omit type even when type=Routing was
			// requested. Accept only records that still have routing-host
			// characteristics; never infer eligibility from VM-like names.
			hypervisor := strings.ToLower(strings.TrimSpace(host.Hypervisor))
			if hostType != "" || hypervisor == "" || hypervisor == "none" || strings.TrimSpace(host.ClusterID) == "" {
				continue
			}
		}
		if strings.TrimSpace(host.ManagementIP) == "" {
			continue
		}
		identity := strings.ToLower(firstNonEmptyString(host.UUID, host.ID, host.ManagementIP))
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		filtered = append(filtered, host)
	}
	return filtered
}

func selectInfrastructureAgentHosts(hosts []moldHostDetail, requestedIDs []string) ([]moldHostDetail, error) {
	if len(requestedIDs) == 0 {
		return hosts, nil
	}
	wanted := make(map[string]struct{}, len(requestedIDs))
	for _, id := range requestedIDs {
		if id = strings.TrimSpace(id); id != "" {
			wanted[strings.ToLower(id)] = struct{}{}
		}
	}
	selected := make([]moldHostDetail, 0, len(wanted))
	for _, host := range hosts {
		_, byID := wanted[strings.ToLower(host.ID)]
		_, byUUID := wanted[strings.ToLower(host.UUID)]
		if byID || byUUID {
			selected = append(selected, host)
			delete(wanted, strings.ToLower(host.ID))
			delete(wanted, strings.ToLower(host.UUID))
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("Mold에서 대상 호스트를 찾을 수 없습니다: %s", strings.Join(missing, ", "))
	}
	return selected, nil
}

func restartInfrastructureAgent(host moldHostDetail) error {
	args, err := infrastructureAgentSSHArgs(host, currentInfrastructureAgentSSHConfig())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "ssh", args...)
	output, err := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("재시작 명령 시간이 초과되었습니다")
	}
	if err != nil {
		reason := strings.TrimSpace(string(output))
		if reason == "" {
			reason = err.Error()
		}
		return fmt.Errorf("재시작 실패: %s", reason)
	}
	return nil
}

func currentInfrastructureAgentRestartStatus() infrastructureAgentRestartStatus {
	infrastructureAgentRestartState.RLock()
	defer infrastructureAgentRestartState.RUnlock()
	status := infrastructureAgentRestartState.status
	status.Targets = append([]infrastructureAgentRestartTargetResult(nil), status.Targets...)
	return status
}

func setInfrastructureAgentRestartFinished(results []infrastructureAgentRestartTargetResult) infrastructureAgentRestartStatus {
	succeeded := 0
	for _, result := range results {
		if result.Success {
			succeeded++
		}
	}
	lastResult := "failed"
	if succeeded == len(results) && succeeded > 0 {
		lastResult = "success"
	} else if succeeded > 0 {
		lastResult = "partial"
	}
	infrastructureAgentRestartState.Lock()
	infrastructureAgentRestartState.status.Running = false
	infrastructureAgentRestartState.status.LastCompletedAt = time.Now().UTC().Format(time.RFC3339)
	infrastructureAgentRestartState.status.LastResult = lastResult
	infrastructureAgentRestartState.status.Total = len(results)
	infrastructureAgentRestartState.status.Succeeded = succeeded
	infrastructureAgentRestartState.status.Failed = len(results) - succeeded
	infrastructureAgentRestartState.status.Targets = append([]infrastructureAgentRestartTargetResult(nil), results...)
	status := infrastructureAgentRestartState.status
	infrastructureAgentRestartState.Unlock()
	return status
}

func writeInfrastructureAgentRestartJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func handleInfrastructureAgentRestartGet(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, infrastructureAgentRestartObject, "read") {
		writeInfrastructureAgentRestartJSON(w, http.StatusForbidden, map[string]string{"message": "관리 권한이 필요합니다."})
		return
	}
	hosts, err := listInfrastructureAgentHosts()
	if err != nil {
		writeInfrastructureAgentRestartJSON(w, http.StatusBadGateway, map[string]string{"message": err.Error()})
		return
	}
	items := make([]infrastructureAgentRestartHost, 0, len(hosts))
	for _, host := range hosts {
		items = append(items, infrastructureAgentRestartHost{ID: firstNonEmptyString(host.UUID, host.ID), Name: host.Name})
	}
	writeInfrastructureAgentRestartJSON(w, http.StatusOK, infrastructureAgentRestartResponse{
		Status: currentInfrastructureAgentRestartStatus(),
		Hosts:  items,
	})
}

func handleInfrastructureAgentRestartPost(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, infrastructureAgentRestartObject, "write") {
		writeInfrastructureAgentRestartJSON(w, http.StatusForbidden, map[string]string{"message": "관리 권한이 필요합니다."})
		return
	}
	request := infrastructureAgentRestartRequest{}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeInfrastructureAgentRestartJSON(w, http.StatusBadRequest, map[string]string{"message": "요청 형식이 올바르지 않습니다."})
			return
		}
	}

	infrastructureAgentRestartState.Lock()
	if infrastructureAgentRestartState.status.Running {
		status := infrastructureAgentRestartState.status
		infrastructureAgentRestartState.Unlock()
		writeInfrastructureAgentRestartJSON(w, http.StatusConflict, infrastructureAgentRestartResponse{Status: status})
		return
	}
	infrastructureAgentRestartState.status = infrastructureAgentRestartStatus{
		Running:       true,
		LastStartedAt: time.Now().UTC().Format(time.RFC3339),
		LastResult:    "running",
	}
	infrastructureAgentRestartState.Unlock()

	hosts, err := listInfrastructureAgentHosts()
	if err == nil {
		hosts, err = selectInfrastructureAgentHosts(hosts, request.HostIDs)
	}
	if err != nil || len(hosts) == 0 {
		if err == nil {
			err = fmt.Errorf("재시작할 호스트가 없습니다")
		}
		status := setInfrastructureAgentRestartFinished([]infrastructureAgentRestartTargetResult{{Name: "Mold 호스트 조회", Error: err.Error()}})
		writeInfrastructureAgentRestartJSON(w, http.StatusBadGateway, infrastructureAgentRestartResponse{Status: status})
		return
	}

	results := make([]infrastructureAgentRestartTargetResult, len(hosts))
	var wg sync.WaitGroup
	for i := range hosts {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			host := hosts[index]
			result := infrastructureAgentRestartTargetResult{
				ID:   firstNonEmptyString(host.UUID, host.ID),
				Name: firstNonEmptyString(host.Name, host.ManagementIP),
			}
			if restartErr := runInfrastructureAgentRestart(host); restartErr != nil {
				result.Error = restartErr.Error()
			} else {
				result.Success = true
			}
			results[index] = result
		}(i)
	}
	wg.Wait()
	status := setInfrastructureAgentRestartFinished(results)
	writeInfrastructureAgentRestartJSON(w, http.StatusOK, infrastructureAgentRestartResponse{Status: status})
}

// RegisterInfrastructureAgentRestartAPI registers the privileged Netdive Agent restart endpoint.
func RegisterInfrastructureAgentRestartAPI(httpServer *shttp.Server, authBackend shttp.AuthenticationBackend) {
	httpServer.RegisterRoutes([]shttp.Route{
		{Name: "InfrastructureAgentRestartStatus", Method: "GET", Path: "/api/infrastructure/agents/restart", HandlerFunc: handleInfrastructureAgentRestartGet},
		{Name: "InfrastructureAgentRestart", Method: "POST", Path: "/api/infrastructure/agents/restart", HandlerFunc: handleInfrastructureAgentRestartPost},
	}, authBackend)
}
