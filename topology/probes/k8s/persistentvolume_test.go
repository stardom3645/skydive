//go:build !windows

package k8s

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPVNodeAreLinkedByHostnameAffinity(t *testing.T) {
	pv := &v1.PersistentVolume{
		Spec: v1.PersistentVolumeSpec{
			NodeAffinity: &v1.VolumeNodeAffinity{
				Required: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{{
						MatchExpressions: []v1.NodeSelectorRequirement{{
							Key:      "kubernetes.io/hostname",
							Operator: v1.NodeSelectorOpIn,
							Values:   []string{"worker-01"},
						}},
					}},
				},
			},
		},
	}
	matching := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-01", Labels: map[string]string{"kubernetes.io/hostname": "worker-01"}}}
	other := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-02", Labels: map[string]string{"kubernetes.io/hostname": "worker-02"}}}

	if !pvNodeAreLinked(pv, matching) {
		t.Fatal("expected PV to link to the node selected by hostname affinity")
	}
	if pvNodeAreLinked(pv, other) {
		t.Fatal("did not expect PV to link to a node outside its affinity")
	}
}

func TestPVNodeAreLinkedRequiresAffinity(t *testing.T) {
	pv := &v1.PersistentVolume{}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-01"}}
	if pvNodeAreLinked(pv, node) {
		t.Fatal("PV without node affinity must not be linked to a node")
	}
}
