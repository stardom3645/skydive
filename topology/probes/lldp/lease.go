package lldp

import (
	"github.com/skydive-project/skydive/graffiti/graph"
	"github.com/skydive-project/skydive/topology"
	"time"
)

// These helpers run under the graph lock. The graph itself stores leases, so a
// probe restart cannot extend an old observation or create parallel relations.
func isLLDPNeighbor(g *graph.Graph, edge *graph.Edge, nic *graph.Node) bool {
	if manual, _ := edge.GetFieldBool("ManualPortMapping"); manual {
		return false
	}
	relation, _ := edge.GetFieldString("RelationType")
	if relation != topology.Layer2Link {
		return false
	}
	peerID := edge.Parent
	if peerID == nic.ID {
		peerID = edge.Child
	}
	peer := g.GetNode(peerID)
	if peer == nil {
		return false
	}
	kind, _ := peer.GetFieldString("Type")
	_, err := peer.GetField("LLDP")
	return err == nil && (kind == "switchport" || kind == "switch")
}

func observeLLDPLink(g *graph.Graph, root, port, nic *graph.Node, ttl uint16, now time.Time) error {
	var current *graph.Edge
	for _, edge := range g.GetNodeEdges(nic, topology.Layer2Metadata()) {
		if !isLLDPNeighbor(g, edge, nic) {
			continue
		}
		same := edge.Parent == port.ID || edge.Child == port.ID
		if same && ttl > 0 {
			current = edge
			continue
		}
		// A TTL=0 withdrawal refers only to the advertising neighbor. A delayed
		// withdrawal from a previous cable location cannot delete the new neighbor.
		if ttl > 0 || same {
			if err := g.DelEdge(edge); err != nil {
				return err
			}
		}
	}
	if ttl == 0 {
		return nil
	}
	metadata := graph.Metadata{"Probe": "lldp", "LLDP": graph.Metadata{
		"AgentID": string(root.ID), "LocalNodeID": string(nic.ID), "TTL": int64(ttl),
		"LastSeen": now.UnixNano() / int64(time.Millisecond), "ExpiresAt": now.Add(time.Duration(ttl)*time.Second).UnixNano() / int64(time.Millisecond),
	}}
	if current == nil {
		_, err := topology.AddLayer2Link(g, port, nic, metadata)
		return err
	}
	transaction := g.StartMetadataTransaction(current)
	for key, value := range metadata {
		transaction.AddMetadata(key, value)
	}
	return transaction.Commit()
}

func expireLLDPLinks(g *graph.Graph, root *graph.Node, now time.Time, removeLegacy bool) {
	for _, nic := range g.LookupChildren(root, nil, topology.OwnershipMetadata()) {
		for _, edge := range g.GetNodeEdges(nic, topology.Layer2Metadata()) {
			if !isLLDPNeighbor(g, edge, nic) {
				continue
			}
			agent, _ := edge.GetFieldString("LLDP.AgentID")
			if agent != "" && agent != string(root.ID) {
				continue
			}
			_, expiryErr := edge.GetFieldInt64("LLDP.ExpiresAt")
			if !topology.LLDPLinkIsCurrent(edge, now) || (removeLegacy && expiryErr != nil) {
				_ = g.DelEdge(edge)
			}
		}
	}
}
