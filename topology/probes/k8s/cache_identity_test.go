package k8s

import (
	"github.com/skydive-project/skydive/graffiti/graph"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"testing"
)

func TestResourceCacheRejectsRecreatedNameAndForeignCluster(t *testing.T) {
	index := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	object := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "ns", UID: "new-uid"}}
	if err := index.Add(object); err != nil {
		t.Fatal(err)
	}
	resource := &ResourceCache{KubeCache: &KubeCache{cache: index}, clusterName: "cluster-a"}
	backend, _ := graph.NewMemoryBackend()
	g := graph.NewGraph("test", backend, "test")
	create := func(id, uid, cluster string) *graph.Node {
		m := NewMetadata(Manager, "deployment", graph.Metadata{"Name": "api", "Namespace": "ns", "UID": uid}, nil, "api")
		m[ClusterNameField] = cluster
		n, err := g.NewNode(graph.Identifier(id), m)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	old := create("old-uid", "old-uid", "cluster-a")
	foreign := create("foreign", "new-uid", "cluster-b")
	current := create("new-uid", "new-uid", "cluster-a")
	if resource.GetByNode(old) != nil {
		t.Fatal("old UID matched replacement cache entry")
	}
	if resource.GetByNode(foreign) != nil {
		t.Fatal("foreign cluster matched local cache")
	}
	if resource.GetByNode(current) != object {
		t.Fatal("current object not resolved")
	}
}
