// Package eventhistory observes the existing graph event stream. No resource
// polling or full snapshots are persisted. Restart establishes a fresh baseline.
package eventhistory

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/skydive-project/skydive/graffiti/graph"
	db "github.com/skydive-project/skydive/netdive/database"
)

type baseline struct {
	value string
	seen  time.Time
}
type relation struct {
	event   db.ChangeEvent
	present bool
	timer   *time.Timer
}
type Recorder struct {
	graph.DefaultGraphListener
	mu           sync.Mutex
	sink         func(db.ChangeEvent)
	values       map[string]baseline
	relations    map[string]*relation
	ports        map[string]bool
	edgeKeys     map[graph.Identifier]string
	started      time.Time
	closed       bool
	removalGrace time.Duration
	graph        *graph.Graph
}

func New(sink func(db.ChangeEvent)) *Recorder {
	return &Recorder{sink: sink, values: map[string]baseline{}, relations: map[string]*relation{}, ports: map[string]bool{}, edgeKeys: map[graph.Identifier]string{}, started: time.Now(), removalGrace: 10 * time.Second}
}

func Attach(g *graph.Graph, database *db.Database) *Recorder {
	r := New(database.RecordEvent)
	r.graph = g
	g.Lock()
	defer g.Unlock()
	for _, n := range g.GetNodes(nil) {
		r.OnNodeAdded(n)
	}
	for _, e := range g.GetEdges(nil) {
		r.OnEdgeAdded(e)
	}
	g.AddEventListener(r)
	return r
}

func (r *Recorder) Close() {
	if r.graph != nil {
		r.graph.RemoveEventListener(r)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	for _, rel := range r.relations {
		if rel.timer != nil {
			rel.timer.Stop()
		}
	}
}

// Observe also accepts existing Mold lifecycle callbacks, using Mold's UUID.
func (r *Recorder) Observe(e db.ChangeEvent) {
	if e.NewValue == "" || e.ResourceID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	key := e.Source + ":" + e.ResourceType + ":" + e.ResourceID + ":" + e.EventType
	previous, ok := r.values[key]
	// Bound stale baselines without retaining disappeared resources indefinitely.
	if !ok && len(r.values) >= 100000 {
		for k, v := range r.values {
			if time.Since(v.seen) > 24*time.Hour {
				delete(r.values, k)
			}
		}
		if len(r.values) >= 100000 {
			return
		}
	}
	r.values[key] = baseline{e.NewValue, time.Now()}
	if !ok || previous.value == e.NewValue {
		return
	}
	e.OldValue = previous.value
	r.sink(e)
}

func field(n *graph.Node, key string) string  { v, _ := n.GetFieldString(key); return v }
func (r *Recorder) OnNodeAdded(n *graph.Node) { r.OnNodeUpdated(n, nil) }
func (r *Recorder) OnNodeUpdated(n *graph.Node, _ []graph.PartiallyUpdatedOp) {
	kind := field(n, "Type")
	source := "infrastructure"
	value := field(n, "State")
	eventType := "state_changed"
	id := string(n.ID)
	if field(n, "Manager") == "k8s" {
		source = "kubernetes"
		if uid := field(n, "K8s.UID"); uid != "" {
			id = uid
		}
		switch kind {
		case "node":
			kind = "k8s_node"
			value = field(n, "K8s.ConditionStates.Ready")
			switch value {
			case "True":
				value = "Ready"
			case "False":
				value = "NotReady"
			case "Unknown":
				value = "Unknown"
			}
		case "pod":
			value = field(n, "K8s.Extra.status.phase")
		default:
			return // Cluster lifecycle is supplied by Mold, not synthetic graph state.
		}
	} else {
		id = field(n, "TID")
		switch kind {
		case "host", "bridge", "bond":
		case "device":
			kind = "nic"
			eventType = "link_changed"
		case "libvirt":
			kind = "vm"
			id = field(n, "UUID")
			if id == "" {
				return
			}
		default:
			return
		}
	}
	metadata, _ := json.Marshal(map[string]string{"nodeId": string(n.ID)})
	r.Observe(db.ChangeEvent{ResourceType: kind, ResourceID: id, ResourceName: field(n, "Name"), EventType: eventType, NewValue: value, Source: source, Metadata: string(metadata)})
}

func (r *Recorder) edgeEvent(e *graph.Edge) (string, db.ChangeEvent) {
	if r.graph == nil {
		return "", db.ChangeEvent{}
	}
	relationType, _ := e.GetFieldString("RelationType")
	if relationType != "layer2" {
		return "", db.ChangeEvent{}
	}
	port, nic := r.graph.GetNode(e.Parent), r.graph.GetNode(e.Child)
	if port == nil || nic == nil {
		return "", db.ChangeEvent{}
	}
	if field(nic, "Type") == "switchport" {
		port, nic = nic, port
	}
	if field(port, "Type") != "switchport" || field(nic, "Type") != "device" {
		return "", db.ChangeEvent{}
	}
	nicID := field(nic, "TID")
	if nicID == "" {
		return "", db.ChangeEvent{}
	}
	key := string(port.ID) + ":" + nicID
	metadata, _ := json.Marshal(map[string]string{"nodeId": string(port.ID), "peerId": string(nic.ID), "peerName": field(nic, "Name")})
	return key, db.ChangeEvent{ResourceType: "switchport", ResourceID: string(port.ID), ResourceName: field(port, "Name"), Source: "lldp", Metadata: string(metadata)}
}

func (r *Recorder) OnEdgeAdded(e *graph.Edge) {
	key, event := r.edgeEvent(e)
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.edgeKeys[e.ID] = key
	if rel, ok := r.relations[key]; ok {
		if rel.timer != nil {
			rel.timer.Stop()
			rel.timer = nil
			rel.present = true
			return
		}
		if rel.present {
			return
		}
	} else if !r.ports[event.ResourceID] {
		r.ports[event.ResourceID] = true
		r.relations[key] = &relation{event: event, present: true}
		return
	}
	r.relations[key] = &relation{event: event, present: true}
	event.EventType = "relation_created"
	event.NewValue = "connected"
	r.sink(event)
}

func (r *Recorder) OnEdgeUpdated(e *graph.Edge, _ []graph.PartiallyUpdatedOp) { r.OnEdgeAdded(e) }
func (r *Recorder) OnEdgeDeleted(e *graph.Edge) {
	relationType, _ := e.GetFieldString("RelationType")
	if relationType != "layer2" {
		return
	}
	// Find by endpoints even when node deletion has already removed the graph node.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	key := r.edgeKeys[e.ID]
	delete(r.edgeKeys, e.ID)
	rel, ok := r.relations[key]
	if !ok || !rel.present || rel.timer != nil {
		return
	}
	removedAt := time.Now().Unix()
	// A one-shot grace period suppresses transient informer/hub rebuilds. There is
	// no additional polling loop, and the timestamp is the original observation.
	rel.timer = time.AfterFunc(r.removalGrace, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.closed || rel.timer == nil {
			return
		}
		rel.timer = nil
		rel.present = false
		event := rel.event
		event.EventType = "relation_removed"
		event.OldValue = "connected"
		event.NewValue = "disconnected"
		event.OccurredAt = removedAt
		r.sink(event)
		delete(r.relations, key)
	})
}
