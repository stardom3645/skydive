package k8s

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestControllerLinksUseOwnerUID(t *testing.T) {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "k8s-prom-grafana", Namespace: "monitoring", UID: types.UID("deployment-uid")}}
	active := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: "k8s-prom-grafana-5dc7944964", Namespace: "monitoring", UID: types.UID("active-rs-uid"),
		OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: deployment.Name, UID: deployment.UID}},
	}}
	unrelated := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: "k8s-prom-grafana-lookalike", Namespace: "monitoring", UID: types.UID("other-rs-uid"),
		OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "other", UID: types.UID("other-deployment-uid")}},
	}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "k8s-prom-grafana-5dc7944964-pod", Namespace: "monitoring",
		OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: active.Name, UID: active.UID}},
	}}

	if !deploymentReplicaSetAreLinked(deployment, active) {
		t.Fatal("expected Deployment to link to its UID-owned ReplicaSet")
	}
	if deploymentReplicaSetAreLinked(deployment, unrelated) {
		t.Fatal("must not link a selector-like ReplicaSet owned by another Deployment")
	}
	if !replicaSetPodAreLinked(active, pod) {
		t.Fatal("expected ReplicaSet to link to its UID-owned Pod")
	}
}

func TestMetadataFieldsPublishRelationshipIdentity(t *testing.T) {
	ownerUID := types.UID("deployment-uid")
	replicaSet := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name: "grafana-rs", Namespace: "monitoring", UID: types.UID("rs-uid"),
		OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "grafana", UID: ownerUID}},
	}}
	metadata := NewMetadataFields(replicaSet)
	if metadata["UID"] != "rs-uid" {
		t.Fatalf("expected explicit UID metadata, got %#v", metadata["UID"])
	}
	owners, ok := metadata["OwnerReferences"].([]metav1.OwnerReference)
	if !ok || len(owners) != 1 || owners[0].UID != ownerUID {
		t.Fatalf("expected explicit OwnerReferences metadata, got %#v", metadata["OwnerReferences"])
	}
}

func TestStatefulSetAndDaemonSetOwnershipIsNotSelectorMembership(t *testing.T) {
	trueValue, falseValue := true, false
	for _, kind := range []string{"StatefulSet", "DaemonSet"} {
		t.Run(kind, func(t *testing.T) {
			meta := metav1.ObjectMeta{Name: "controller", Namespace: "ns", UID: types.UID("owner")}
			selector := &metav1.LabelSelector{MatchLabels: map[string]string{"app": "shared"}}
			ss := &appsv1.StatefulSet{ObjectMeta: meta, Spec: appsv1.StatefulSetSpec{Selector: selector}}
			ds := &appsv1.DaemonSet{ObjectMeta: meta, Spec: appsv1.DaemonSetSpec{Selector: selector}}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", Labels: map[string]string{"app": "shared"}}}
			linked := func() bool {
				if kind == "StatefulSet" {
					return statefulSetPodAreLinked(ss, pod)
				}
				return daemonSetPodAreLinked(ds, pod)
			}
			if linked() {
				t.Fatal("selector-only Pod linked as owned")
			}
			pod.OwnerReferences = []metav1.OwnerReference{{Kind: kind, Name: meta.Name, UID: meta.UID, Controller: &trueValue}}
			if !linked() {
				t.Fatal("UID owner not linked")
			}
			pod.OwnerReferences[0].UID = "recreated-old-uid"
			if linked() {
				t.Fatal("UID mismatch fell back to name")
			}
			pod.OwnerReferences[0].UID = meta.UID
			pod.OwnerReferences[0].Controller = &falseValue
			if linked() {
				t.Fatal("non-controller owner linked")
			}
			pod.OwnerReferences[0].Controller = &trueValue
			pod.Namespace = "other"
			if linked() {
				t.Fatal("foreign namespace linked")
			}
		})
	}
}

func TestControllerUIDCannotFallBackWhenParentIdentityMissing(t *testing.T) {
	parent := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "ns"}}
	child := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "api-rs", Namespace: "ns", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", UID: types.UID("old")}}}}
	if deploymentReplicaSetAreLinked(parent, child) {
		t.Fatal("known owner UID matched missing parent UID by name")
	}
}
