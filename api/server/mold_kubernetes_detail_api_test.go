package server

import (
	"encoding/json"
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
	pod := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
		Name: "app", RestartCount: 2,
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}},
	}}}}
	problem, restarted, oom := podProblemState(pod)
	if !problem || !restarted || !oom {
		t.Fatalf("expected problem/restarted/oom to be true, got %v/%v/%v", problem, restarted, oom)
	}
}
