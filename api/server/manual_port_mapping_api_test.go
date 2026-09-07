// Copyright (C) 2026 ABLESTACK
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	auth "github.com/abbot/go-http-auth"
	"github.com/gorilla/mux"

	"github.com/skydive-project/skydive/graffiti/graph"
	netdivedb "github.com/skydive-project/skydive/netdive/database"
	"github.com/skydive-project/skydive/statics"
	"github.com/skydive-project/skydive/topology"
)

type manualPortMappingAPIFixture struct {
	api   *manualPortMappingAPI
	db    *netdivedb.Database
	graph *graph.Graph
}

func newManualPortMappingAPIFixture(t *testing.T) *manualPortMappingAPIFixture {
	t.Helper()
	backend, err := graph.NewMemoryBackend()
	if err != nil {
		t.Fatal(err)
	}
	g := graph.NewGraph("test-host", backend, "test")
	nodes := []struct {
		id       graph.Identifier
		metadata graph.Metadata
	}{
		{"switch-1", graph.Metadata{"Type": "switch", "Name": "Switch 1"}},
		{"port-1", graph.Metadata{"Type": "switchport", "Name": "Ethernet1"}},
		{"port-2", graph.Metadata{"Type": "switchport", "Name": "Ethernet2"}},
		{"host-1", graph.Metadata{"Type": "host", "Name": "Host 1"}},
		{"nic-1", graph.Metadata{"Type": "device", "Name": "eno1"}},
		{"nic-2", graph.Metadata{"Type": "device", "Name": "eno2"}},
	}
	created := make(map[graph.Identifier]*graph.Node)
	for _, item := range nodes {
		node, err := g.NewNode(item.id, item.metadata)
		if err != nil {
			t.Fatal(err)
		}
		created[item.id] = node
	}
	for _, relation := range [][2]graph.Identifier{
		{"switch-1", "port-1"},
		{"switch-1", "port-2"},
		{"host-1", "nic-1"},
		{"host-1", "nic-2"},
	} {
		if _, err := topology.AddOwnershipLink(g, created[relation[0]], created[relation[1]], nil); err != nil {
			t.Fatal(err)
		}
	}
	db, err := netdivedb.Open(context.Background(), netdivedb.Config{
		Driver: "sqlite3", Path: filepath.Join(t.TempDir(), "netdive.db"), JournalMode: "WAL", BusyTimeout: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &manualPortMappingAPIFixture{api: &manualPortMappingAPI{db: db, graph: g}, db: db, graph: g}
}

func authenticatedManualPortMappingRequest(method, target string, body interface{}) *auth.AuthenticatedRequest {
	var payload *bytes.Reader
	if body == nil {
		payload = bytes.NewReader(nil)
	} else {
		encoded, _ := json.Marshal(body)
		payload = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, target, payload)
	return &auth.AuthenticatedRequest{Request: *request, Username: "admin"}
}

func TestManualPortMappingAPICRUD(t *testing.T) {
	fixture := newManualPortMappingAPIFixture(t)
	body := manualPortMappingRequest{
		SwitchNodeID: "switch-1", SwitchPortNodeID: "port-1",
		HostNodeID: "host-1", HostNICNodeID: "nic-1",
	}

	createRecorder := httptest.NewRecorder()
	fixture.api.create(createRecorder, authenticatedManualPortMappingRequest(http.MethodPost, "/api/infrastructure/manual-port-mappings", body))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRecorder.Code, createRecorder.Body.String())
	}
	var created manualPortMappingResponse
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Mapping.ID <= 0 || created.Mapping.SwitchName != "Switch 1" || created.Mapping.HostNICName != "eno1" || !created.Mapping.Enabled {
		t.Fatalf("unexpected created mapping: %+v", created.Mapping)
	}

	listRecorder := httptest.NewRecorder()
	fixture.api.list(listRecorder, authenticatedManualPortMappingRequest(http.MethodGet, "/api/infrastructure/manual-port-mappings?switchNodeId=switch-1", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRecorder.Code, listRecorder.Body.String())
	}
	var listed manualPortMappingsResponse
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Mappings) != 1 {
		t.Fatalf("listed mappings = %+v", listed.Mappings)
	}

	body.SwitchPortNodeID = "port-2"
	body.HostNICNodeID = "nic-2"
	updateRequest := authenticatedManualPortMappingRequest(http.MethodPut, "/api/infrastructure/manual-port-mappings/1", body)
	updateRequest.Request = *mux.SetURLVars(&updateRequest.Request, map[string]string{"id": "1"})
	updateRecorder := httptest.NewRecorder()
	fixture.api.update(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateRecorder.Code, updateRecorder.Body.String())
	}

	deleteRequest := authenticatedManualPortMappingRequest(http.MethodDelete, "/api/infrastructure/manual-port-mappings/1", nil)
	deleteRequest.Request = *mux.SetURLVars(&deleteRequest.Request, map[string]string{"id": "1"})
	deleteRecorder := httptest.NewRecorder()
	fixture.api.disable(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	active, err := fixture.db.ListManualPortMappings(context.Background(), netdivedb.ManualPortMappingFilter{})
	if err != nil || len(active) != 0 {
		t.Fatalf("active mappings = %+v, err = %v", active, err)
	}
	all, err := fixture.db.ListManualPortMappings(context.Background(), netdivedb.ManualPortMappingFilter{IncludeDisabled: true})
	if err != nil || len(all) != 1 || all[0].Enabled {
		t.Fatalf("all mappings = %+v, err = %v", all, err)
	}
}

func TestManualPortMappingAPIRejectsInvalidTopologyAndConflict(t *testing.T) {
	fixture := newManualPortMappingAPIFixture(t)
	body := manualPortMappingRequest{
		SwitchNodeID: "switch-1", SwitchPortNodeID: "port-1",
		HostNodeID: "host-1", HostNICNodeID: "nic-1",
	}
	first := httptest.NewRecorder()
	fixture.api.create(first, authenticatedManualPortMappingRequest(http.MethodPost, "/api/infrastructure/manual-port-mappings", body))
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, body = %s", first.Code, first.Body.String())
	}

	body.HostNICNodeID = "nic-2"
	conflict := httptest.NewRecorder()
	fixture.api.create(conflict, authenticatedManualPortMappingRequest(http.MethodPost, "/api/infrastructure/manual-port-mappings", body))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, body = %s", conflict.Code, conflict.Body.String())
	}

	body.SwitchPortNodeID = "nic-2"
	invalid := httptest.NewRecorder()
	fixture.api.create(invalid, authenticatedManualPortMappingRequest(http.MethodPost, "/api/infrastructure/manual-port-mappings", body))
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid topology status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
}

func TestManualPortMappingAPIRejectsAutomaticallyMappedPort(t *testing.T) {
	fixture := newManualPortMappingAPIFixture(t)
	port := fixture.graph.GetNode(graph.Identifier("port-1"))
	nic := fixture.graph.GetNode(graph.Identifier("nic-1"))
	if _, err := topology.AddLayer2Link(fixture.graph, port, nic, nil); err != nil {
		t.Fatal(err)
	}

	body := manualPortMappingRequest{
		SwitchNodeID: "switch-1", SwitchPortNodeID: "port-1",
		HostNodeID: "host-1", HostNICNodeID: "nic-1",
	}
	recorder := httptest.NewRecorder()
	fixture.api.create(recorder, authenticatedManualPortMappingRequest(http.MethodPost, "/api/infrastructure/manual-port-mappings", body))
	if recorder.Code != http.StatusConflict {
		t.Fatalf("automatic mapping conflict status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "LLDP") {
		t.Fatalf("automatic mapping conflict body = %s", recorder.Body.String())
	}
}

func TestManualPortMappingRBACPolicyIsBundled(t *testing.T) {
	policy, err := statics.Asset("rbac/policy.csv")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"p, admin, manual-port-mapping, read, allow",
		"p, admin, manual-port-mapping, write, allow",
		"p, guest, manual-port-mapping, read, deny",
		"p, guest, manual-port-mapping, write, deny",
	} {
		if !strings.Contains(string(policy), line) {
			t.Errorf("bundled RBAC policy is missing %q", line)
		}
	}
}
