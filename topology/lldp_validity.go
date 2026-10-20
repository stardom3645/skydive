package topology

import (
	"github.com/skydive-project/skydive/graffiti/graph"
	"time"
)

// LLDPLinkIsCurrent only evaluates observation freshness, never physical health.
// Legacy links without lease metadata retain their existing behavior until the
// owning LLDP probe starts and replaces its unverified local observations.
func LLDPLinkIsCurrent(edge *graph.Edge, now time.Time) bool {
	expires, err := edge.GetFieldInt64("LLDP.ExpiresAt")
	return err != nil || expires > now.UnixNano()/int64(time.Millisecond)
}
