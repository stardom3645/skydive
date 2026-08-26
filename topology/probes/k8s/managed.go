package k8s

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/skydive-project/skydive/config"
	"github.com/skydive-project/skydive/graffiti/graph"
	"github.com/skydive-project/skydive/graffiti/logging"
	"github.com/skydive-project/skydive/probe"
)

const (
	defaultKubernetesStatePollInterval = 30 * time.Second
	defaultKubernetesTransitionGrace   = 20 * time.Second
	maxKubernetesStateRetryInterval    = 5 * time.Minute
)

// KubernetesClusterStateResolver returns Mold lifecycle states keyed by
// Kubernetes cluster ID. It intentionally does not probe the Kubernetes API.
type KubernetesClusterStateResolver func() (map[string]string, error)

type managedK8sChild interface {
	Start() error
	Stop()
}

type managedK8sChildFactory func(moldKubernetesClusterEntry, string) (managedK8sChild, error)

type managedK8sProbe struct {
	graph        *graph.Graph
	entries      []moldKubernetesClusterEntry
	resolver     KubernetesClusterStateResolver
	factory      managedK8sChildFactory
	pollInterval time.Duration
	gracePeriod  time.Duration

	lock             sync.Mutex
	children         map[string]managedK8sChild
	transitionSince  map[string]time.Time
	stopCh           chan struct{}
	stopped          chan struct{}
	started          bool
	resolverFailures int
}

// NewMoldManagedK8sProbe creates a per-cluster lifecycle supervisor. Unlike a
// single static probe, it can stop only the informer set belonging to a Mold
// cluster that entered Stopped and rebuild it when that cluster is Running.
func NewMoldManagedK8sProbe(g *graph.Graph, resolver KubernetesClusterStateResolver) (probe.Handler, error) {
	if ShouldSkipK8sProbe() {
		return nil, nil
	}
	entries := moldKubernetesSelectedClusterEntries()
	if len(entries) == 0 {
		// Non Mold-managed deployments keep the historical static probe path.
		return NewK8sProbe(g)
	}

	pollInterval := configuredKubernetesDuration("mold.kubernetes.statusPollInterval", defaultKubernetesStatePollInterval)
	gracePeriod := configuredKubernetesDuration("mold.kubernetes.transitionGracePeriod", defaultKubernetesTransitionGrace)
	factory := func(entry moldKubernetesClusterEntry, manager string) (managedK8sChild, error) {
		return newSingleK8sProbe(g, entry.Path, manager, entry.Name, false)
	}
	return newManagedK8sProbe(g, entries, resolver, factory, pollInterval, gracePeriod), nil
}

func configuredKubernetesDuration(key string, fallback time.Duration) time.Duration {
	seconds := config.GetInt(key)
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func newManagedK8sProbe(g *graph.Graph, entries []moldKubernetesClusterEntry, resolver KubernetesClusterStateResolver, factory managedK8sChildFactory, pollInterval, gracePeriod time.Duration) *managedK8sProbe {
	return &managedK8sProbe{
		graph:           g,
		entries:         entries,
		resolver:        resolver,
		factory:         factory,
		pollInterval:    pollInterval,
		gracePeriod:     gracePeriod,
		children:        make(map[string]managedK8sChild),
		transitionSince: make(map[string]time.Time),
	}
}

func (p *managedK8sProbe) Start() error {
	if p == nil {
		return nil
	}
	p.lock.Lock()
	if p.started {
		p.lock.Unlock()
		return nil
	}
	p.started = true
	p.stopCh = make(chan struct{})
	p.stopped = make(chan struct{})
	p.lock.Unlock()

	// A Mold status lookup failure must not silently classify a selected,
	// potentially Running cluster as inactive. Start it using the existing
	// Kubernetes behavior and let client-go report/back off API failures.
	states, err := p.resolveStates()
	if err != nil {
		logging.GetLogger().Warningf("Unable to resolve Mold Kubernetes states; retaining Kubernetes collection: %s", err)
		states = nil
	}
	if err := p.reconcile(states, time.Now()); err != nil {
		p.lock.Lock()
		for id, child := range p.children {
			child.Stop()
			delete(p.children, id)
		}
		p.started = false
		close(p.stopCh)
		close(p.stopped)
		p.lock.Unlock()
		return err
	}
	go p.monitor()
	return nil
}

func (p *managedK8sProbe) Stop() {
	if p == nil {
		return
	}
	p.lock.Lock()
	if !p.started {
		p.lock.Unlock()
		return
	}
	close(p.stopCh)
	stopped := p.stopped
	p.started = false
	p.lock.Unlock()
	<-stopped

	p.lock.Lock()
	for id, child := range p.children {
		child.Stop()
		delete(p.children, id)
	}
	p.lock.Unlock()
	CleanupK8sGraph(p.graph)
	ResetK8sRuntimeState()
}

func (p *managedK8sProbe) monitor() {
	defer close(p.stopped)
	delay := p.pollInterval
	for {
		timer := time.NewTimer(delay)
		select {
		case <-p.stopCh:
			if !timer.Stop() {
				<-timer.C
			}
			return
		case now := <-timer.C:
			states, err := p.resolveStates()
			if err != nil {
				p.resolverFailures++
				delay = cappedKubernetesRetryDelay(p.resolverFailures, p.pollInterval, maxKubernetesStateRetryInterval)
				logging.GetLogger().Warningf("Unable to refresh Mold Kubernetes states; retrying in %s: %s", delay, err)
				continue
			}
			p.resolverFailures = 0
			delay = p.pollInterval
			if err := p.reconcile(states, now); err != nil {
				logging.GetLogger().Warningf("Unable to reconcile Mold Kubernetes probes: %s", err)
			}
		}
	}
}

func (p *managedK8sProbe) resolveStates() (map[string]string, error) {
	if p.resolver == nil {
		return nil, nil
	}
	return p.resolver()
}

func (p *managedK8sProbe) reconcile(states map[string]string, now time.Time) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	for index, entry := range p.entries {
		state := strings.TrimSpace(states[entry.ID])
		switch classifyMoldKubernetesLifecycle(state) {
		case kubernetesLifecycleStopped:
			delete(p.transitionSince, entry.ID)
			p.stopChild(entry, "inactive", state)
		case kubernetesLifecycleTransitioning:
			since, exists := p.transitionSince[entry.ID]
			if !exists {
				p.transitionSince[entry.ID] = now
				since = now
			}
			if _, running := p.children[entry.ID]; running && now.Sub(since) >= p.gracePeriod {
				p.stopChild(entry, "stale", state)
			}
			// Starting clusters are intentionally not contacted until Mold reports
			// Running. This is the grace period that suppresses startup noise.
		case kubernetesLifecycleRunning, kubernetesLifecycleUnknown:
			delete(p.transitionSince, entry.ID)
			manager := Manager
			if len(p.entries) > 1 {
				manager = managedKubernetesRuntimeManager(entry, index)
			}
			if err := p.startChild(entry, manager); err != nil {
				return err
			}
			p.markClusterCollectionState(entry.Name, "active", state)
		}
	}
	return nil
}

func (p *managedK8sProbe) startChild(entry moldKubernetesClusterEntry, manager string) error {
	if _, exists := p.children[entry.ID]; exists {
		return nil
	}
	child, err := p.factory(entry, manager)
	if err != nil {
		return fmt.Errorf("create Kubernetes probe for %s: %w", entry.Name, err)
	}
	if child == nil {
		return nil
	}
	if err := child.Start(); err != nil {
		return fmt.Errorf("start Kubernetes probe for %s: %w", entry.Name, err)
	}
	p.children[entry.ID] = child
	logging.GetLogger().Infof("Kubernetes collection active for Mold cluster %s", entry.Name)
	return nil
}

func (p *managedK8sProbe) stopChild(entry moldKubernetesClusterEntry, collectionState, moldState string) {
	if child, exists := p.children[entry.ID]; exists {
		child.Stop()
		delete(p.children, entry.ID)
		logging.GetLogger().Infof("Kubernetes collection paused for Mold cluster %s (%s)", entry.Name, moldState)
	}
	p.markClusterCollectionState(entry.Name, collectionState, moldState)
}

func (p *managedK8sProbe) markClusterCollectionState(clusterName, collectionState, moldState string) {
	if p.graph == nil || strings.TrimSpace(clusterName) == "" {
		return
	}
	p.graph.Lock()
	defer p.graph.Unlock()
	for _, node := range p.graph.GetNodes(graph.Metadata{ClusterNameField: clusterName}) {
		_ = p.graph.AddMetadata(node, MetadataField("CollectionStatus"), collectionState)
		if moldState != "" {
			_ = p.graph.AddMetadata(node, MetadataField("MoldState"), moldState)
		}
	}
}

func managedKubernetesRuntimeManager(entry moldKubernetesClusterEntry, index int) string {
	return fmt.Sprintf("%s#%d", Manager, index)
}

type kubernetesLifecycle int

const (
	kubernetesLifecycleUnknown kubernetesLifecycle = iota
	kubernetesLifecycleRunning
	kubernetesLifecycleTransitioning
	kubernetesLifecycleStopped
)

func classifyMoldKubernetesLifecycle(state string) kubernetesLifecycle {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running":
		return kubernetesLifecycleRunning
	case "stopped":
		return kubernetesLifecycleStopped
	case "starting", "stopping":
		return kubernetesLifecycleTransitioning
	default:
		// Failed/Error and absent states are not inactive. Existing Kubernetes
		// problem/unknown handling remains authoritative for those cases.
		return kubernetesLifecycleUnknown
	}
}

func cappedKubernetesRetryDelay(attempt int, base, maximum time.Duration) time.Duration {
	if attempt <= 1 || base >= maximum {
		return base
	}
	delay := base
	for i := 1; i < attempt && delay < maximum; i++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}
