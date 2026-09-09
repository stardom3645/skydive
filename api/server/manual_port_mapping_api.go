// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	auth "github.com/abbot/go-http-auth"
	"github.com/gorilla/mux"

	"github.com/skydive-project/skydive/graffiti/graph"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/logging"
	"github.com/skydive-project/skydive/graffiti/rbac"
	netdivedb "github.com/skydive-project/skydive/netdive/database"
	"github.com/skydive-project/skydive/topology"
)

const manualPortMappingObject = "manual-port-mapping"

var (
	errManualPortAutomaticallyMapped = errors.New("switch port already has an automatic topology relation")
	errHostNICAutomaticallyMapped    = errors.New("host NIC already has an automatic topology relation")
)

type manualPortMappingAPI struct {
	db    *netdivedb.Database
	graph *graph.Graph
}

type manualPortMappingReconciler struct {
	graph.DefaultGraphListener
	db    *netdivedb.Database
	graph *graph.Graph
}

func (r *manualPortMappingReconciler) reconcile() {
	mappings, err := r.db.ListManualPortMappings(context.Background(), netdivedb.ManualPortMappingFilter{})
	if err != nil {
		logging.GetLogger().Errorf("Failed to reconcile manual port mappings with LLDP: %s", err)
		return
	}
	for _, mapping := range mappings {
		reason := manualPortMappingLLDPDisableReason(r.graph, mapping)
		if reason == "" {
			continue
		}
		if err := r.db.SupersedeManualPortMappingByLLDP(context.Background(), mapping.ID, reason); err != nil {
			logging.GetLogger().Errorf("Failed to supersede manual port mapping %d with LLDP: %s", mapping.ID, err)
			continue
		}
		logging.GetLogger().Infof("Disabled manual port mapping %d because an LLDP automatic relation is now available (%s)", mapping.ID, reason)
	}
}

// manualPortMappingLLDPDisableReason classifies the AUTO takeover. AUTO always
// wins, but retaining the distinction between a confirmed relation and a
// physical endpoint conflict makes the transition auditable after restart.
func manualPortMappingLLDPDisableReason(g *graph.Graph, mapping netdivedb.ManualPortMapping) string {
	switchNode := g.GetNode(graph.Identifier(mapping.SwitchNodeID))
	hostNIC := g.GetNode(graph.Identifier(mapping.HostNICNodeID))
	automaticNICs := make(map[graph.Identifier]struct{})
	portConflict := false
	if switchNode != nil {
		automaticNICs, portConflict = automaticPortRelationForName(g, switchNode, mapping.SwitchPortName)
	}

	if hostNIC != nil {
		if _, exactMatch := automaticNICs[hostNIC.ID]; exactMatch {
			return netdivedb.ManualPortMappingDisabledByLLDPMatch
		}
	}
	nicConflict := hostNIC != nil && hasAutomaticHostNICRelation(g, hostNIC)
	switch {
	case portConflict && nicConflict:
		return netdivedb.ManualPortMappingDisabledByLLDPConflict
	case portConflict:
		return netdivedb.ManualPortMappingDisabledByLLDPPortConflict
	case nicConflict:
		return netdivedb.ManualPortMappingDisabledByLLDPNICConflict
	default:
		return ""
	}
}

func (r *manualPortMappingReconciler) OnEdgeAdded(edge *graph.Edge) {
	relationType, _ := edge.GetFieldString("RelationType")
	if !strings.EqualFold(strings.TrimSpace(relationType), topology.OwnershipLink) {
		r.reconcile()
	}
}

func (r *manualPortMappingReconciler) OnEdgeUpdated(edge *graph.Edge, _ []graph.PartiallyUpdatedOp) {
	r.OnEdgeAdded(edge)
}

type manualPortMappingRequest struct {
	SwitchNodeID   string `json:"switchNodeId"`
	SwitchPortName string `json:"switchPortName"`
	HostNodeID     string `json:"hostNodeId"`
	HostNICNodeID  string `json:"hostNicNodeId"`
	Enabled        *bool  `json:"enabled,omitempty"`
}

type manualPortMappingsResponse struct {
	Mappings []netdivedb.ManualPortMapping `json:"mappings"`
}

type manualPortMappingResponse struct {
	Mapping netdivedb.ManualPortMapping `json:"mapping"`
}

func (a *manualPortMappingAPI) list(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, manualPortMappingObject, "read") {
		writeManualPortMappingError(w, http.StatusForbidden, "관리 권한이 필요합니다.")
		return
	}
	filter := netdivedb.ManualPortMappingFilter{
		SwitchNodeID:    strings.TrimSpace(r.URL.Query().Get("switchNodeId")),
		HostNodeID:      strings.TrimSpace(r.URL.Query().Get("hostNodeId")),
		HostNICNodeID:   strings.TrimSpace(r.URL.Query().Get("hostNicNodeId")),
		IncludeDisabled: strings.EqualFold(r.URL.Query().Get("includeDisabled"), "true"),
	}
	mappings, err := a.db.ListManualPortMappings(r.Context(), filter)
	if err != nil {
		writeManualPortMappingError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeManualPortMappingJSON(w, http.StatusOK, manualPortMappingsResponse{Mappings: mappings})
}

func (a *manualPortMappingAPI) create(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, manualPortMappingObject, "write") {
		writeManualPortMappingError(w, http.StatusForbidden, "관리 권한이 필요합니다.")
		return
	}
	request, err := decodeManualPortMappingRequest(r)
	if err != nil {
		writeManualPortMappingError(w, http.StatusBadRequest, err.Error())
		return
	}
	mapping, err := a.mappingFromTopology(request)
	if err != nil {
		a.writeTopologyValidationError(w, err)
		return
	}
	mapping.Enabled = true
	if request.Enabled != nil {
		mapping.Enabled = *request.Enabled
	}
	mapping, err = a.db.CreateManualPortMapping(r.Context(), mapping)
	if err != nil {
		a.writeDatabaseError(w, err)
		return
	}
	writeManualPortMappingJSON(w, http.StatusCreated, manualPortMappingResponse{Mapping: mapping})
}

func (a *manualPortMappingAPI) update(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, manualPortMappingObject, "write") {
		writeManualPortMappingError(w, http.StatusForbidden, "관리 권한이 필요합니다.")
		return
	}
	id, err := manualPortMappingID(r)
	if err != nil {
		writeManualPortMappingError(w, http.StatusBadRequest, err.Error())
		return
	}
	request, err := decodeManualPortMappingRequest(r)
	if err != nil {
		writeManualPortMappingError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := a.db.GetManualPortMapping(r.Context(), id)
	if err != nil {
		a.writeDatabaseError(w, err)
		return
	}
	mapping, err := a.mappingFromTopology(request)
	if err != nil {
		a.writeTopologyValidationError(w, err)
		return
	}
	mapping.ID = id
	mapping.Enabled = current.Enabled
	if request.Enabled != nil {
		mapping.Enabled = *request.Enabled
	}
	mapping, err = a.db.UpdateManualPortMapping(r.Context(), mapping)
	if err != nil {
		a.writeDatabaseError(w, err)
		return
	}
	writeManualPortMappingJSON(w, http.StatusOK, manualPortMappingResponse{Mapping: mapping})
}

func (a *manualPortMappingAPI) disable(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if !rbac.Enforce(r.Username, manualPortMappingObject, "write") {
		writeManualPortMappingError(w, http.StatusForbidden, "관리 권한이 필요합니다.")
		return
	}
	id, err := manualPortMappingID(r)
	if err != nil {
		writeManualPortMappingError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.DisableManualPortMapping(r.Context(), id); err != nil {
		a.writeDatabaseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeManualPortMappingRequest(r *auth.AuthenticatedRequest) (manualPortMappingRequest, error) {
	var request manualPortMappingRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fmt.Errorf("요청 형식이 올바르지 않습니다: %v", err)
	}
	request.SwitchNodeID = strings.TrimSpace(request.SwitchNodeID)
	request.SwitchPortName = strings.TrimSpace(request.SwitchPortName)
	request.HostNodeID = strings.TrimSpace(request.HostNodeID)
	request.HostNICNodeID = strings.TrimSpace(request.HostNICNodeID)
	if request.SwitchNodeID == "" || request.SwitchPortName == "" || request.HostNodeID == "" || request.HostNICNodeID == "" {
		return request, errors.New("switchNodeId, switchPortName, hostNodeId, hostNicNodeId가 모두 필요합니다")
	}
	if utf8.RuneCountInString(request.SwitchPortName) > 255 {
		return request, errors.New("스위치 포트명은 255자를 초과할 수 없습니다")
	}
	if strings.IndexFunc(request.SwitchPortName, unicode.IsControl) >= 0 {
		return request, errors.New("스위치 포트명에는 제어 문자를 사용할 수 없습니다")
	}
	return request, nil
}

func manualPortMappingID(r *auth.AuthenticatedRequest) (int64, error) {
	id, err := strconv.ParseInt(mux.Vars(&r.Request)["id"], 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("올바른 수동 매핑 ID가 필요합니다")
	}
	return id, nil
}

func (a *manualPortMappingAPI) mappingFromTopology(request manualPortMappingRequest) (netdivedb.ManualPortMapping, error) {
	a.graph.RLock()
	defer a.graph.RUnlock()

	switchNode := a.graph.GetNode(graph.Identifier(request.SwitchNodeID))
	hostNode := a.graph.GetNode(graph.Identifier(request.HostNodeID))
	hostNIC := a.graph.GetNode(graph.Identifier(request.HostNICNodeID))
	if switchNode == nil {
		return netdivedb.ManualPortMapping{}, fmt.Errorf("스위치 topology node를 찾을 수 없습니다: %s", request.SwitchNodeID)
	}
	if hostNode == nil {
		return netdivedb.ManualPortMapping{}, fmt.Errorf("호스트 topology node를 찾을 수 없습니다: %s", request.HostNodeID)
	}
	if hostNIC == nil {
		return netdivedb.ManualPortMapping{}, fmt.Errorf("호스트 NIC topology node를 찾을 수 없습니다: %s", request.HostNICNodeID)
	}
	if nodeType(switchNode) != "switch" {
		return netdivedb.ManualPortMapping{}, errors.New("switchNodeId가 스위치 node를 가리키지 않습니다")
	}
	if nodeType(hostNode) != "host" {
		return netdivedb.ManualPortMapping{}, errors.New("hostNodeId가 호스트 node를 가리키지 않습니다")
	}
	if !isPhysicalHostNIC(hostNIC) {
		return netdivedb.ManualPortMapping{}, errors.New("hostNicNodeId가 물리 호스트 NIC node를 가리키지 않습니다")
	}
	if !isOwnershipDescendant(a.graph, hostNode, hostNIC) {
		return netdivedb.ManualPortMapping{}, errors.New("호스트 NIC가 지정한 호스트에 속하지 않습니다")
	}
	if hasAutomaticPortName(a.graph, switchNode, request.SwitchPortName) {
		return netdivedb.ManualPortMapping{}, fmt.Errorf("%w: %s", errManualPortAutomaticallyMapped, request.SwitchPortName)
	}
	if hasAutomaticHostNICRelation(a.graph, hostNIC) {
		return netdivedb.ManualPortMapping{}, fmt.Errorf("%w: %s", errHostNICAutomaticallyMapped, request.HostNICNodeID)
	}

	return netdivedb.ManualPortMapping{
		SwitchNodeID:   string(switchNode.ID),
		SwitchName:     nodeDisplayName(switchNode),
		SwitchPortName: request.SwitchPortName,
		HostNodeID:     string(hostNode.ID),
		HostName:       nodeDisplayName(hostNode),
		HostNICNodeID:  string(hostNIC.ID),
		HostNICName:    nodeDisplayName(hostNIC),
	}, nil
}

func hasAutomaticPortName(g *graph.Graph, switchNode *graph.Node, switchPortName string) bool {
	_, found := automaticPortRelationForName(g, switchNode, switchPortName)
	return found
}

func automaticPortRelationForName(g *graph.Graph, switchNode *graph.Node, switchPortName string) (map[graph.Identifier]struct{}, bool) {
	wanted := strings.TrimSpace(switchPortName)
	matches := make(map[graph.Identifier]struct{})
	found := false
	queue := g.LookupChildren(switchNode, nil, topology.OwnershipMetadata())
	visited := make(map[graph.Identifier]struct{})
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if _, seen := visited[current.ID]; seen {
			continue
		}
		visited[current.ID] = struct{}{}
		if portType := nodeType(current); (portType == "switchport" || portType == "port") &&
			strings.EqualFold(topologyPortName(current), wanted) {
			for _, edge := range g.GetNodeEdges(current, nil) {
				relationType, _ := edge.GetFieldString("RelationType")
				if strings.EqualFold(strings.TrimSpace(relationType), topology.OwnershipLink) {
					continue
				}
				found = true
				parents, children := g.GetEdgeNodes(edge, nil, nil)
				for _, peer := range append(parents, children...) {
					if peer.ID != current.ID && isPhysicalHostNIC(peer) {
						matches[peer.ID] = struct{}{}
					}
				}
			}
		}
		queue = append(queue, g.LookupChildren(current, nil, topology.OwnershipMetadata())...)
	}

	// Older LLDP graphs can link the switch directly to a host NIC and keep the
	// remote port identifier only in the NIC's LLDP metadata.
	for _, edge := range g.GetNodeEdges(switchNode, nil) {
		relationType, _ := edge.GetFieldString("RelationType")
		if strings.EqualFold(strings.TrimSpace(relationType), topology.OwnershipLink) {
			continue
		}
		parents, children := g.GetEdgeNodes(edge, nil, nil)
		for _, peer := range append(parents, children...) {
			if peer.ID == switchNode.ID {
				continue
			}
			if isPhysicalHostNIC(peer) && strings.EqualFold(lldpRemotePortName(peer), wanted) {
				found = true
				matches[peer.ID] = struct{}{}
			}
		}
	}
	return matches, found
}

func topologyPortName(node *graph.Node) string {
	for _, field := range []string{"Name", "PortID", "PortId", "IfName", "InterfaceName"} {
		if value, err := node.GetFieldString(field); err == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return string(node.ID)
}

func lldpRemotePortName(node *graph.Node) string {
	for _, field := range []string{
		"LLDP.RemotePortID", "LLDP.RemotePortId", "LLDP.PortID", "LLDP.PortId", "LLDP.RemotePortDescription",
		"RemotePortID", "RemotePortId", "PortID", "PortId", "RemotePortDescription",
	} {
		if value, err := node.GetFieldString(field); err == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func hasAutomaticHostNICRelation(g *graph.Graph, hostNIC *graph.Node) bool {
	for _, edge := range g.GetNodeEdges(hostNIC, nil) {
		relationType, _ := edge.GetFieldString("RelationType")
		if strings.EqualFold(strings.TrimSpace(relationType), topology.OwnershipLink) {
			continue
		}
		parents, children := g.GetEdgeNodes(edge, nil, nil)
		for _, peer := range append(parents, children...) {
			if peer.ID == hostNIC.ID {
				continue
			}
			if nodeType(peer) == "switch" || isSwitchPortOwnedBySwitch(g, peer) {
				return true
			}
		}
	}
	return false
}

func isSwitchPortOwnedBySwitch(g *graph.Graph, node *graph.Node) bool {
	portType := nodeType(node)
	if portType != "switchport" && portType != "port" {
		return false
	}
	visited := make(map[graph.Identifier]struct{})
	queue := g.LookupParents(node, nil, topology.OwnershipMetadata())
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if _, seen := visited[current.ID]; seen {
			continue
		}
		visited[current.ID] = struct{}{}
		if nodeType(current) == "switch" {
			return true
		}
		queue = append(queue, g.LookupParents(current, nil, topology.OwnershipMetadata())...)
	}
	return false
}

func nodeType(node *graph.Node) string {
	value, _ := node.GetFieldString("Type")
	return strings.ToLower(strings.TrimSpace(value))
}

func nodeDisplayName(node *graph.Node) string {
	value, _ := node.GetFieldString("Name")
	if value == "" {
		return string(node.ID)
	}
	return value
}

func isPhysicalHostNIC(node *graph.Node) bool {
	switch nodeType(node) {
	case "device", "nic", "interface", "ethernet":
		return true
	default:
		return false
	}
}

func isOwnershipDescendant(g *graph.Graph, ancestor, node *graph.Node) bool {
	visited := make(map[graph.Identifier]struct{})
	queue := []*graph.Node{node}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if _, seen := visited[current.ID]; seen {
			continue
		}
		visited[current.ID] = struct{}{}
		for _, parent := range g.LookupParents(current, nil, topology.OwnershipMetadata()) {
			if parent.ID == ancestor.ID {
				return true
			}
			queue = append(queue, parent)
		}
	}
	return false
}

func (a *manualPortMappingAPI) writeDatabaseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, netdivedb.ErrManualPortMappingNotFound):
		writeManualPortMappingError(w, http.StatusNotFound, "수동 매핑을 찾을 수 없습니다.")
	case errors.Is(err, netdivedb.ErrManualPortMappingConflict):
		writeManualPortMappingError(w, http.StatusConflict, "동일한 스위치 포트명 또는 호스트 NIC에 활성 수동 매핑이 이미 있습니다.")
	case errors.Is(err, netdivedb.ErrManualPortMappingInactive):
		writeManualPortMappingError(w, http.StatusConflict, "AUTO 전환 또는 삭제로 비활성화된 수동 매핑 이력은 수정할 수 없습니다.")
	default:
		writeManualPortMappingError(w, http.StatusInternalServerError, err.Error())
	}
}

func (a *manualPortMappingAPI) writeTopologyValidationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errManualPortAutomaticallyMapped) {
		writeManualPortMappingError(w, http.StatusConflict, "LLDP로 자동 연결된 스위치 포트는 수동 매핑할 수 없습니다.")
		return
	}
	if errors.Is(err, errHostNICAutomaticallyMapped) {
		writeManualPortMappingError(w, http.StatusConflict, "LLDP로 자동 연결된 호스트 NIC는 수동 매핑할 수 없습니다.")
		return
	}
	writeManualPortMappingError(w, http.StatusUnprocessableEntity, err.Error())
}

func writeManualPortMappingError(w http.ResponseWriter, status int, message string) {
	writeManualPortMappingJSON(w, status, map[string]string{"message": message})
}

func writeManualPortMappingJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// RegisterManualPortMappingAPI registers authenticated supplemental mapping
// endpoints. Automatic LLDP data remains exclusively in the topology graph.
func RegisterManualPortMappingAPI(httpServer *shttp.Server, authBackend shttp.AuthenticationBackend, db *netdivedb.Database, g *graph.Graph) func() {
	api := &manualPortMappingAPI{db: db, graph: g}
	httpServer.RegisterRoutes([]shttp.Route{
		{Name: "ManualPortMappingList", Method: "GET", Path: "/api/infrastructure/manual-port-mappings", HandlerFunc: api.list},
		{Name: "ManualPortMappingCreate", Method: "POST", Path: "/api/infrastructure/manual-port-mappings", HandlerFunc: api.create},
		{Name: "ManualPortMappingUpdate", Method: "PUT", Path: "/api/infrastructure/manual-port-mappings/{id:[0-9]+}", HandlerFunc: api.update},
		{Name: "ManualPortMappingDelete", Method: "DELETE", Path: "/api/infrastructure/manual-port-mappings/{id:[0-9]+}", HandlerFunc: api.disable},
	}, authBackend)
	reconciler := &manualPortMappingReconciler{db: db, graph: g}
	g.AddEventListener(reconciler)
	reconciler.reconcile()
	return func() { g.RemoveEventListener(reconciler) }
}
