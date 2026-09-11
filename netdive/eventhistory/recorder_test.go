package eventhistory

import (
	"github.com/skydive-project/skydive/graffiti/graph"
	db "github.com/skydive-project/skydive/netdive/database"
	"testing"
	"time"
)

func TestBaselineTransitionsRestartAndIdentity(t *testing.T) {
	events := []db.ChangeEvent{}
	r := New(func(e db.ChangeEvent) { events = append(events, e) })
	defer r.Close()
	e := db.ChangeEvent{ResourceType: "nic", ResourceID: "stable-id", ResourceName: "eno2", EventType: "link_changed", Source: "infrastructure", NewValue: "UP"}
	r.Observe(e)
	if len(events) != 0 {
		t.Fatal("baseline produced event")
	}
	e.NewValue = "DOWN"
	r.Observe(e)
	for i := 0; i < 10; i++ {
		r.Observe(e)
	}
	if len(events) != 1 || events[0].OldValue != "UP" || events[0].Severity != "" {
		t.Fatalf("wrong transition: %+v", events)
	}
	e.ResourceName = "renamed"
	r.Observe(e)
	if len(events) != 1 {
		t.Fatal("rename changes identity")
	}
	e.NewValue = "UP"
	r.Observe(e)
	if len(events) != 2 {
		t.Fatal(events)
	}
	restarted := New(func(e db.ChangeEvent) { events = append(events, e) })
	defer restarted.Close()
	restarted.Observe(e)
	if len(events) != 2 {
		t.Fatal("restart generated transition")
	}
}

func TestKubernetesReadyAndPodPhase(t *testing.T) {
	events := []db.ChangeEvent{}
	r := New(func(e db.ChangeEvent) { events = append(events, e) })
	defer r.Close()
	node := graph.CreateNode("node-uid", graph.Metadata{"Name": "worker-2", "Manager": "k8s", "Type": "node", "K8s": map[string]interface{}{"UID": "node-uid", "ConditionStates": map[string]interface{}{"Ready": "True"}}}, graph.Time(time.Now()), "", "")
	r.OnNodeAdded(node)
	node.Metadata.SetField("K8s.ConditionStates.Ready", "False")
	r.OnNodeUpdated(node, nil)
	r.OnNodeUpdated(node, nil)
	if len(events) != 1 || events[0].OldValue != "Ready" || events[0].NewValue != "NotReady" {
		t.Fatalf("ready: %+v", events)
	}
	pod := graph.CreateNode("pod-uid", graph.Metadata{"Name": "pod", "Manager": "k8s", "Type": "pod", "K8s": map[string]interface{}{"UID": "pod-uid", "Extra": map[string]interface{}{"status": map[string]interface{}{"phase": "Pending"}}}}, graph.Time(time.Now()), "", "")
	r.OnNodeAdded(pod)
	pod.Metadata.SetField("K8s.Extra.status.phase", "Running")
	r.OnNodeUpdated(pod, nil)
	if len(events) != 2 || events[1].NewValue != "Running" {
		t.Fatalf("phase: %+v", events)
	}
}

func TestLLDPBaselineReconnectRemovalAndCreation(t *testing.T) {
	backend, _ := graph.NewMemoryBackend()
	g := graph.NewGraph("test", backend, "")
	port, _ := g.NewNode("port-id", graph.Metadata{"Type": "switchport", "Name": "xg1"})
	nic, _ := g.NewNode("nic-id", graph.Metadata{"Type": "device", "Name": "eno1", "TID": "nic-tid"})
	edge := graph.CreateEdge("edge", port, nic, graph.Metadata{"RelationType": "layer2"}, graph.Time(time.Now()), "", "")
	events := make(chan db.ChangeEvent, 10)
	r := New(func(e db.ChangeEvent) { events <- e })
	r.graph = g
	r.removalGrace = 20 * time.Millisecond
	defer r.Close()
	r.OnEdgeAdded(edge)
	r.OnEdgeAdded(edge)
	if len(events) != 0 {
		t.Fatal("initial or repeated relation emitted")
	}
	r.OnEdgeDeleted(edge)
	r.OnEdgeAdded(edge)
	time.Sleep(30 * time.Millisecond)
	if len(events) != 0 {
		t.Fatal("transient reconnect emitted")
	}
	r.OnEdgeDeleted(edge)
	r.OnEdgeDeleted(edge)
	select {
	case e := <-events:
		if e.EventType != "relation_removed" {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("missing removal")
	}
	r.started = time.Now().Add(-time.Minute)
	r.OnEdgeAdded(edge)
	r.OnEdgeAdded(edge)
	if e := <-events; e.EventType != "relation_created" {
		t.Fatal(e)
	}
	if len(events) != 0 {
		t.Fatal("duplicate event")
	}
}

func TestVMUsesUUIDRatherThanDisplayNameOrRebuiltNodeID(t *testing.T) {
	events := []db.ChangeEvent{}
	r := New(func(e db.ChangeEvent) { events = append(events, e) })
	defer r.Close()
	n := graph.CreateNode("random1", graph.Metadata{"Type": "libvirt", "UUID": "vm-uuid", "Name": "vm", "State": "UP"}, graph.Time(time.Now()), "", "")
	r.OnNodeAdded(n)
	n = graph.CreateNode("random2", graph.Metadata{"Type": "libvirt", "UUID": "vm-uuid", "Name": "vm", "State": "UP"}, graph.Time(time.Now()), "", "")
	r.OnNodeAdded(n)
	if len(events) != 0 {
		t.Fatal("rebuilt VM emitted")
	}
	n.Metadata["State"] = "DOWN"
	r.OnNodeUpdated(n, nil)
	if len(events) != 1 || events[0].ResourceID != "vm-uuid" {
		t.Fatal(events)
	}
}
