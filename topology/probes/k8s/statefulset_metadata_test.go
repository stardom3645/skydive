//go:build !windows

package k8s

import (
	"testing"
	"time"

	v1apps "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStatefulSetMetadataPreservesCreationTimestamp(t *testing.T) {
	statefulSet := &v1apps.StatefulSet{ObjectMeta: metav1.ObjectMeta{
		Name:              "prometheus-k8s-prom-kube-prometheus-s-prometheus",
		Namespace:         "monitoring",
		UID:               "statefulset-a",
		CreationTimestamp: metav1.NewTime(time.Date(2026, 7, 24, 0, 33, 40, 0, time.UTC)),
	}}

	_, metadata := (&statefulSetHandler{}).Map(statefulSet)
	value, err := metadata.GetFieldString("K8s.CreationTimestamp")
	if err != nil {
		t.Fatalf("expected K8s.CreationTimestamp in graph metadata: %v", err)
	}
	if value != "2026-07-24T00:33:40Z" {
		t.Fatalf("unexpected creation timestamp: got %q", value)
	}
}
