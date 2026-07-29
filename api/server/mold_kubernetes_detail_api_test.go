package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestBuildKubernetesPodDetailDoesNotExposeEnvironmentOrSecretValues(t *testing.T) {
	started := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("pod-uid"), Name: "web", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "app", Image: "example/app:1",
			Env: []corev1.EnvVar{
				{Name: "PLAIN_SECRET", Value: "must-not-leak"},
				{Name: "SECRET_REF", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "credentials"}, Key: "password"}}},
			},
		}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Image: "example/app:1", Ready: true, Started: &started}}},
	}

	payload, err := json.Marshal(buildKubernetesPodDetail("cluster-1", pod))
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"must-not-leak", "credentials", "password", "PLAIN_SECRET", "SECRET_REF"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sensitive environment data leaked in response: %s", forbidden)
		}
	}
}

func TestPodProblemStateDetectsRestartAndOOMKilled(t *testing.T) {
	pod := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
		Name: "app", RestartCount: 2,
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}},
	}}}}
	problem, restarted, oom := podProblemState(pod)
	if !problem || !restarted || !oom {
		t.Fatalf("expected problem/restarted/oom to be true, got %v/%v/%v", problem, restarted, oom)
	}
}

func TestAggregateKubernetesPodsExcludesTerminatedHistory(t *testing.T) {
	pods := make([]corev1.Pod, 0, 264)
	for i := 0; i < 13; i++ {
		name := fmt.Sprintf("running-%d", i)
		pods = append(pods, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)},
			Spec:       corev1.PodSpec{NodeName: "worker-1"},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			},
		})
	}
	for i := 0; i < 248; i++ {
		name := fmt.Sprintf("evicted-%d", i)
		pods = append(pods, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)},
			Spec:       corev1.PodSpec{NodeName: "worker-1"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"},
		})
	}
	pods = append(pods,
		corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: types.UID("succeeded")}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
		corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: types.UID("failed")}, Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Error"}},
	)
	deleting := metav1.Now()
	pods = append(pods, corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("deleting"), DeletionTimestamp: &deleting},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})

	aggregate := aggregateKubernetesPods(pods)
	if got := len(aggregate.ActivePods); got != 13 {
		t.Fatalf("active Pods = %d, want 13", got)
	}
	if aggregate.Running != 13 || aggregate.Pending != 0 || len(aggregate.ProblemPods) != 0 {
		t.Fatalf("unexpected current aggregate: running=%d pending=%d problems=%d", aggregate.Running, aggregate.Pending, len(aggregate.ProblemPods))
	}
	if got := len(aggregate.TerminatedPods); got != 251 {
		t.Fatalf("terminated Pods = %d, want 251", got)
	}
	if got := len(aggregate.EvictedPods); got != 248 {
		t.Fatalf("Evicted Pods = %d, want 248", got)
	}
	if percent := float64(len(aggregate.ActivePods)) / 110 * 100; percent < 11.81 || percent > 11.82 {
		t.Fatalf("Pod utilization = %.2f, want 11.82", percent)
	}
}

func TestClusterActivePodsExplainsTwentyThreeAsNodeSumsPlusUnscheduledPending(t *testing.T) {
	pods := make([]corev1.Pod, 0, 271)
	for i := 0; i < 22; i++ {
		name := fmt.Sprintf("scheduled-%d", i)
		pods = append(pods, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{UID: types.UID(name), Name: name},
			Spec:       corev1.PodSpec{NodeName: fmt.Sprintf("worker-%d", i%2)},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		})
	}
	pods = append(pods, corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("unscheduled"), Name: "unscheduled"},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	})
	for i := 0; i < 248; i++ {
		name := fmt.Sprintf("history-%d", i)
		pods = append(pods, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{UID: types.UID(name), Name: name},
			Spec:       corev1.PodSpec{NodeName: "worker-0"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"},
		})
	}

	cluster := aggregateKubernetesPods(pods)
	worker0 := aggregateKubernetesPodsInScope(pods, kubernetesPodScope{NodeName: "worker-0"})
	worker1 := aggregateKubernetesPodsInScope(pods, kubernetesPodScope{NodeName: "worker-1"})
	if len(cluster.ActivePods) != 23 || worker0.Running+worker1.Running+cluster.UnscheduledPending != 23 {
		t.Fatalf("cluster=%d worker running=%d+%d unscheduled=%d, want 23", len(cluster.ActivePods), worker0.Running, worker1.Running, cluster.UnscheduledPending)
	}
}

func TestClusterActivePodsEqualsScheduledNodeSumsPlusUnscheduledPending(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{UID: types.UID("running")}, Spec: corev1.PodSpec{NodeName: "worker-1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{ObjectMeta: metav1.ObjectMeta{UID: types.UID("pending")}, Spec: corev1.PodSpec{NodeName: "worker-2"}, Status: corev1.PodStatus{Phase: corev1.PodPending}},
		{ObjectMeta: metav1.ObjectMeta{UID: types.UID("unscheduled")}, Status: corev1.PodStatus{Phase: corev1.PodPending}},
		{ObjectMeta: metav1.ObjectMeta{UID: types.UID("evicted")}, Spec: corev1.PodSpec{NodeName: "worker-1"}, Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"}},
	}
	cluster := aggregateKubernetesPods(pods)
	scheduled := 0
	unscheduledPending := 0
	for i := range cluster.ActivePods {
		if cluster.ActivePods[i].Spec.NodeName == "" {
			unscheduledPending++
		} else {
			scheduled++
		}
	}
	if len(cluster.ActivePods) != scheduled+unscheduledPending {
		t.Fatalf("cluster active Pods %d != scheduled %d + unscheduled Pending %d", len(cluster.ActivePods), scheduled, unscheduledPending)
	}
	if len(cluster.ActivePods) != 3 {
		t.Fatalf("cluster active Pods = %d, want 3", len(cluster.ActivePods))
	}
}

func TestAggregateKubernetesPodsUsesUIDAndScope(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{UID: types.UID("same"), Name: "a", Namespace: "default"}, Spec: corev1.PodSpec{NodeName: "worker-1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{ObjectMeta: metav1.ObjectMeta{UID: types.UID("same"), Name: "duplicate", Namespace: "default"}, Spec: corev1.PodSpec{NodeName: "worker-1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		{ObjectMeta: metav1.ObjectMeta{UID: types.UID("other"), Name: "b", Namespace: "other"}, Spec: corev1.PodSpec{NodeName: "worker-2"}, Status: corev1.PodStatus{Phase: corev1.PodPending}},
	}
	aggregate := aggregateKubernetesPodsInScope(pods, kubernetesPodScope{NodeName: "worker-1", Namespace: "default"})
	if len(aggregate.AllPods) != 1 || len(aggregate.ActivePods) != 1 {
		t.Fatalf("scoped UID aggregate = all %d active %d, want 1/1", len(aggregate.AllPods), len(aggregate.ActivePods))
	}
}

func TestPodProblemStateDetectsNotReadyAndImagePull(t *testing.T) {
	pod := &corev1.Pod{Status: corev1.PodStatus{
		Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{{
			Type: corev1.PodReady, Status: corev1.ConditionFalse,
		}},
		ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
				Reason: "ImagePullBackOff",
			}},
		}},
	}}
	problem, _, _ := podProblemState(pod)
	if !problem {
		t.Fatal("expected Ready=false/ImagePullBackOff pod to be a problem")
	}
}

func TestKubernetesPodUsesLocalPathStorageClass(t *testing.T) {
	className := "local-path"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
		Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
			Name: "data",
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: "web-data",
			}},
		}}},
	}
	claims := map[string]corev1.PersistentVolumeClaim{
		"default/web-data": {
			Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &className},
		},
	}
	classes := map[string]bool{className: true}
	if !kubernetesPodUsesNodeLocalStorage(pod, claims, map[string]corev1.PersistentVolume{}, classes) {
		t.Fatal("expected local-path PVC to be node-local storage")
	}
}
