package server

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestEndpointReadinessDeduplicationAndConcentration(t *testing.T) {
	for _, ready := range []*bool{nil, boolPointer(true), boolPointer(false)} {
		detail := kubernetesServiceDetail{EndpointDataAvailable: true, Endpoints: []kubernetesEndpointDetail{
			{TargetUID: "a", Address: "10.0.0.1", NodeName: "node-a", Ready: ready},
			{TargetUID: "a", Address: "fd00::1", NodeName: "node-a", Ready: ready},
			{TargetUID: "a", Address: "10.0.0.1", NodeName: "node-a", Ready: ready},
			{TargetUID: "b", Address: "10.0.0.2", NodeName: "node-a", Ready: ready},
			{TargetUID: "c", Address: "10.0.0.3", NodeName: "node-b", Ready: boolPointer(false)},
		}}
		applyKubernetesServiceEndpointStatus(&detail)
		if detail.EndpointCount != 3 {
			t.Fatalf("duplicate targets counted: %+v", detail)
		}
		expected := 2
		if ready != nil && !*ready {
			expected = 0
		}
		if detail.ReadyEndpointCount != expected || detail.SingleNodeConcentration != (expected == 2) {
			t.Fatalf("wrong readiness/concentration: %+v", detail)
		}
	}
}

func TestUnknownNodeAndExternalNameDoNotClaimConcentrationOrOutage(t *testing.T) {
	detail := kubernetesServiceDetail{EndpointDataAvailable: true, Endpoints: []kubernetesEndpointDetail{{TargetUID: "a", NodeName: "node-a"}, {TargetUID: "b"}}}
	applyKubernetesServiceEndpointStatus(&detail)
	if detail.SingleNodeConcentration {
		t.Fatal("unknown placement treated as single-node")
	}
	detail.Type = "ExternalName"
	detail.Endpoints = nil
	applyKubernetesServiceEndpointStatus(&detail)
	if detail.NoReadyEndpoints {
		t.Fatal("ExternalName has no endpoint health contract")
	}
	detail.Type = "ClusterIP"
	detail.EndpointDataAvailable = false
	applyKubernetesServiceEndpointStatus(&detail)
	if detail.NoReadyEndpoints {
		t.Fatal("missing collection treated as zero ready")
	}
	detail.EndpointDataAvailable = true
	applyKubernetesServiceEndpointStatus(&detail)
	if !detail.NoReadyEndpoints {
		t.Fatal("collected empty targets not detected")
	}
}

func TestServiceImpactUsesSameReadyAndLogicalTargetPolicy(t *testing.T) {
	services := []corev1.Service{{ObjectMeta: metav1.ObjectMeta{Name: "direct", Namespace: "ns"}}}
	slices := []discoveryv1.EndpointSlice{{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Labels: map[string]string{discoveryv1.LabelServiceName: "direct"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}}}}}
	slices = append(slices, slices[0])
	impact := aggregateKubernetesServiceImpact(services, slices, kubernetesPodAggregate{}, nil)["ns/direct"]
	if impact.Affected || impact.ReadyEndpoints != 1 || impact.CandidateEndpoints != 1 {
		t.Fatalf("wrong direct endpoint impact: %+v", impact)
	}
	services[0].Spec.Type = corev1.ServiceTypeExternalName
	if len(aggregateKubernetesServiceImpact(services, slices, kubernetesPodAggregate{}, nil)) != 0 {
		t.Fatal("ExternalName evaluated as endpoints")
	}
}

func TestEndpointRowsRetainDistinctAddressesAndMergePorts(t *testing.T) {
	portA, portB := int32(80), int32(443)
	detail := kubernetesServiceDetail{EndpointDataAvailable: true, Endpoints: []kubernetesEndpointDetail{
		{TargetUID: "pod", Address: "10.0.0.1", Ports: []kubernetesEndpointPort{{Port: &portA}}},
		{TargetUID: "pod", Address: "10.0.0.1", Ports: []kubernetesEndpointPort{{Port: &portA}, {Port: &portB}}},
		{TargetUID: "pod", Address: "fd00::1", Ready: boolPointer(false)},
	}}
	applyKubernetesServiceEndpointStatus(&detail)
	if len(detail.Endpoints) != 2 || len(detail.Endpoints[0].Ports) != 2 || detail.EndpointCount != 1 || detail.ReadyEndpointCount != 0 {
		t.Fatalf("wrong duplicate/conflicting endpoint merge: %+v", detail)
	}
}

func TestSingleReadyTargetRemainsClusterPlacementRisk(t *testing.T) {
	detail := kubernetesServiceDetail{EndpointDataAvailable: true, Endpoints: []kubernetesEndpointDetail{{TargetUID: "one", NodeName: "node-a"}, {TargetUID: "not-ready", NodeName: "node-b", Ready: boolPointer(false)}}}
	applyKubernetesServiceEndpointStatus(&detail)
	if !detail.SingleEndpoint || detail.SingleNodeConcentration || !kubernetesReadyEndpointsOnSingleNode(detail.Endpoints) {
		t.Fatalf("single-target and placement contracts diverged: %+v", detail)
	}
}
