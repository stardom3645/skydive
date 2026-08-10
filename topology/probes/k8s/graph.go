/*
 * Copyright 2017 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy ofthe License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specificlanguage governing permissions and
 * limitations under the License.
 *
 */

package k8s

import (
	"time"

	"github.com/skydive-project/skydive/graffiti/filters"
	"github.com/skydive-project/skydive/graffiti/graph"
	"github.com/skydive-project/skydive/graffiti/logging"
	"github.com/skydive-project/skydive/probe"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Manager is the manager value for Kubernetes
	Manager = "k8s"
	// KubeKey is the metadata area for k8s specific fields
	KubeKey = "K8s"
	// ExtraKey is the metadata area for k8s extra fields
	ExtraKey = "K8s.Extra"

	ClusterNameField = "ClusterName"
)

// MetadataField is generates full path of a k8s specific field
func MetadataField(field string) string {
	return KubeKey + "." + field
}

// MetadataFields generates full path of a list of k8s specific fields
func MetadataFields(fields ...string) []string {
	kubeFields := make([]string, len(fields))
	for i, field := range fields {
		kubeFields[i] = MetadataField(field)
	}
	return kubeFields
}

// NewMetadataFields creates internal k8s node metadata struct
func NewMetadataFields(o metav1.Object) graph.Metadata {
	m := graph.Metadata{}
	m["Name"] = o.GetName()
	m["Namespace"] = o.GetNamespace()
	m["UID"] = string(o.GetUID())
	m["Labels"] = o.GetLabels()
	m["Annotations"] = o.GetAnnotations()
	m["OwnerReferences"] = o.GetOwnerReferences()
	if createdAt := o.GetCreationTimestamp(); !createdAt.IsZero() {
		// metav1.Time contains an embedded time.Time whose fields are not
		// preserved by the generic graph normalizer. Publish the RFC3339 value
		// explicitly so every Kubernetes resource retains its creation time.
		m["CreationTimestamp"] = createdAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return m
}

// controlledBy uses the Kubernetes controller identity rather than label
// coincidence. UID is authoritative; name is retained only for objects whose
// owner UID was not collected by an older API source.
func controlledBy(child, parent metav1.Object, parentKind string) bool {
	parentUID := string(parent.GetUID())
	for _, owner := range child.GetOwnerReferences() {
		if owner.Kind != parentKind {
			continue
		}
		if string(owner.UID) != "" && parentUID != "" {
			if string(owner.UID) == parentUID {
				return true
			}
			continue
		}
		if owner.Name == parent.GetName() {
			return true
		}
	}
	return false
}

// NewMetadata creates a k8s node base metadata struct
func NewMetadata(manager, ty string, kubeMeta graph.Metadata, extra interface{}, name string) graph.Metadata {
	m := graph.Metadata{}
	m["Manager"] = manager
	m["Type"] = ty
	m["Name"] = name
	m.SetFieldAndNormalize(KubeKey, map[string]interface{}(kubeMeta))
	if extra != nil {
		m.SetFieldAndNormalize(ExtraKey, extra)
	}
	return m
}

// SetState field of node metadata
func SetState(m *graph.Metadata, isUp bool) {
	state := "DOWN"
	if isUp {
		state = "UP"
	}
	m.SetField("State", state)
}

// NewEdgeMetadata creates a new edge metadata
func NewEdgeMetadata(manager, name string) graph.Metadata {
	m := graph.Metadata{
		"Manager":      manager,
		"RelationType": name,
	}
	return m
}

func isTheSameCluster(aObj, bObj *graph.Node) bool {
	aCl, aErr := aObj.GetFieldString(ClusterNameField)
	bCl, bErr := bObj.GetFieldString(ClusterNameField)
	if aErr != nil || bErr != nil {
		logging.GetLogger().Warning("ClusterNameFields are not defined \n")
		return true
	}
	return aCl == bCl
}

func newTypesFilter(manager string, types ...string) *filters.Filter {
	filtersArray := make([]*filters.Filter, len(types))
	for i, ty := range types {
		filtersArray[i] = filters.NewTermStringFilter("Type", ty)
	}
	return filters.NewAndFilter(
		filters.NewTermStringFilter("Manager", manager),
		filters.NewOrFilter(filtersArray...),
	)
}

func CleanupK8sGraph(g *graph.Graph) {
	if g == nil {
		return
	}

	g.Lock()
	defer g.Unlock()

	filter := graph.NewElementFilter(filters.NewTermStringFilter("Manager", Manager))
	if err := g.DelNodes(filter); err != nil {
		logging.GetLogger().Errorf("Failed to cleanup Kubernetes graph nodes: %s", err)
	}
}

func ResetK8sRuntimeState() {
	resetAllSubprobes()
	resetKubeCaches()
}

func newObjectIndexerFromFilter(g *graph.Graph, h graph.ListenerHandler, filter *filters.Filter, indexes ...string) *graph.MetadataIndexer {
	filtersArray := make([]*filters.Filter, len(indexes)+1)
	filtersArray[0] = filter
	for i, index := range indexes {
		filtersArray[i+1] = filters.NewNotNullFilter(index)
	}
	m := graph.NewElementFilter(filters.NewAndFilter(filtersArray...))
	return graph.NewMetadataIndexer(g, h, m, indexes...)
}

func newResourceIndexer(g *graph.Graph, manager, ty string, attrs []string) *graph.MetadataIndexer {
	cache := GetSubprobe(manager, ty)
	if cache == nil {
		return nil
	}
	indexes := append([]string{}, attrs...)
	hasClusterName := false
	for _, index := range indexes {
		if index == ClusterNameField {
			hasClusterName = true
			break
		}
	}
	if !hasClusterName {
		indexes = append(indexes, ClusterNameField)
	}
	metadata := graph.Metadata{"Manager": Manager, "Type": ty}
	indexer := graph.NewMetadataIndexer(g, cache, metadata, indexes...)
	indexer.Start()
	return indexer
}

func newResourceLinker(g *graph.Graph, srcIndexer, dstIndexer *graph.MetadataIndexer, edgeMetadata graph.Metadata) probe.Handler {
	if srcIndexer == nil || dstIndexer == nil {
		return nil
	}

	ml := graph.NewMetadataIndexerLinker(g, srcIndexer, dstIndexer, edgeMetadata)

	linker := &Linker{
		ResourceLinker: ml.ResourceLinker,
	}
	ml.AddEventListener(linker)

	return linker
}

func objectToNode(g *graph.Graph, object metav1.Object) (node *graph.Node) {
	return g.GetNode(graph.Identifier(object.GetUID()))
}

func objectsToNodes(g *graph.Graph, objects []metav1.Object) (nodes []*graph.Node) {
	for _, obj := range objects {
		if node := objectToNode(g, obj); node != nil {
			nodes = append(nodes, node)
		}
	}
	return
}
