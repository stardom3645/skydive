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
	ID                     string                `json:"id,omitempty"`
	UUID                   string                `json:"uuid,omitempty"`
	Name                   string                `json:"name,omitempty"`
	State                  string                `json:"state,omitempty"`
	ResourceState          string                `json:"resourceState,omitempty"`
	ManagementIP           string                `json:"managementIp,omitempty"`
	Hypervisor             string                `json:"hypervisor,omitempty"`
	Type                   string                `json:"type,omitempty"`
	Zone                   string                `json:"zone,omitempty"`
	ZoneID                 string                `json:"zoneId,omitempty"`
	Pod                    string                `json:"pod,omitempty"`
	PodID                  string                `json:"podId,omitempty"`
	Cluster                string                `json:"cluster,omitempty"`
	ClusterID              string                `json:"clusterId,omitempty"`
	CPUAllocated           string                `json:"cpuAllocated,omitempty"`
	CPUTotal               string                `json:"cpuTotal,omitempty"`
	CPUAllocatedPercent    string                `json:"cpuAllocatedPercent,omitempty"`
	MemoryAllocated        string                `json:"memoryAllocated,omitempty"`
	MemoryTotal            string                `json:"memoryTotal,omitempty"`
	MemoryAllocatedPercent string                `json:"memoryAllocatedPercent,omitempty"`
	StorageUsedPercent     string                `json:"storageUsedPercent,omitempty"`
	UserVMCount            *int                  `json:"userVmCount,omitempty"`
	RunningVMCount         *int                  `json:"runningVmCount,omitempty"`
	SystemVMCount          *int                  `json:"systemVmCount,omitempty"`
	VirtualRouterCount     *int                  `json:"virtualRouterCount,omitempty"`
	NetworkCount           *int                  `json:"networkCount,omitempty"`
	ConnectedVMs           []moldHostConnectedVM `json:"connectedVMs,omitempty"`
}

type moldHostConnectedVM struct {
	ID           string   `json:"id,omitempty"`
	UUID         string   `json:"uuid,omitempty"`
	Name         string   `json:"name,omitempty"`
	DisplayName  string   `json:"displayName,omitempty"`
	InstanceName string   `json:"instanceName,omitempty"`
	HostID       string   `json:"hostId,omitempty"`
	HostName     string   `json:"hostName,omitempty"`
	HostIP       string   `json:"hostIp,omitempty"`
	IPs          []string `json:"ips,omitempty"`
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
	regularUserVMs := userVMs
	classifiedSystemVMs := []interface{}{}
	if err == nil {
		regularUserVMs, classifiedSystemVMs = splitMoldUserAndSystemVMs(userVMs)
		count := countUniqueMoldItems(regularUserVMs)
		host.UserVMCount = &count
		host.RunningVMCount = &count
	}

	systemVMs, err := requestMoldItems(moldHostSystemVMListCommand, "systemvm", []apiParam{
		{Key: "command", Value: moldHostSystemVMListCommand},
		{Key: "response", Value: "json"},
		{Key: "hostid", Value: hostID},
	})
	if err == nil || len(classifiedSystemVMs) > 0 {
		count := countUniqueMoldItems(systemVMs, classifiedSystemVMs)
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
	collectMoldNetworkIDs(regularUserVMs, networks)
	collectMoldNetworkIDs(systemVMs, networks)
	collectMoldNetworkIDs(classifiedSystemVMs, networks)
	collectMoldNetworkIDs(routers, networks)
	if len(networks) > 0 {
		count := len(networks)
		host.NetworkCount = &count
	}

	host.ConnectedVMs = collectMoldHostConnectedVMs(regularUserVMs, systemVMs, classifiedSystemVMs, routers)

	return nil
}

func collectMoldHostConnectedVMs(groups ...[]interface{}) []moldHostConnectedVM {
	seen := make(map[string]struct{})
	vms := make([]moldHostConnectedVM, 0)
	for _, group := range groups {
		for _, item := range group {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			vm := moldHostConnectedVM{
				ID:           firstNonEmptyString(moldHostValueAsString(m["id"]), moldHostValueAsString(m["vmid"])),
				UUID:         firstNonEmptyString(moldHostValueAsString(m["uuid"]), moldHostValueAsString(m["id"])),
				Name:         moldHostValueAsString(m["name"]),
				DisplayName:  firstNonEmptyString(moldHostValueAsString(m["displayname"]), moldHostValueAsString(m["displayName"])),
				InstanceName: firstNonEmptyString(moldHostValueAsString(m["instancename"]), moldHostValueAsString(m["instanceName"])),
				HostID:       firstNonEmptyString(moldHostValueAsString(m["hostid"]), moldHostValueAsString(m["hostId"])),
				HostName:     firstNonEmptyString(moldHostValueAsString(m["hostname"]), moldHostValueAsString(m["hostName"]), moldHostValueAsString(m["host"])),
				HostIP:       firstNonEmptyString(moldHostValueAsString(m["hostip"]), moldHostValueAsString(m["hostIp"])),
				IPs:          collectMoldVMIPs(m),
			}
			key := firstNonEmptyString(vm.ID, vm.UUID, vm.InstanceName, vm.Name, vm.DisplayName)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			vms = append(vms, vm)
		}
	}
	return vms
}

func collectMoldVMIPs(m map[string]interface{}) []string {
	seen := make(map[string]struct{})
	ips := make([]string, 0)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		ips = append(ips, value)
	}
	add(firstNonEmptyString(moldHostValueAsString(m["ipaddress"]), moldHostValueAsString(m["ipAddress"])))
	collectMoldVMIPsFromValue(m["nic"], add)
	collectMoldVMIPsFromValue(m["nics"], add)
	return ips
}

func collectMoldVMIPsFromValue(value interface{}, add func(string)) {
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				collectMoldVMIPsFromMap(m, add)
			}
		}
	case map[string]interface{}:
		collectMoldVMIPsFromMap(v, add)
	}
}

func collectMoldVMIPsFromMap(m map[string]interface{}, add func(string)) {
	add(firstNonEmptyString(moldHostValueAsString(m["ipaddress"]), moldHostValueAsString(m["ipAddress"])))
	add(firstNonEmptyString(moldHostValueAsString(m["secondaryip"]), moldHostValueAsString(m["secondaryIp"])))
	if secondary, ok := m["secondaryip"].([]interface{}); ok {
		for _, item := range secondary {
			if sm, ok := item.(map[string]interface{}); ok {
				add(firstNonEmptyString(moldHostValueAsString(sm["ipaddress"]), moldHostValueAsString(sm["ipAddress"])))
			}
		}
	}
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

func splitMoldUserAndSystemVMs(items []interface{}) ([]interface{}, []interface{}) {
	regular := make([]interface{}, 0, len(items))
	systemLike := make([]interface{}, 0)
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if ok && isMoldSystemLikeVM(m) {
			systemLike = append(systemLike, item)
			continue
		}
		regular = append(regular, item)
	}
	return regular, systemLike
}

func isMoldSystemLikeVM(m map[string]interface{}) bool {
	text := strings.ToLower(strings.Join([]string{
		moldHostValueAsString(m["name"]),
		moldHostValueAsString(m["displayname"]),
		moldHostValueAsString(m["displayName"]),
		moldHostValueAsString(m["instancename"]),
		moldHostValueAsString(m["instanceName"]),
		moldHostValueAsString(m["hostname"]),
		moldHostValueAsString(m["serviceofferingname"]),
		moldHostValueAsString(m["serviceOfferingName"]),
		moldHostValueAsString(m["templatename"]),
		moldHostValueAsString(m["templateName"]),
		moldHostValueAsString(m["type"]),
	}, " "))
	if strings.Contains(text, "router") || strings.Contains(text, "domain router") || strings.Contains(text, "virtual router") {
		return false
	}
	patterns := []string{
		"scvm",
		"storage controller",
		"glue storage",
		"storage vm",
		"system vm",
		"systemvm",
		"secondary storage",
		"console proxy",
		"cpvm",
		"ssvm",
	}
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func countUniqueMoldItems(groups ...[]interface{}) int {
	seen := make(map[string]struct{})
	count := 0
	for _, group := range groups {
		for _, item := range group {
			m, ok := item.(map[string]interface{})
			if !ok {
				count++
				continue
			}
			key := firstNonEmptyString(
				moldHostValueAsString(m["id"]),
				moldHostValueAsString(m["uuid"]),
				moldHostValueAsString(m["name"]),
				moldHostValueAsString(m["instancename"]),
				moldHostValueAsString(m["instanceName"]),
			)
			if key == "" {
				count++
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			count++
		}
	}
	return count
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
