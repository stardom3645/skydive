/*
 * Copyright 2018 Red Hat
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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/skydive-project/skydive/config"
	"github.com/skydive-project/skydive/graffiti/graph"
	"github.com/skydive-project/skydive/probe"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sProbe defines the k8s probe
type K8sProbe struct {
	*Probe
	clusterSubprobe Subprobe
	cleanupOnStop   bool
}

type MultiK8sProbe struct {
	graph  *graph.Graph
	probes []*K8sProbe
}

// Start the k8s probe
func (p *K8sProbe) Start() error {
	if p == nil || p.Probe == nil || p.clusterSubprobe == nil {
		return nil
	}
	if err := p.clusterSubprobe.Start(); err != nil {
		return err
	}

	return p.Probe.Start()
}

// Stop the k8s probe and remove stale Kubernetes graph nodes.
func (p *K8sProbe) Stop() {
	if p == nil {
		return
	}
	if p.clusterSubprobe != nil {
		p.clusterSubprobe.Stop()
	}
	if p.Probe == nil {
		return
	}
	p.Probe.Stop()
	if p.cleanupOnStop {
		CleanupK8sGraph(p.graph)
		ResetK8sRuntimeState()
	}
}

func (p *MultiK8sProbe) Start() error {
	if p == nil {
		return nil
	}
	for _, child := range p.probes {
		if err := child.Start(); err != nil {
			for _, started := range p.probes {
				if started == child {
					break
				}
				started.Stop()
			}
			return err
		}
	}
	return nil
}

func (p *MultiK8sProbe) Stop() {
	if p == nil {
		return
	}
	for _, child := range p.probes {
		if child == nil {
			continue
		}
		child.Stop()
	}
	CleanupK8sGraph(p.graph)
	ResetK8sRuntimeState()
}

// NewConfig returns a new Kubernetes configuration object
func NewConfig(kubeconfigPath string) (*rest.Config, *clientcmd.ClientConfig, error) {
	var err error
	var cc *rest.Config

	if kubeconfigPath == "" {
		cc, err := rest.InClusterConfig()
		if err == nil {
			return cc, nil, nil
		}
	}

	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{})

	cc, err = kubeconfig.ClientConfig()
	if err == nil {
		return cc, &kubeconfig, nil
	}

	kubeconfig = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{})

	cc, err = kubeconfig.ClientConfig()
	if err == nil {
		return cc, &kubeconfig, nil
	}

	return nil, nil, fmt.Errorf("Failed to load Kubernetes config: %s", err)
}

func moldKubernetesSelectionEnabled() bool {
	if !config.GetBool("mold.kubernetes.enforceSelection") {
		return true
	}
	stateFile := config.GetString("mold.kubernetes.stateFile")
	if stateFile == "" {
		return false
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return false
	}
	var state struct {
		Enabled    bool     `json:"enabled"`
		ClusterIDs []string `json:"clusterIds"`
		ClusterID  string   `json:"clusterId"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return false
	}
	if !state.Enabled {
		return false
	}
	return len(normalizeClusterIDs(state.ClusterIDs, state.ClusterID)) > 0
}

func shouldSkipMissingKubeconfig(kubeconfigPath string) bool {
	if kubeconfigPath == "" {
		return false
	}
	_, err := os.Stat(kubeconfigPath)
	return os.IsNotExist(err)
}

// ShouldSkipK8sProbe returns true when Mold-managed Kubernetes collection is not selected yet.
func ShouldSkipK8sProbe() bool {
	if !moldKubernetesSelectionEnabled() {
		return true
	}
	entries := moldKubernetesSelectedClusterEntries()
	if len(entries) == 0 {
		return shouldSkipMissingKubeconfig(config.GetString("analyzer.topology.k8s.config_file"))
	}
	for _, entry := range entries {
		if !shouldSkipMissingKubeconfig(entry.Path) {
			return false
		}
	}
	return true
}

// NewK8sProbe returns a new Kubernetes probe
func NewK8sProbe(g *graph.Graph) (probe.Handler, error) {
	if ShouldSkipK8sProbe() {
		return nil, nil
	}
	CleanupK8sGraph(g)
	ResetK8sRuntimeState()

	entries := moldKubernetesSelectedClusterEntries()
	if len(entries) == 0 {
		defaultPath := config.GetString("analyzer.topology.k8s.config_file")
		entries = []moldKubernetesClusterEntry{{Path: defaultPath}}
	}

	if len(entries) == 1 {
		return newSingleK8sProbe(g, entries[0].Path, Manager, entries[0].Name, true)
	}

	probes := make([]*K8sProbe, 0, len(entries))
	for index, entry := range entries {
		runtimeManager := fmt.Sprintf("%s#%d", Manager, index)
		child, err := newSingleK8sProbe(g, entry.Path, runtimeManager, entry.Name, false)
		if err != nil {
			return nil, err
		}
		if child != nil {
			probes = append(probes, child)
		}
	}
	if len(probes) == 0 {
		return nil, nil
	}
	return &MultiK8sProbe{graph: g, probes: probes}, nil
}

func newSingleK8sProbe(g *graph.Graph, kubeconfigPath, runtimeManager, clusterNameOverride string, cleanupOnStop bool) (*K8sProbe, error) {
	enabledSubprobes := config.GetStringSlice("analyzer.topology.k8s.probes")
	// Storage is part of the Netdive Kubernetes product topology. Older product
	// configurations predate these resources and keep an explicit allow-list,
	// so append only the missing storage probes while preserving every operator
	// choice already present in the list.
	if len(enabledSubprobes) > 0 {
		seen := make(map[string]bool, len(enabledSubprobes))
		for _, name := range enabledSubprobes {
			seen[name] = true
		}
		for _, name := range []string{"persistentvolume", "persistentvolumeclaim", "storageclass"} {
			if !seen[name] {
				enabledSubprobes = append(enabledSubprobes, name)
			}
		}
	}

	clientconfig, kubeconfig, err := NewConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(clientconfig)
	if err != nil {
		return nil, fmt.Errorf("Failed to create Kubernetes client: %s", err)
	}

	clusterName := getClusterName(kubeconfig, clusterNameOverride)

	subprobeHandlers := map[string]SubprobeHandler{
		"configmap":             newConfigMapProbe,
		"container":             newContainerProbe,
		"cronjob":               newCronJobProbe,
		"daemonset":             newDaemonSetProbe,
		"deployment":            newDeploymentProbe,
		"endpoints":             newEndpointsProbe,
		"ingress":               newIngressProbe,
		"job":                   newJobProbe,
		"namespace":             newNamespaceProbe,
		"networkpolicy":         newNetworkPolicyProbe,
		"node":                  newNodeProbe,
		"persistentvolume":      newPersistentVolumeProbe,
		"persistentvolumeclaim": newPersistentVolumeClaimProbe,
		"pod":                   newPodProbe,
		"replicaset":            newReplicaSetProbe,
		"replicationcontroller": newReplicationControllerProbe,
		"secret":                newSecretProbe,
		"service":               newServiceProbe,
		"statefulset":           newStatefulSetProbe,
		"storageclass":          newStorageClassProbe,
	}

	InitSubprobes(enabledSubprobes, subprobeHandlers, clientset, g, runtimeManager, clusterName)

	linkerHandlers := []LinkHandler{
		newContainerDockerLinker(runtimeManager),
		newReplicaSetPodLinker(runtimeManager),
		newDeploymentReplicaSetLinker(runtimeManager),
		newDaemonSetPodLinker(runtimeManager),
		newPodContainerLinker(runtimeManager),
		newPodConfigMapLinker(runtimeManager),
		newPodSecretLinker(runtimeManager),
		newHostNodeLinker(runtimeManager),
		newNodePodLinker(runtimeManager),
		newIngressServiceLinker(runtimeManager),
		newNetworkPolicyLinker(runtimeManager),
		newServiceEndpointsLinker(runtimeManager),
		newServicePodLinker(runtimeManager),
		newStatefulSetPodLinker(runtimeManager),
		newPodPVCLinker(runtimeManager),
		newPVPVCLinker(runtimeManager),
		newStorageClassPVCLinker(runtimeManager),
		newStorageClassPVLinker(runtimeManager),
		newPVNodeLinker(runtimeManager),
	}

	linkers := InitLinkers(linkerHandlers, g)

	verifiers := []probe.Handler{}
	clusterSubprobe := initClusterSubprobe(g, runtimeManager, clusterName)
	clusterProbe, _ := clusterSubprobe.(*clusterCache)

	probe := &K8sProbe{
		Probe:           NewProbe(g, runtimeManager, subprobes[runtimeManager], clusterProbe, linkers, verifiers),
		clusterSubprobe: clusterSubprobe,
		cleanupOnStop:   cleanupOnStop,
	}

	probe.AppendClusterLinkers(
		"namespace",
		"node",
		"persistentvolume",
		"storageclass",
	)

	probe.AppendNamespaceLinkers(
		"configmap",
		"cronjob",
		"deployment",
		"daemonset",
		"endpoints",
		"ingress",
		"job",
		"networkpolicy",
		"pod",
		"persistentvolumeclaim",
		"replicaset",
		"replicationcontroller",
		"secret",
		"service",
		"statefulset",
	)

	return probe, nil
}

func getClusterName(kubeconfig *clientcmd.ClientConfig, clusterNameOverride string) string {
	if clusterName := strings.TrimSpace(config.GetString("analyzer.topology.k8s.cluster_name")); clusterName != "" {
		return clusterName
	}
	if clusterName := strings.TrimSpace(clusterNameOverride); clusterName != "" {
		return clusterName
	}
	if clusterName := moldKubernetesSelectedClusterName(); clusterName != "" {
		return clusterName
	}

	clusterName := "cluster"
	if kubeconfig != nil {
		rawconfig, err := (*kubeconfig).RawConfig()
		if err == nil {
			if context := rawconfig.Contexts[rawconfig.CurrentContext]; context != nil {
				if context.Cluster != "" {
					clusterName = context.Cluster
				}
			}
		}
	}
	return clusterName
}

type moldKubernetesClusterEntry struct {
	ID   string
	Name string
	Path string
}

func normalizeClusterIDs(ids []string, fallback string) []string {
	normalized := make([]string, 0, len(ids)+1)
	seen := make(map[string]bool)
	appendID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		normalized = append(normalized, id)
	}
	for _, id := range ids {
		appendID(id)
	}
	appendID(fallback)
	return normalized
}

func moldKubernetesSelectedClusterEntries() []moldKubernetesClusterEntry {
	stateFile := config.GetString("mold.kubernetes.stateFile")
	if strings.TrimSpace(stateFile) == "" {
		return nil
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil
	}
	var state struct {
		Enabled      bool     `json:"enabled"`
		ClusterIDs   []string `json:"clusterIds"`
		ClusterNames []string `json:"clusterNames"`
		ClusterID    string   `json:"clusterId"`
		ClusterName  string   `json:"clusterName"`
	}
	if err := json.Unmarshal(data, &state); err != nil || !state.Enabled {
		return nil
	}

	ids := normalizeClusterIDs(state.ClusterIDs, state.ClusterID)
	if len(ids) == 0 {
		return nil
	}
	names := state.ClusterNames
	if len(names) == 0 && strings.TrimSpace(state.ClusterName) != "" {
		names = []string{state.ClusterName}
	}
	entries := make([]moldKubernetesClusterEntry, 0, len(ids))
	basePath := config.GetString("analyzer.topology.k8s.config_file")
	for index, id := range ids {
		entry := moldKubernetesClusterEntry{
			ID:   id,
			Path: filepath.Join(filepath.Dir(basePath), sanitizeClusterFileName(id)+".kubeconfig"),
		}
		if index < len(names) {
			entry.Name = strings.TrimSpace(names[index])
		}
		entries = append(entries, entry)
	}
	return entries
}

func sanitizeClusterFileName(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

func moldKubernetesSelectedClusterName() string {
	stateFile := config.GetString("mold.kubernetes.stateFile")
	if strings.TrimSpace(stateFile) == "" {
		return ""
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}
	var state struct {
		Enabled     bool   `json:"enabled"`
		ClusterName string `json:"clusterName"`
	}
	if err := json.Unmarshal(data, &state); err != nil || !state.Enabled {
		return ""
	}
	return strings.TrimSpace(state.ClusterName)
}
