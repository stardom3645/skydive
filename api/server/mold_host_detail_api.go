package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	shttp "github.com/skydive-project/skydive/graffiti/http"
)

const (
	moldHostListCommand         = "listHosts"
	moldHostVMListCommand       = "listVirtualMachines"
	moldHostSystemVMListCommand = "listSystemVms"
	moldHostRouterListCommand   = "listRouters"
)

type moldHostDetailResponse struct {
	NodeID      string          `json:"nodeId,omitempty"`
	Name        string          `json:"name,omitempty"`
	MoldMatched bool            `json:"moldMatched"`
	Mold        *moldHostDetail `json:"mold,omitempty"`
	Message     string          `json:"message,omitempty"`
}

type moldHostDetail struct {
	ID                     string `json:"id,omitempty"`
	UUID                   string `json:"uuid,omitempty"`
	Name                   string `json:"name,omitempty"`
	State                  string `json:"state,omitempty"`
	ResourceState          string `json:"resourceState,omitempty"`
	ManagementIP           string `json:"managementIp,omitempty"`
	Hypervisor             string `json:"hypervisor,omitempty"`
	Type                   string `json:"type,omitempty"`
	Zone                   string `json:"zone,omitempty"`
	ZoneID                 string `json:"zoneId,omitempty"`
	Pod                    string `json:"pod,omitempty"`
	PodID                  string `json:"podId,omitempty"`
	Cluster                string `json:"cluster,omitempty"`
	ClusterID              string `json:"clusterId,omitempty"`
	CPUAllocated           string `json:"cpuAllocated,omitempty"`
	CPUTotal               string `json:"cpuTotal,omitempty"`
	CPUAllocatedPercent    string `json:"cpuAllocatedPercent,omitempty"`
	MemoryAllocated        string `json:"memoryAllocated,omitempty"`
	MemoryTotal            string `json:"memoryTotal,omitempty"`
	MemoryAllocatedPercent string `json:"memoryAllocatedPercent,omitempty"`
	StorageUsedPercent     string `json:"storageUsedPercent,omitempty"`
	UserVMCount            *int   `json:"userVmCount,omitempty"`
	RunningVMCount         *int   `json:"runningVmCount,omitempty"`
	SystemVMCount          *int   `json:"systemVmCount,omitempty"`
	VirtualRouterCount     *int   `json:"virtualRouterCount,omitempty"`
	NetworkCount           *int   `json:"networkCount,omitempty"`
}

type moldHostLookupParams struct {
	NodeID       string
	Name         string
	HostID       string
	ManagementIP string
}

func RegisterMoldHostDetailAPI(httpServer *shttp.Server) {
	httpServer.Router.HandleFunc("/api/mold/hosts/detail", handleMoldHostDetail).Methods("GET")
}

func handleMoldHostDetail(w http.ResponseWriter, r *http.Request) {
	params := moldHostLookupParams{
		NodeID:       strings.TrimSpace(r.URL.Query().Get("nodeId")),
		Name:         strings.TrimSpace(r.URL.Query().Get("name")),
		HostID:       firstNonEmptyString(r.URL.Query().Get("hostId"), r.URL.Query().Get("moldHostId"), r.URL.Query().Get("moldHostUuid")),
		ManagementIP: firstNonEmptyString(r.URL.Query().Get("managementIp"), r.URL.Query().Get("ip"), r.URL.Query().Get("privateIp")),
	}
	if params.Name == "" && params.HostID == "" && params.ManagementIP == "" {
		writeMoldHostDetailJSON(w, http.StatusBadRequest, moldHostDetailResponse{
			NodeID:      params.NodeID,
			MoldMatched: false,
			Message:     "name, hostId or managementIp is required.",
		})
		return
	}

	host, err := resolveMoldHostDetail(params)
	if err != nil {
		writeMoldHostDetailJSON(w, http.StatusBadGateway, moldHostDetailResponse{
			NodeID:      params.NodeID,
			Name:        params.Name,
			MoldMatched: false,
			Message:     err.Error(),
		})
		return
	}
	if host == nil {
		writeMoldHostDetailJSON(w, http.StatusOK, moldHostDetailResponse{
			NodeID:      params.NodeID,
			Name:        params.Name,
			MoldMatched: false,
			Message:     "Mold host metadata was not found by name, host id or management IP.",
		})
		return
	}

	_ = enrichMoldHostConnectedResources(host)

	writeMoldHostDetailJSON(w, http.StatusOK, moldHostDetailResponse{
		NodeID:      params.NodeID,
		Name:        firstNonEmptyString(params.Name, host.Name),
		MoldMatched: true,
		Mold:        host,
	})
}

func resolveMoldHostDetail(params moldHostLookupParams) (*moldHostDetail, error) {
	if params.HostID != "" {
		host, err := findMoldHostDetailByAPIParam("id", params.HostID, params)
		if err != nil || host != nil {
			return host, err
		}
	}
	if params.Name != "" {
		host, err := findMoldHostDetailByAPIParam("name", params.Name, params)
		if err != nil || host != nil {
			return host, err
		}

		host, err = findMoldHostDetailByAPIParam("keyword", params.Name, params)
		if err != nil || host != nil {
			return host, err
		}
	}
	if params.ManagementIP != "" {
		host, err := findMoldHostDetailByAPIParam("keyword", params.ManagementIP, params)
		if err != nil || host != nil {
			return host, err
		}
	}

	return nil, nil
}

func findMoldHostDetailByAPIParam(key, value string, params moldHostLookupParams) (*moldHostDetail, error) {
	body, _, err := requestMoldAPI(moldHostListCommand, []apiParam{
		{Key: "command", Value: moldHostListCommand},
		{Key: "response", Value: "json"},
		{Key: key, Value: value},
	})
	if err != nil {
		return nil, err
	}

	hosts, err := parseMoldHostDetails(body)
	if err != nil {
		return nil, err
	}
	return pickBestMoldHostDetail(hosts, params), nil
}

func enrichMoldHostConnectedResources(host *moldHostDetail) error {
	hostID := firstNonEmptyString(host.UUID, host.ID)
	if hostID == "" {
		return nil
	}

	userVMs, err := requestMoldItems(moldHostVMListCommand, "virtualmachine", []apiParam{
		{Key: "command", Value: moldHostVMListCommand},
		{Key: "response", Value: "json"},
		{Key: "hostid", Value: hostID},
	})
	if err == nil {
		count := len(userVMs)
		host.UserVMCount = &count
		host.RunningVMCount = &count
	}

	systemVMs, err := requestMoldItems(moldHostSystemVMListCommand, "systemvm", []apiParam{
		{Key: "command", Value: moldHostSystemVMListCommand},
		{Key: "response", Value: "json"},
		{Key: "hostid", Value: hostID},
	})
	if err == nil {
		count := len(systemVMs)
		host.SystemVMCount = &count
	}

	routers, err := requestMoldItems(moldHostRouterListCommand, "router", []apiParam{
		{Key: "command", Value: moldHostRouterListCommand},
		{Key: "response", Value: "json"},
		{Key: "hostid", Value: hostID},
	})
	if err == nil {
		count := len(routers)
		host.VirtualRouterCount = &count
	}

	networks := make(map[string]struct{})
	collectMoldNetworkIDs(userVMs, networks)
	collectMoldNetworkIDs(systemVMs, networks)
	collectMoldNetworkIDs(routers, networks)
	if len(networks) > 0 {
		count := len(networks)
		host.NetworkCount = &count
	}

	return nil
}

func requestMoldItems(command, itemKey string, params []apiParam) ([]interface{}, error) {
	body, _, err := requestMoldAPI(command, params)
	if err != nil {
		return nil, err
	}
	var payload interface{}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("Mold %s response parse failed: %w", command, err)
	}
	return findMoldHostArrayByKey(payload, itemKey), nil
}

func collectMoldNetworkIDs(items []interface{}, out map[string]struct{}) {
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		collectMoldNetworkIDsFromValue(m["nic"], out)
		collectMoldNetworkIDsFromValue(m["nics"], out)
		collectMoldNetworkID(m, out)
	}
}

func collectMoldNetworkIDsFromValue(value interface{}, out map[string]struct{}) {
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				collectMoldNetworkID(m, out)
			}
		}
	case map[string]interface{}:
		collectMoldNetworkID(v, out)
	}
}

func collectMoldNetworkID(m map[string]interface{}, out map[string]struct{}) {
	key := firstNonEmptyString(
		moldHostValueAsString(m["networkid"]),
		moldHostValueAsString(m["networkId"]),
		moldHostValueAsString(m["networkname"]),
		moldHostValueAsString(m["networkName"]),
	)
	if key != "" {
		out[key] = struct{}{}
	}
}

func parseMoldHostDetails(body []byte) ([]moldHostDetail, error) {
	var payload interface{}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("Mold host detail response parse failed: %w", err)
	}

	items := findMoldHostArrayByKey(payload, "host")
	hosts := make([]moldHostDetail, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		host := moldHostDetail{
			ID:            firstNonEmptyString(moldHostValueAsString(m["id"]), moldHostValueAsString(m["hostid"])),
			UUID:          firstNonEmptyString(moldHostValueAsString(m["uuid"]), moldHostValueAsString(m["id"])),
			Name:          moldHostValueAsString(m["name"]),
			State:         firstNonEmptyString(moldHostValueAsString(m["state"]), moldHostValueAsString(m["status"])),
			ResourceState: firstNonEmptyString(moldHostValueAsString(m["resourcestate"]), moldHostValueAsString(m["resourceState"])),
			ManagementIP: firstNonEmptyString(
				moldHostValueAsString(m["ipaddress"]),
				moldHostValueAsString(m["privateipaddress"]),
				moldHostValueAsString(m["managementip"]),
				moldHostValueAsString(m["managementIp"]),
			),
			Hypervisor: firstNonEmptyString(moldHostValueAsString(m["hypervisor"]), moldHostValueAsString(m["hypervisortype"])),
			Type:       moldHostValueAsString(m["type"]),
			Zone:       firstNonEmptyString(moldHostValueAsString(m["zonename"]), moldHostValueAsString(m["zone"])),
			ZoneID:     firstNonEmptyString(moldHostValueAsString(m["zoneid"]), moldHostValueAsString(m["data_center_id"])),
			Pod:        firstNonEmptyString(moldHostValueAsString(m["podname"]), moldHostValueAsString(m["pod"])),
			PodID:      firstNonEmptyString(moldHostValueAsString(m["podid"]), moldHostValueAsString(m["pod_id"])),
			Cluster:    firstNonEmptyString(moldHostValueAsString(m["clustername"]), moldHostValueAsString(m["cluster"])),
			ClusterID:  firstNonEmptyString(moldHostValueAsString(m["clusterid"]), moldHostValueAsString(m["cluster_id"])),
		}
		host.CPUAllocated = firstNonEmptyString(moldHostValueAsString(m["cpuallocated"]), moldHostValueAsString(m["cpuAllocated"]))
		host.CPUTotal = firstNonEmptyString(moldHostValueAsString(m["cputotal"]), moldHostValueAsString(m["cpuTotal"]))
		host.CPUAllocatedPercent = percentString(host.CPUAllocated, host.CPUTotal)
		host.MemoryAllocated = firstNonEmptyString(moldHostValueAsString(m["memoryallocated"]), moldHostValueAsString(m["memoryAllocated"]))
		host.MemoryTotal = firstNonEmptyString(moldHostValueAsString(m["memorytotal"]), moldHostValueAsString(m["memoryTotal"]))
		host.MemoryAllocatedPercent = percentString(host.MemoryAllocated, host.MemoryTotal)
		host.StorageUsedPercent = percentString(firstNonEmptyString(moldHostValueAsString(m["disksizeused"]), moldHostValueAsString(m["storageused"])), firstNonEmptyString(moldHostValueAsString(m["disksizetotal"]), moldHostValueAsString(m["storagetotal"])))
		hosts = append(hosts, host)
	}
	return hosts, nil
}

func pickBestMoldHostDetail(hosts []moldHostDetail, params moldHostLookupParams) *moldHostDetail {
	if len(hosts) == 0 {
		return nil
	}
	if params.HostID != "" {
		for i := range hosts {
			if strings.EqualFold(hosts[i].ID, params.HostID) || strings.EqualFold(hosts[i].UUID, params.HostID) {
				return &hosts[i]
			}
		}
	}
	if params.Name != "" {
		for i := range hosts {
			if strings.EqualFold(hosts[i].Name, params.Name) {
				return &hosts[i]
			}
		}
	}
	if params.ManagementIP != "" {
		for i := range hosts {
			if strings.TrimSpace(hosts[i].ManagementIP) == params.ManagementIP {
				return &hosts[i]
			}
		}
	}
	return &hosts[0]
}

func findMoldHostArrayByKey(v interface{}, key string) []interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			if strings.EqualFold(k, key) {
				if arr, ok := vv.([]interface{}); ok {
					return arr
				}
			}
			if arr := findMoldHostArrayByKey(vv, key); arr != nil {
				return arr
			}
		}
	case []interface{}:
		for _, item := range t {
			if arr := findMoldHostArrayByKey(item, key); arr != nil {
				return arr
			}
		}
	}
	return nil
}

func moldHostValueAsString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		if t == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func percentString(allocated, total string) string {
	allocatedNumber, ok := parseFloatLikeString(allocated)
	if !ok {
		return ""
	}
	totalNumber, ok := parseFloatLikeString(total)
	if !ok || totalNumber <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0f", allocatedNumber/totalNumber*100)
}

func parseFloatLikeString(value string) (float64, bool) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return 0, false
	}
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	fields := strings.Fields(cleaned)
	if len(fields) > 0 {
		cleaned = fields[0]
	}
	number, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func writeMoldHostDetailJSON(w http.ResponseWriter, statusCode int, payload moldHostDetailResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
