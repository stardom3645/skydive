package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMoldSystemVMExactIdentityAndServiceFields(t *testing.T) {
	items := []interface{}{
		map[string]interface{}{"id": "storage-id", "name": "s-173-VM", "systemvmtype": "secondarystoragevm", "state": "Running", "agentstate": "Up", "activeviewersessions": 100},
		map[string]interface{}{"id": "console-id", "name": "v-174-VM", "systemvmtype": "consoleproxy", "state": "Running", "agentstate": "Disconnected", "activeviewersessions": json.Number("0")},
		map[string]interface{}{"id": "other-id", "name": "v-174-VM-copy", "systemvmtype": "consoleproxy", "activeviewersessions": 12},
	}
	vm, err := pickMoldSystemVMDetail(items, "console-id", "wrong-name")
	if err != nil || vm == nil || vm.Name != "v-174-VM" || vm.AgentState != "Disconnected" || vm.State != "Running" || vm.ActiveViewerSessions == nil || *vm.ActiveViewerSessions != 0 {
		t.Fatalf("unexpected console metadata: %#v, %v", vm, err)
	}
	vm, err = pickMoldSystemVMDetail(items, "missing-id", "v-174-VM")
	if err != nil || vm != nil {
		t.Fatal("UUID mismatch must never fall back to a matching name")
	}
	vm, err = pickMoldSystemVMDetail(items, "", "v-174")
	if err != nil || vm != nil {
		t.Fatal("substring names must not select a VM")
	}
	vm, err = pickMoldSystemVMDetail(items, "", "s-173-VM")
	if err != nil || vm == nil || vm.ActiveViewerSessions != nil || vm.SystemVMType != "secondarystoragevm" {
		t.Fatal("storage VMs must not have console sessions")
	}
	_, err = pickMoldSystemVMDetail(append(items, items[1]), "console-id", "")
	if err == nil {
		t.Fatal("ambiguous identity must be rejected")
	}
}

func TestMoldSystemVMUnavailableSessions(t *testing.T) {
	for _, value := range []interface{}{nil, "", true, -1, 1.5, "bad", "9007199254740992"} {
		vm, err := pickMoldSystemVMDetail([]interface{}{map[string]interface{}{
			"id": "id", "name": "v-1-VM", "systemvmtype": "consoleproxy", "activeviewersessions": value,
		}}, "id", "")
		if err != nil || vm == nil || vm.ActiveViewerSessions != nil {
			t.Fatalf("invalid count must remain absent: %v, %#v, %v", value, vm, err)
		}
	}
	vm, err := pickMoldSystemVMDetail([]interface{}{map[string]interface{}{
		"id": "id", "name": "r-1-VM", "systemvmtype": "domainrouter",
	}}, "id", "")
	if err != nil || vm != nil {
		t.Fatal("router metadata must not enter system service cards")
	}
}

func TestMoldSystemVMDetailRequiresIdentity(t *testing.T) {
	w := httptest.NewRecorder()
	handleMoldSystemVMDetail(w, httptest.NewRequest(http.MethodGet, "/api/mold/system-vms/detail", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected identity validation: %d", w.Code)
	}
}
