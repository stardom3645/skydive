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

func TestFilterInfrastructureAgentHostsExcludesSystemVMHosts(t *testing.T) {
	hosts := []moldHostDetail{
		{ID: "host-1", Name: "ablecube22-1", Type: "Routing", Hypervisor: "KVM", ClusterID: "cluster-1", ManagementIP: "10.0.0.11"},
		{ID: "host-2", Name: "ablecube22-2", Type: "Routing", Hypervisor: "KVM", ClusterID: "cluster-1", ManagementIP: "10.0.0.12"},
		{ID: "ssvm", Name: "s-595-VM", Type: "SecondaryStorageVM", ManagementIP: "10.0.0.21"},
		{ID: "cpvm", Name: "v-23-VM", Type: "ConsoleProxy", ManagementIP: "10.0.0.22"},
		{ID: "missing-ip", Name: "unreachable-host", Type: "Routing", Hypervisor: "KVM", ClusterID: "cluster-1"},
		// Older responses can omit type after a type=Routing query. Preserve a
		// record only when routing host fields are still present.
		{ID: "legacy-host", Name: "ablecube22-3", Hypervisor: "KVM", ClusterID: "cluster-1", ManagementIP: "10.0.0.13"},
		{ID: "duplicate", UUID: "host-1", Name: "duplicate-host", Type: "Routing", ManagementIP: "10.0.0.11"},
	}

	filtered := filterInfrastructureAgentHosts(hosts)
	if len(filtered) != 3 {
		t.Fatalf("filtered hosts=%#v, want three routing Agent targets", filtered)
	}
	for _, host := range filtered {
		if host.Name == "s-595-VM" || host.Name == "v-23-VM" || host.Name == "unreachable-host" {
			t.Fatalf("non-Agent target leaked into restart list: %#v", host)
		}
	}
	allTargets, err := selectInfrastructureAgentHosts(filtered, nil)
	if err != nil || len(allTargets) != 3 {
		t.Fatalf("all-host restart targets=%#v err=%v, want the same three filtered routing hosts", allTargets, err)
	}
	if _, err := selectInfrastructureAgentHosts(filtered, []string{"ssvm"}); err == nil {
		t.Fatal("an explicitly requested system VM must not bypass the Agent-host filter")
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
