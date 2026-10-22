package k8s

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skydive-project/skydive/config"
	"github.com/skydive-project/skydive/graffiti/graph"
	"github.com/spf13/viper"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestEnabledK8sSubprobesRetainsSelectionAndResolvesDeploymentOwnership(t *testing.T) {
	storage := []string{"persistentvolume", "persistentvolumeclaim", "storageclass"}
	for _, test := range []struct {
		name             string
		configured, want []string
	}{
		{"all resources", nil, nil},
		{"explicit empty means all", []string{}, nil},
		{"legacy Deployment list", []string{"cluster", "pod", "deployment"}, append([]string{"cluster", "pod", "deployment", "replicaset"}, storage...)},
		{"explicit ReplicaSet", []string{"deployment", "replicaset", "pod"}, append([]string{"deployment", "replicaset", "pod"}, storage...)},
		{"unrelated list", []string{"node", "pod", "daemonset"}, append([]string{"node", "pod", "daemonset"}, storage...)},
		{"storage already enabled", []string{"deployment", "persistentvolume", "storageclass", "persistentvolumeclaim"}, []string{"deployment", "persistentvolume", "storageclass", "persistentvolumeclaim", "replicaset"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := append([]string(nil), test.configured...)
			// Spare capacity catches accidental append writes into Viper's slice.
			backing := make([]string, len(test.configured), len(test.configured)+8)
			copy(backing, test.configured)
			got := enabledK8sSubprobes(backing)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
			if len(original) > 0 && !reflect.DeepEqual(backing, original) {
				t.Fatalf("configured list changed: %v", backing)
			}
			if len(got) > 0 {
				got[0] = "mutated"
				if backing[0] == "mutated" {
					t.Fatal("resolved list aliases configuration")
				}
			}
		})
	}
}

// Exercise the real constructor, REST LIST/WATCH cache, metadata mapper and
// relationship linkers. The API is local; no production object is changed.
func TestReplicaSetListWatchRestoresDeploymentPodGraph(t *testing.T) {
	t.Run("initial LIST", func(t *testing.T) { testReplicaSetListWatchGraph(t, true) })
	t.Run("missing relation restored by WATCH", func(t *testing.T) { testReplicaSetListWatchGraph(t, false) })
}

func testReplicaSetListWatchGraph(t *testing.T, initialReplicaSet bool) {
	controller := true
	replicas := int32(2)
	deployment := &appsv1.Deployment{TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "rt1230-app", UID: "deployment-api", ResourceVersion: "1"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 0, AvailableReplicas: 0}}
	rs := &appsv1.ReplicaSet{TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSet"},
		ObjectMeta: metav1.ObjectMeta{Name: "api-76c485fff7", Namespace: deployment.Namespace, UID: "replicaset-api", ResourceVersion: "2",
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: deployment.Name, UID: deployment.UID, Controller: &controller}}}}
	pods := []corev1.Pod{}
	for i := 0; i < 2; i++ {
		pods = append(pods, corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("api-pod-%d", i), Namespace: deployment.Namespace, UID: types.UID(fmt.Sprintf("pod-%d", i)), ResourceVersion: "1",
			OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &controller}},
		}})
	}
	updates := make(chan map[string]interface{}, 4)
	var mu sync.Mutex
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resource := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		watching := r.URL.Query().Get("watch") == "true"
		mu.Lock()
		requests[fmt.Sprintf("%s/%t", resource, watching)]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if watching {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			for {
				if resource != "replicasets" {
					<-r.Context().Done()
					return
				}
				select {
				case <-r.Context().Done():
					return
				case update := <-updates:
					if err := json.NewEncoder(w).Encode(update); err != nil {
						return
					}
					w.(http.Flusher).Flush()
				}
			}
		}
		var list interface{}
		switch resource {
		case "deployments":
			list = &appsv1.DeploymentList{TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "DeploymentList"}, ListMeta: metav1.ListMeta{ResourceVersion: "1"}, Items: []appsv1.Deployment{*deployment}}
		case "pods":
			list = &corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}, ListMeta: metav1.ListMeta{ResourceVersion: "1"}, Items: pods}
		case "replicasets":
			items := []appsv1.ReplicaSet{}
			if initialReplicaSet {
				items = append(items, *rs.DeepCopy())
			}
			list = &appsv1.ReplicaSetList{TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ReplicaSetList"}, ListMeta: metav1.ListMeta{ResourceVersion: "1"}, Items: items}
		default:
			kinds := map[string]string{"persistentvolumes": "PersistentVolumeList", "persistentvolumeclaims": "PersistentVolumeClaimList", "storageclasses": "StorageClassList"}
			kind := kinds[resource]
			if kind == "" {
				http.NotFound(w, r)
				return
			}
			version := "v1"
			if resource == "storageclasses" {
				version = "storage.k8s.io/v1"
			}
			list = map[string]interface{}{"apiVersion": version, "kind": kind, "metadata": map[string]string{"resourceVersion": "1"}, "items": []interface{}{}}
		}
		_ = json.NewEncoder(w).Encode(list)
	}))
	t.Cleanup(server.Close)

	previous := config.GetConfig().Viper
	config.GetConfig().Viper = viper.New()
	t.Cleanup(func() { config.GetConfig().Viper = previous })
	// A real YAML override replaces the updated defaults with a legacy list.
	file := filepath.Join(t.TempDir(), "analyzer.yml")
	if err := os.WriteFile(file, []byte("analyzer:\n  topology:\n    k8s:\n      probes: [deployment, pod]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := config.LoadConfig(config.GetConfig().Viper, "file", []string{file}); err != nil {
		t.Fatal(err)
	}
	kubeconfig := filepath.Join(t.TempDir(), "test.kubeconfig")
	content := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: regression\n  cluster:\n    server: %s\ncontexts:\n- name: regression\n  context:\n    cluster: regression\n    user: test\ncurrent-context: regression\nusers:\n- name: test\n  user: {}\n", server.URL)
	if err := os.WriteFile(kubeconfig, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	backend, _ := graph.NewMemoryBackend()
	g := graph.NewGraph("test", backend, "test")
	const manager = "k8s-replicaset-regression"
	probe, err := newSingleK8sProbe(g, kubeconfig, manager, "regression", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { probe.Stop(); delete(subprobes, manager) })
	if GetSubprobe(manager, "replicaset") == nil {
		t.Fatal("legacy file override omitted required ReplicaSet collector")
	}
	if err := probe.Start(); err != nil {
		t.Fatal(err)
	}
	await := func(label string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out: %s", label)
	}
	await("LIST objects and ReplicaSet WATCH", func() bool {
		g.RLock()
		objects := g.GetNode("deployment-api") != nil && g.GetNode("pod-0") != nil && g.GetNode("pod-1") != nil
		g.RUnlock()
		mu.Lock()
		watching := requests["replicasets/true"] > 0 && requests["replicasets/false"] > 0
		mu.Unlock()
		return objects && watching
	})
	if !initialReplicaSet {
		g.RLock()
		if len(g.GetNodes(graph.Metadata{"Type": "replicaset"})) != 0 || len(g.GetEdges(graph.Metadata{"RelationType": "deployment"})) != 0 || len(g.GetEdges(graph.Metadata{"RelationType": "replicaset"})) != 0 {
			t.Error("missing ReplicaSet must not produce guessed ownership edges")
		}
		g.RUnlock()
	}
	updates <- map[string]interface{}{"type": "ADDED", "object": rs.DeepCopy()}
	await("UID ownership graph restored", func() bool {
		g.RLock()
		defer g.RUnlock()
		return len(g.GetNodes(graph.Metadata{"Type": "replicaset"})) == 1 && len(g.GetEdges(graph.Metadata{"RelationType": "deployment"})) == 1 && len(g.GetEdges(graph.Metadata{"RelationType": "replicaset"})) == 2
	})
	g.RLock()
	if n := g.GetNode(graph.Identifier(rs.UID)); n != nil {
		uid, _ := n.GetFieldString("K8s.UID")
		cluster, _ := n.GetFieldString(ClusterNameField)
		if uid != string(rs.UID) || cluster != "regression" {
			t.Errorf("missing identity metadata: UID=%s cluster=%s", uid, cluster)
		}
	}
	for _, edge := range g.GetEdges(graph.Metadata{"RelationType": "deployment"}) {
		if edge.Parent != graph.Identifier(deployment.UID) || edge.Child != graph.Identifier(rs.UID) {
			t.Errorf("wrong Deployment owner edge: %v", edge)
		}
	}
	for _, edge := range g.GetEdges(graph.Metadata{"RelationType": "replicaset"}) {
		if edge.Parent != graph.Identifier(rs.UID) || (edge.Child != "pod-0" && edge.Child != "pod-1") {
			t.Errorf("wrong ReplicaSet owner edge: %v", edge)
		}
	}
	g.RUnlock()
	// Re-LIST delivery against a retained graph reuses the existing UID node.
	GetSubprobe(manager, "replicaset").(*ResourceCache).OnAdd(rs.DeepCopy())
	// Repeated watch Add and Modified must reuse the UID node/edge IDs.
	rs.ResourceVersion = "3"
	updates <- map[string]interface{}{"type": "ADDED", "object": rs.DeepCopy()}
	rs.ResourceVersion = "4"
	updates <- map[string]interface{}{"type": "MODIFIED", "object": rs.DeepCopy()}
	await("watch update applied without duplicates", func() bool {
		g.RLock()
		defer g.RUnlock()
		n := g.GetNode(graph.Identifier(rs.UID))
		if n == nil {
			return false
		}
		version, _ := n.GetFieldString("K8s.Extra.ObjectMeta.ResourceVersion")
		return version == "4" && len(g.GetNodes(graph.Metadata{"Type": "replicaset"})) == 1 && len(g.GetEdges(graph.Metadata{"RelationType": "deployment"})) == 1 && len(g.GetEdges(graph.Metadata{"RelationType": "replicaset"})) == 2
	})
	updates <- map[string]interface{}{"type": "DELETED", "object": rs.DeepCopy()}
	await("deleted ReplicaSet removes obsolete ownership edges", func() bool {
		g.RLock()
		defer g.RUnlock()
		return g.GetNode(graph.Identifier(rs.UID)) == nil && len(g.GetEdges(graph.Metadata{"RelationType": "deployment"})) == 0 && len(g.GetEdges(graph.Metadata{"RelationType": "replicaset"})) == 0 && g.GetNode("deployment-api") != nil && g.GetNode("pod-0") != nil
	})
}

func TestReplicaSetOwnershipRejectsUnverifiedRelations(t *testing.T) {
	controller, nonController := true, false
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "app", UID: "deployment"}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "api-rs", Namespace: "app", UID: "rs", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api", UID: deployment.UID, Controller: &controller}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-rs-pod", Namespace: "app", UID: "pod", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs", UID: rs.UID, Controller: &controller}}}}
	for _, test := range []struct {
		name   string
		mutate func(*metav1.ObjectMeta)
	}{
		{"same name different owner UID", func(meta *metav1.ObjectMeta) { meta.OwnerReferences[0].UID = "old-owner" }},
		{"foreign namespace", func(meta *metav1.ObjectMeta) { meta.Namespace = "other" }},
		{"non-controller reference", func(meta *metav1.ObjectMeta) { meta.OwnerReferences[0].Controller = &nonController }},
		{"no owner despite matching name", func(meta *metav1.ObjectMeta) { meta.OwnerReferences = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			otherRS, otherPod := rs.DeepCopy(), pod.DeepCopy()
			test.mutate(&otherRS.ObjectMeta)
			test.mutate(&otherPod.ObjectMeta)
			if deploymentReplicaSetAreLinked(deployment, otherRS) {
				t.Fatal("unverified ReplicaSet linked to Deployment")
			}
			if replicaSetPodAreLinked(rs, otherPod) {
				t.Fatal("unverified Pod linked to ReplicaSet")
			}
		})
	}
}
