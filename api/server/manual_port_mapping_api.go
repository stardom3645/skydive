// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	auth "github.com/abbot/go-http-auth"
	"github.com/gorilla/mux"

	"github.com/skydive-project/skydive/graffiti/graph"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/rbac"
	netdivedb "github.com/skydive-project/skydive/netdive/database"
	"github.com/skydive-project/skydive/topology"
)

const manualPortMappingObject = "manual-port-mapping"

type manualPortMappingAPI struct {
	db    *netdivedb.Database
	graph *graph.Graph
}

type manualPortMappingRequest struct {
	SwitchNodeID     string `json:"switchNodeId"`
	SwitchPortNodeID string `json:"switchPortNodeId"`
	HostNodeID       string `json:"hostNodeId"`
	HostNICNodeID    string `json:"hostNicNodeId"`
	Enabled          *bool  `json:"enabled,omitempty"`
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
		writeManualPortMappingError(w, http.StatusUnprocessableEntity, err.Error())
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
		writeManualPortMappingError(w, http.StatusUnprocessableEntity, err.Error())
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
	request.SwitchPortNodeID = strings.TrimSpace(request.SwitchPortNodeID)
	request.HostNodeID = strings.TrimSpace(request.HostNodeID)
	request.HostNICNodeID = strings.TrimSpace(request.HostNICNodeID)
	if request.SwitchNodeID == "" || request.SwitchPortNodeID == "" || request.HostNodeID == "" || request.HostNICNodeID == "" {
		return request, errors.New("switchNodeId, switchPortNodeId, hostNodeId, hostNicNodeId가 모두 필요합니다")
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
	switchPort := a.graph.GetNode(graph.Identifier(request.SwitchPortNodeID))
	hostNode := a.graph.GetNode(graph.Identifier(request.HostNodeID))
	hostNIC := a.graph.GetNode(graph.Identifier(request.HostNICNodeID))
	if switchNode == nil {
		return netdivedb.ManualPortMapping{}, fmt.Errorf("스위치 topology node를 찾을 수 없습니다: %s", request.SwitchNodeID)
	}
	if switchPort == nil {
		return netdivedb.ManualPortMapping{}, fmt.Errorf("스위치 포트 topology node를 찾을 수 없습니다: %s", request.SwitchPortNodeID)
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
	if portType := nodeType(switchPort); portType != "switchport" && portType != "port" {
		return netdivedb.ManualPortMapping{}, errors.New("switchPortNodeId가 스위치 포트 node를 가리키지 않습니다")
	}
	if nodeType(hostNode) != "host" {
		return netdivedb.ManualPortMapping{}, errors.New("hostNodeId가 호스트 node를 가리키지 않습니다")
	}
	if !isPhysicalHostNIC(hostNIC) {
		return netdivedb.ManualPortMapping{}, errors.New("hostNicNodeId가 물리 호스트 NIC node를 가리키지 않습니다")
	}
	if !isOwnershipDescendant(a.graph, switchNode, switchPort) {
		return netdivedb.ManualPortMapping{}, errors.New("스위치 포트가 지정한 스위치에 속하지 않습니다")
	}
	if !isOwnershipDescendant(a.graph, hostNode, hostNIC) {
		return netdivedb.ManualPortMapping{}, errors.New("호스트 NIC가 지정한 호스트에 속하지 않습니다")
	}

	return netdivedb.ManualPortMapping{
		SwitchNodeID:     string(switchNode.ID),
		SwitchName:       nodeDisplayName(switchNode),
		SwitchPortNodeID: string(switchPort.ID),
		SwitchPortName:   nodeDisplayName(switchPort),
		HostNodeID:       string(hostNode.ID),
		HostName:         nodeDisplayName(hostNode),
		HostNICNodeID:    string(hostNIC.ID),
		HostNICName:      nodeDisplayName(hostNIC),
	}, nil
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
		writeManualPortMappingError(w, http.StatusConflict, "선택한 스위치 포트 또는 호스트 NIC에 활성 수동 매핑이 이미 있습니다.")
	default:
		writeManualPortMappingError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeManualPortMappingError(w http.ResponseWriter, status int, message string) {
	writeManualPortMappingJSON(w, status, map[string]string{"message": message})
}

func writeManualPortMappingJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// RegisterManualPortMappingAPI registers the authenticated manual correction
// endpoints. Automatic LLDP data remains exclusively in the topology graph.
func RegisterManualPortMappingAPI(httpServer *shttp.Server, authBackend shttp.AuthenticationBackend, db *netdivedb.Database, g *graph.Graph) {
	api := &manualPortMappingAPI{db: db, graph: g}
	httpServer.RegisterRoutes([]shttp.Route{
		{Name: "ManualPortMappingList", Method: "GET", Path: "/api/infrastructure/manual-port-mappings", HandlerFunc: api.list},
		{Name: "ManualPortMappingCreate", Method: "POST", Path: "/api/infrastructure/manual-port-mappings", HandlerFunc: api.create},
		{Name: "ManualPortMappingUpdate", Method: "PUT", Path: "/api/infrastructure/manual-port-mappings/{id:[0-9]+}", HandlerFunc: api.update},
		{Name: "ManualPortMappingDelete", Method: "DELETE", Path: "/api/infrastructure/manual-port-mappings/{id:[0-9]+}", HandlerFunc: api.disable},
	}, authBackend)
}
