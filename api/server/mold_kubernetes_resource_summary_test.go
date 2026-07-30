package server

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAddResourceListIncludesPodCapacity(t *testing.T) {
	summary := kubernetesResourceSummary{}

	addResourceList(&summary, corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("4Gi"),
		corev1.ResourcePods:   resource.MustParse("110"),
	}, true)
	addResourceList(&summary, corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1900m"),
		corev1.ResourceMemory: resource.MustParse("3.65Gi"),
		corev1.ResourcePods:   resource.MustParse("110"),
	}, false)

	if summary.CapacityPods != 110 {
		t.Fatalf("capacity pods = %d, want 110", summary.CapacityPods)
	}
	if summary.AllocatablePods != 110 {
		t.Fatalf("allocatable pods = %d, want 110", summary.AllocatablePods)
	}
}

func TestAddResourceListSumsAllocatablePodsAcrossNodes(t *testing.T) {
	summary := kubernetesResourceSummary{}
	nodeResources := corev1.ResourceList{
		corev1.ResourcePods: resource.MustParse("110"),
	}

	addResourceList(&summary, nodeResources, false)
	addResourceList(&summary, nodeResources, false)

	if summary.AllocatablePods != 220 {
		t.Fatalf("allocatable pods = %d, want 220", summary.AllocatablePods)
	}
}
