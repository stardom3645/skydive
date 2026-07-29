package server

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func boolPointer(value bool) *bool {
	return &value
}

func TestServiceImpactKeepsServiceHealthyWhenOneReadyEndpointRemains(t *testing.T) {
	services := []corev1.Service{{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "api"}},
	}}
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{UID: types.UID("problem"), Name: "api-a", Namespace: "default", Labels: map[string]string{"app": "api"}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionFalse,
			}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{UID: types.UID("healthy"), Name: "api-b", Namespace: "default", Labels: map[string]string{"app": "api"}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: corev1.ConditionTrue,
			}}},
		},
	}
	slices := []discoveryv1.EndpointSlice{{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-1", Namespace: "default",
			Labels: map[string]string{discoveryv1.LabelServiceName: "api"},
		},
		Endpoints: []discoveryv1.Endpoint{
			{TargetRef: &corev1.ObjectReference{Kind: "Pod", UID: types.UID("problem")}, Conditions: discoveryv1.EndpointConditions{Ready: boolPointer(true)}},
			{TargetRef: &corev1.ObjectReference{Kind: "Pod", UID: types.UID("healthy")}, Conditions: discoveryv1.EndpointConditions{Ready: boolPointer(true)}},
		},
	}}

	impact := aggregateKubernetesServiceImpact(services, slices, aggregateKubernetesPods(pods), nil)["default/api"]
	if impact.Affected || impact.ReadyEndpoints != 1 {
		t.Fatalf("impact = %+v, want healthy with one usable Ready endpoint", impact)
	}
}

func TestServiceImpactRequiresUsableReadyEndpoint(t *testing.T) {
	services := []corev1.Service{{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "api"}},
	}}
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("problem"), Name: "api-a", Namespace: "default", Labels: map[string]string{"app": "api"}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{
			Type: corev1.PodReady, Status: corev1.ConditionFalse,
		}}},
	}}
	slices := []discoveryv1.EndpointSlice{{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-1", Namespace: "default",
			Labels: map[string]string{discoveryv1.LabelServiceName: "api"},
		},
		Endpoints: []discoveryv1.Endpoint{{
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", UID: types.UID("problem")},
			Conditions: discoveryv1.EndpointConditions{Ready: boolPointer(false)},
		}},
	}}

	impact := aggregateKubernetesServiceImpact(services, slices, aggregateKubernetesPods(pods), nil)["default/api"]
	if !impact.Affected || impact.Reason != kubernetesServiceImpactNoReadyEndpoint {
		t.Fatalf("impact = %+v, want no-ready-endpoint impact", impact)
	}
}

func TestTerminatedPodsDoNotChangeSelectorServiceCurrentImpact(t *testing.T) {
	services := []corev1.Service{{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "api"}},
	}}
	active := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("active"), Name: "api-current", Namespace: "default", Labels: map[string]string{"app": "api"}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{
			Type: corev1.PodReady, Status: corev1.ConditionTrue,
		}}},
	}
	evicted := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("evicted"), Name: "api-old", Namespace: "default", Labels: map[string]string{"app": "api"}},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"},
	}
	withoutHistory := aggregateKubernetesServiceImpact(services, nil, aggregateKubernetesPods([]corev1.Pod{active}), nil)["default/api"]
	withHistory := aggregateKubernetesServiceImpact(services, nil, aggregateKubernetesPods([]corev1.Pod{active, evicted}), nil)["default/api"]
	if withoutHistory != withHistory || withHistory.Affected {
		t.Fatalf("terminated history changed current impact: without=%+v with=%+v", withoutHistory, withHistory)
	}
}

func TestSelectorFallbackDoesNotTreatPendingPodAsReadyEndpoint(t *testing.T) {
	services := []corev1.Service{{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "api"}},
	}}
	pending := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("pending"), Name: "api-pending", Namespace: "default", Labels: map[string]string{"app": "api"}},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}
	impact := aggregateKubernetesServiceImpact(services, nil, aggregateKubernetesPods([]corev1.Pod{pending}), nil)["default/api"]
	if !impact.Affected || impact.ReadyEndpoints != 0 {
		t.Fatalf("impact = %+v, Pending selector target must not be a Ready endpoint", impact)
	}
}
