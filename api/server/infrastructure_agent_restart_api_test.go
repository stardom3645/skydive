package server

import "testing"

func TestSelectInfrastructureAgentHosts(t *testing.T) {
	hosts := []moldHostDetail{
		{ID: "1", UUID: "host-a", Name: "alpha", ManagementIP: "10.0.0.1"},
		{ID: "2", UUID: "host-b", Name: "beta", ManagementIP: "10.0.0.2"},
	}

	selected, err := selectInfrastructureAgentHosts(hosts, []string{"host-b"})
	if err != nil {
		t.Fatalf("unexpected selection error: %v", err)
	}
	if len(selected) != 1 || selected[0].Name != "beta" {
		t.Fatalf("expected beta only, got %#v", selected)
	}

	if _, err := selectInfrastructureAgentHosts(hosts, []string{"missing"}); err == nil {
		t.Fatal("expected an error for a host id that Mold did not return")
	}
}

func TestSetInfrastructureAgentRestartFinishedClassifiesResult(t *testing.T) {
	infrastructureAgentRestartState.Lock()
	infrastructureAgentRestartState.status = infrastructureAgentRestartStatus{Running: true, LastResult: "running"}
	infrastructureAgentRestartState.Unlock()

	status := setInfrastructureAgentRestartFinished([]infrastructureAgentRestartTargetResult{
		{ID: "host-a", Name: "alpha", Success: true},
		{ID: "host-b", Name: "beta", Error: "ssh failed"},
	})
	if status.Running {
		t.Fatal("restart status must no longer be running after results are recorded")
	}
	if status.LastResult != "partial" || status.Total != 2 || status.Succeeded != 1 || status.Failed != 1 {
		t.Fatalf("unexpected partial result summary: %#v", status)
	}
}
