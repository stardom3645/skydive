package lldp

import (
	"github.com/skydive-project/skydive/graffiti/graph"
	"github.com/skydive-project/skydive/topology"
	"testing"
	"time"
)

func leaseFixture(t *testing.T) (*graph.Graph, *graph.Node, *graph.Node, *graph.Node, *graph.Node) {
	t.Helper()
	backend, err := graph.NewMemoryBackend()
	if err != nil {
		t.Fatal(err)
	}
	g := graph.NewGraph("agent", backend, "test")
	create := func(id, kind string, metadata graph.Metadata) *graph.Node {
		metadata["Type"] = kind
		n, err := g.NewNode(graph.Identifier(id), metadata)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	root := create("host", "host", graph.Metadata{})
	nic := create("nic", "device", graph.Metadata{"State": "UP"})
	p1 := create("p1", "switchport", graph.Metadata{"LLDP": graph.Metadata{"PortID": "1"}})
	p2 := create("p2", "switchport", graph.Metadata{"LLDP": graph.Metadata{"PortID": "2"}})
	topology.AddOwnershipLink(g, root, nic, nil)
	return g, root, nic, p1, p2
}

func TestLLDPLeaseRefreshExpirationAndRecovery(t *testing.T) {
	g, root, nic, p1, _ := leaseFixture(t)
	now := time.Unix(1000, 0)
	observe := func(at time.Time) {
		if err := observeLLDPLink(g, root, p1, nic, 10, at); err != nil {
			t.Fatal(err)
		}
	}
	observe(now)
	original := g.GetNodeEdges(nic, topology.Layer2Metadata())[0].ID
	observe(now.Add(5 * time.Second))
	expireLLDPLinks(g, root, now.Add(11*time.Second), false)
	if len(g.GetNodeEdges(nic, topology.Layer2Metadata())) != 1 {
		t.Fatal("refreshed lease expired too early")
	}
	expireLLDPLinks(g, root, now.Add(15*time.Second), false)
	if topology.HaveLayer2Link(g, p1, nic) {
		t.Fatal("expired neighbor retained")
	}
	if state, _ := nic.GetFieldString("State"); state != "UP" {
		t.Fatal("expiry changed physical state")
	}
	observe(now.Add(20 * time.Second))
	observe(now.Add(21 * time.Second))
	edges := g.GetNodeEdges(nic, topology.Layer2Metadata())
	if len(edges) != 1 || edges[0].ID != original {
		t.Fatal("recovery created duplicate/unstable relation")
	}
}

func TestLLDPCableMoveWithdrawalAndRestart(t *testing.T) {
	g, root, nic, p1, p2 := leaseFixture(t)
	now := time.Unix(1000, 0)
	observeLLDPLink(g, root, p1, nic, 10, now)
	observeLLDPLink(g, root, p2, nic, 10, now.Add(time.Second))
	if topology.HaveLayer2Link(g, p1, nic) || !topology.HaveLayer2Link(g, p2, nic) {
		t.Fatal("cable move retained previous neighbor")
	}
	observeLLDPLink(g, root, p1, nic, 0, now.Add(2*time.Second))
	if !topology.HaveLayer2Link(g, p2, nic) {
		t.Fatal("old withdrawal removed new neighbor")
	}
	expireLLDPLinks(g, root, now.Add(3*time.Second), true)
	if !topology.HaveLayer2Link(g, p2, nic) {
		t.Fatal("restart discarded live persisted lease")
	}
	expireLLDPLinks(g, root, now.Add(11*time.Second), true)
	if topology.HaveLayer2Link(g, p2, nic) {
		t.Fatal("restart extended expired lease")
	}
	observeLLDPLink(g, root, p2, nic, 10, now.Add(20*time.Second))
	observeLLDPLink(g, root, p2, nic, 0, now.Add(21*time.Second))
	if topology.HaveLayer2Link(g, p2, nic) {
		t.Fatal("TTL zero did not withdraw neighbor")
	}
}

func TestLLDPExpiryPreservesManualAndNonLLDPRelations(t *testing.T) {
	g, root, nic, p1, p2 := leaseFixture(t)
	topology.AddLayer2Link(g, p1, nic, nil) // unverifiable legacy observation
	topology.AddLayer2Link(g, p2, nic, graph.Metadata{"ManualPortMapping": true})
	bridge, _ := g.NewNode("bridge", graph.Metadata{"Type": "bridge"})
	topology.AddLayer2Link(g, bridge, nic, nil)
	expireLLDPLinks(g, root, time.Now(), true)
	if topology.HaveLayer2Link(g, p1, nic) {
		t.Fatal("legacy link promoted to fresh after restart")
	}
	if !topology.HaveLayer2Link(g, p2, nic) || !topology.HaveLayer2Link(g, bridge, nic) {
		t.Fatal("manual/bridge relation lost")
	}
}
