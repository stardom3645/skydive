package k8s

import (
	"errors"
	"testing"
	"time"
)

type fakeManagedK8sChild struct {
	started int
	stopped int
	err     error
}

func (c *fakeManagedK8sChild) Start() error {
	c.started++
	return c.err
}

func (c *fakeManagedK8sChild) Stop() { c.stopped++ }

func testManagedProbe(t *testing.T, state string, child *fakeManagedK8sChild) *managedK8sProbe {
	t.Helper()
	entry := moldKubernetesClusterEntry{ID: "cluster-1", Name: "cluster-one", Path: "/tmp/cluster-one.kubeconfig"}
	return newManagedK8sProbe(nil, []moldKubernetesClusterEntry{entry}, nil,
		func(moldKubernetesClusterEntry, string) (managedK8sChild, error) { return child, nil },
		time.Second, 10*time.Second)
}

func TestManagedK8sProbeRunningStartsCollectionOnce(t *testing.T) {
	child := &fakeManagedK8sChild{}
	probe := testManagedProbe(t, "Running", child)
	now := time.Now()
	if err := probe.reconcile(map[string]string{"cluster-1": "Running"}, now); err != nil {
		t.Fatal(err)
	}
	if err := probe.reconcile(map[string]string{"cluster-1": "Running"}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if child.started != 1 || child.stopped != 0 {
		t.Fatalf("running child start/stop=%d/%d, want 1/0", child.started, child.stopped)
	}
}

func TestManagedK8sProbeStoppedDoesNotStartAndStopsRunningChild(t *testing.T) {
	child := &fakeManagedK8sChild{}
	probe := testManagedProbe(t, "Stopped", child)
	now := time.Now()
	if err := probe.reconcile(map[string]string{"cluster-1": "Stopped"}, now); err != nil {
		t.Fatal(err)
	}
	if child.started != 0 {
		t.Fatalf("stopped cluster started informer %d times", child.started)
	}
	if err := probe.reconcile(map[string]string{"cluster-1": "Running"}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := probe.reconcile(map[string]string{"cluster-1": "Stopped"}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if child.started != 1 || child.stopped != 1 {
		t.Fatalf("running -> stopped start/stop=%d/%d, want 1/1", child.started, child.stopped)
	}
}

func TestManagedK8sProbeStoppedToRunningRecreatesCollection(t *testing.T) {
	created := 0
	var children []*fakeManagedK8sChild
	entry := moldKubernetesClusterEntry{ID: "cluster-1", Name: "cluster-one"}
	probe := newManagedK8sProbe(nil, []moldKubernetesClusterEntry{entry}, nil,
		func(moldKubernetesClusterEntry, string) (managedK8sChild, error) {
			created++
			child := &fakeManagedK8sChild{}
			children = append(children, child)
			return child, nil
		}, time.Second, 10*time.Second)
	now := time.Now()
	_ = probe.reconcile(map[string]string{"cluster-1": "Running"}, now)
	_ = probe.reconcile(map[string]string{"cluster-1": "Stopped"}, now.Add(time.Minute))
	_ = probe.reconcile(map[string]string{"cluster-1": "Running"}, now.Add(2*time.Minute))
	if created != 2 || children[0].stopped != 1 || children[1].started != 1 {
		t.Fatalf("resume lifecycle created=%d first stopped=%d second started=%d", created, children[0].stopped, children[1].started)
	}
}

func TestManagedK8sProbeTransitionUsesGracePeriod(t *testing.T) {
	child := &fakeManagedK8sChild{}
	probe := testManagedProbe(t, "Running", child)
	now := time.Now()
	_ = probe.reconcile(map[string]string{"cluster-1": "Running"}, now)
	_ = probe.reconcile(map[string]string{"cluster-1": "Stopping"}, now.Add(time.Second))
	_ = probe.reconcile(map[string]string{"cluster-1": "Stopping"}, now.Add(5*time.Second))
	if child.stopped != 0 {
		t.Fatal("transitioning cluster was stopped before grace period")
	}
	_ = probe.reconcile(map[string]string{"cluster-1": "Stopping"}, now.Add(12*time.Second))
	if child.stopped != 1 {
		t.Fatal("transitioning cluster was not stopped after grace period")
	}
}

func TestManagedK8sProbeRunningStartFailureRemainsAnError(t *testing.T) {
	child := &fakeManagedK8sChild{err: errors.New("api unreachable")}
	probe := testManagedProbe(t, "Running", child)
	if err := probe.reconcile(map[string]string{"cluster-1": "Running"}, time.Now()); err == nil {
		t.Fatal("Running API failure must remain visible instead of being classified inactive")
	}
}

func TestCappedKubernetesRetryDelayIncreasesAndCaps(t *testing.T) {
	base, maximum := 5*time.Second, 40*time.Second
	wants := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 40 * time.Second}
	for index, want := range wants {
		if got := cappedKubernetesRetryDelay(index+1, base, maximum); got != want {
			t.Fatalf("attempt %d delay=%s, want %s", index+1, got, want)
		}
	}
}

func TestMoldKubernetesLifecycleDoesNotTreatMissingOrFailedAsStopped(t *testing.T) {
	for _, state := range []string{"", "Unknown", "Failed", "Error"} {
		if got := classifyMoldKubernetesLifecycle(state); got != kubernetesLifecycleUnknown {
			t.Fatalf("state %q classified as %v, want unknown/non-inactive", state, got)
		}
	}
}
