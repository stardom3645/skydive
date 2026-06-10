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
	CleanupK8sGraph(p.graph)
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
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return false
	}
	return state.Enabled
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
	return shouldSkipMissingKubeconfig(config.GetString("analyzer.topology.k8s.config_file"))
}

// NewK8sProbe returns a new Kubernetes probe
func NewK8sProbe(g *graph.Graph) (*K8sProbe, error) {
	if ShouldSkipK8sProbe() {
		return nil, nil
	}
	CleanupK8sGraph(g)
	kubeconfigPath := config.GetString("analyzer.topology.k8s.config_file")
	enabledSubprobes := config.GetStringSlice("analyzer.topology.k8s.probes")

	clientconfig, kubeconfig, err := NewConfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(clientconfig)
	if err != nil {
		return nil, fmt.Errorf("Failed to create Kubernetes client: %s", err)
	}

	clusterName := getClusterName(kubeconfig)

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

	InitSubprobes(enabledSubprobes, subprobeHandlers, clientset, g, Manager, clusterName)

	linkerHandlers := []LinkHandler{
		newContainerDockerLinker,
		newReplicaSetPodLinker,
		newDeploymentReplicaSetLinker,
		newDaemonSetPodLinker,
		newPodContainerLinker,
		newPodConfigMapLinker,
		newPodSecretLinker,
		newHostNodeLinker,
		newNodePodLinker,
		newIngressServiceLinker,
		newNetworkPolicyLinker,
		newServiceEndpointsLinker,
		newServicePodLinker,
		newStatefulSetPodLinker,
		newPodPVCLinker,
		newPVPVCLinker,
		newStorageClassPVCLinker,
		newStorageClassPVLinker,
	}

	linkers := InitLinkers(linkerHandlers, g)

	verifiers := []probe.Handler{}

	probe := &K8sProbe{
		Probe:           NewProbe(g, Manager, subprobes[Manager], linkers, verifiers),
		clusterSubprobe: initClusterSubprobe(g, Manager, clusterName),
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

func getClusterName(kubeconfig *clientcmd.ClientConfig) string {
	if clusterName := strings.TrimSpace(config.GetString("analyzer.topology.k8s.cluster_name")); clusterName != "" {
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
