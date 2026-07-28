//go:build !windows

package k8s

import (
	"testing"
	"time"

	"github.com/skydive-project/skydive/graffiti/graph"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func assertMetadataString(t *testing.T, metadata graph.Metadata, path, expected string) {
	t.Helper()
	value, err := metadata.GetFieldString(path)
	if err != nil {
		t.Fatalf("expected %s in metadata: %v", path, err)
	}
	if value != expected {
		t.Fatalf("unexpected %s: got %q, want %q", path, value, expected)
	}
}

func TestStorageResourceMetadataPreservesTimestampAndCapacity(t *testing.T) {
	created := metav1.NewTime(time.Date(2026, 7, 28, 3, 4, 5, 0, time.UTC))
	mode := corev1.PersistentVolumeFilesystem
	storageClassName := "local-path"

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim-a", UID: "claim-a", CreationTimestamp: created},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
			VolumeName:       "pv-a",
			VolumeMode:       &mode,
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("50Gi"),
			}},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("50Gi"),
			},
		},
	}
	_, pvcMetadata := (&persistentVolumeClaimHandler{}).Map(pvc)
	assertMetadataString(t, pvcMetadata, "K8s.CreationTimestamp", "2026-07-28T03:04:05Z")
	assertMetadataString(t, pvcMetadata, "K8s.RequestedCapacity", "50Gi")
	assertMetadataString(t, pvcMetadata, "K8s.StatusCapacity", "50Gi")

	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-a", UID: "pv-a", CreationTimestamp: created},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("50Gi")},
		},
	}
	_, pvMetadata := (&persistentVolumeHandler{}).Map(pv)
	assertMetadataString(t, pvMetadata, "K8s.CreationTimestamp", "2026-07-28T03:04:05Z")
	assertMetadataString(t, pvMetadata, "K8s.Capacity.storage", "50Gi")

	storageClass := &storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: "local-path", UID: "local-path", CreationTimestamp: created},
		Provisioner: "rancher.io/local-path",
	}
	_, storageClassMetadata := (&storageClassHandler{}).Map(storageClass)
	assertMetadataString(t, storageClassMetadata, "K8s.CreationTimestamp", "2026-07-28T03:04:05Z")
}
