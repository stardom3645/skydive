package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const kubernetesClientCacheTTL = 10 * time.Minute

type kubernetesAPIConnectionStatus string

const (
	kubernetesConnected            kubernetesAPIConnectionStatus = "CONNECTED"
	kubernetesSyncing              kubernetesAPIConnectionStatus = "SYNCING"
	kubernetesHealthy              kubernetesAPIConnectionStatus = "HEALTHY"
	kubernetesDelayed              kubernetesAPIConnectionStatus = "DELAYED"
	kubernetesDisconnected         kubernetesAPIConnectionStatus = "DISCONNECTED"
	kubernetesAuthenticationFailed kubernetesAPIConnectionStatus = "AUTHENTICATION_FAILED"
	kubernetesPermissionDenied     kubernetesAPIConnectionStatus = "PERMISSION_DENIED"
)

type kubernetesCollectionState struct {
	Status     kubernetesAPIConnectionStatus `json:"status"`
	LastSyncAt *time.Time                    `json:"lastSyncAt,omitempty"`
	LastError  string                        `json:"lastError,omitempty"`
}

type cachedKubernetesClient struct {
	client    kubernetes.Interface
	config    *rest.Config
	createdAt time.Time
	stopCh    chan struct{}
}

var kubernetesClientRegistry = struct {
	sync.RWMutex
	clients map[string]cachedKubernetesClient
	states  map[string]kubernetesCollectionState
}{clients: make(map[string]cachedKubernetesClient), states: make(map[string]kubernetesCollectionState)}

func getMoldKubernetesClient(cluster moldKubernetesCluster) (kubernetes.Interface, *rest.Config, error) {
	now := time.Now().UTC()
	kubernetesClientRegistry.RLock()
	cached, ok := kubernetesClientRegistry.clients[cluster.ID]
	kubernetesClientRegistry.RUnlock()
	if ok && now.Sub(cached.createdAt) < kubernetesClientCacheTTL {
		return cached.client, cached.config, nil
	}

	setKubernetesCollectionState(cluster.ID, kubernetesSyncing, nil)
	kubeconfigData, err := getMoldKubernetesConfig(cluster.ID)
	if err != nil {
		setKubernetesCollectionState(cluster.ID, classifyKubernetesConnectionError(err), err)
		return nil, nil, err
	}
	clientConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfigData))
	if err != nil {
		setKubernetesCollectionState(cluster.ID, kubernetesAuthenticationFailed, err)
		return nil, nil, newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes kubeconfig 형식이 올바르지 않습니다.", err)
	}
	clientConfig.Timeout = 15 * time.Second
	clientConfig.QPS = 20
	clientConfig.Burst = 40
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		setKubernetesCollectionState(cluster.ID, classifyKubernetesConnectionError(err), err)
		return nil, nil, newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes client를 생성하지 못했습니다.", err)
	}
	stopCh := make(chan struct{})
	startKubernetesListWatch(cluster.ID, clientset, stopCh)

	kubernetesClientRegistry.Lock()
	if previous, exists := kubernetesClientRegistry.clients[cluster.ID]; exists && previous.stopCh != nil {
		close(previous.stopCh)
	}
	kubernetesClientRegistry.clients[cluster.ID] = cachedKubernetesClient{client: clientset, config: rest.CopyConfig(clientConfig), createdAt: now, stopCh: stopCh}
	kubernetesClientRegistry.Unlock()
	setKubernetesCollectionState(cluster.ID, kubernetesConnected, nil)
	return clientset, clientConfig, nil
}

func startKubernetesListWatch(clusterID string, client kubernetes.Interface, stopCh chan struct{}) {
	factory := informers.NewSharedInformerFactory(client, 10*time.Minute)
	informerList := []cache.SharedIndexInformer{
		factory.Core().V1().Nodes().Informer(),
		factory.Core().V1().Namespaces().Informer(),
		factory.Core().V1().Pods().Informer(),
		factory.Core().V1().Services().Informer(),
		factory.Discovery().V1().EndpointSlices().Informer(),
	}
	for _, informer := range informerList {
		_ = informer.SetWatchErrorHandler(func(_ *cache.Reflector, err error) {
			if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
				return
			}
			status := classifyKubernetesConnectionError(err)
			if status == kubernetesDisconnected {
				status = kubernetesDelayed
			}
			setKubernetesCollectionState(clusterID, status, err)
		})
	}
	factory.Start(stopCh)
}

func setKubernetesCollectionState(clusterID string, status kubernetesAPIConnectionStatus, err error) {
	state := kubernetesCollectionState{Status: status}
	if status == kubernetesHealthy {
		now := time.Now().UTC()
		state.LastSyncAt = &now
	}
	kubernetesClientRegistry.Lock()
	previous := kubernetesClientRegistry.states[clusterID]
	if state.LastSyncAt == nil {
		state.LastSyncAt = previous.LastSyncAt
	}
	if err != nil {
		state.LastError = sanitizeKubernetesTestError(err)
	}
	kubernetesClientRegistry.states[clusterID] = state
	kubernetesClientRegistry.Unlock()
}

func getKubernetesCollectionState(clusterID string) kubernetesCollectionState {
	kubernetesClientRegistry.RLock()
	defer kubernetesClientRegistry.RUnlock()
	state, ok := kubernetesClientRegistry.states[clusterID]
	if !ok {
		return kubernetesCollectionState{Status: kubernetesDisconnected}
	}
	if state.Status == kubernetesHealthy && state.LastSyncAt != nil && time.Since(*state.LastSyncAt) > 10*time.Minute {
		state.Status = kubernetesDelayed
	}
	return state
}

func stopKubernetesClient(clusterID string) {
	kubernetesClientRegistry.Lock()
	if cached, ok := kubernetesClientRegistry.clients[clusterID]; ok {
		if cached.stopCh != nil {
			close(cached.stopCh)
		}
		delete(kubernetesClientRegistry.clients, clusterID)
	}
	delete(kubernetesClientRegistry.states, clusterID)
	kubernetesClientRegistry.Unlock()
}

func stopDeselectedKubernetesClients(previous, current moldKubernetesSelection) {
	currentIDs := make(map[string]bool)
	for _, id := range normalizedSelectionClusterIDs(current) {
		currentIDs[id] = true
	}
	for _, id := range normalizedSelectionClusterIDs(previous) {
		if !currentIDs[id] {
			stopKubernetesClient(id)
		}
	}
}

func classifyKubernetesConnectionError(err error) kubernetesAPIConnectionStatus {
	if apierrors.IsUnauthorized(err) || strings.Contains(strings.ToLower(err.Error()), "unauthorized") {
		return kubernetesAuthenticationFailed
	}
	if apierrors.IsForbidden(err) || strings.Contains(strings.ToLower(err.Error()), "forbidden") {
		return kubernetesPermissionDenied
	}
	return kubernetesDisconnected
}

func resolveMoldKubernetesCluster(clusterID string) (moldKubernetesCluster, error) {
	clusters, err := listMoldKubernetesClusters()
	if err != nil {
		return moldKubernetesCluster{}, err
	}
	for _, cluster := range clusters {
		if cluster.ID == clusterID {
			return cluster, nil
		}
	}
	return moldKubernetesCluster{}, newVMConsoleAPIError(http.StatusNotFound, "Kubernetes 클러스터를 찾을 수 없습니다.", nil)
}

type kubernetesObjectReference struct {
	UID       string `json:"uid,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type kubernetesConditionDetail struct {
	Type               string     `json:"type"`
	Status             string     `json:"status"`
	Reason             string     `json:"reason,omitempty"`
	Message            string     `json:"message,omitempty"`
	LastHeartbeatTime  *time.Time `json:"lastHeartbeatTime,omitempty"`
	LastTransitionTime *time.Time `json:"lastTransitionTime,omitempty"`
	DurationSeconds    int64      `json:"durationSeconds,omitempty"`
}

type kubernetesNodeDetail struct {
	ClusterID                          string                      `json:"clusterId"`
	UID                                string                      `json:"uid"`
	Name                               string                      `json:"name"`
	Roles                              []string                    `json:"roles"`
	InternalIP                         string                      `json:"internalIp,omitempty"`
	PodCIDRs                           []string                    `json:"podCidrs,omitempty"`
	KubernetesVersion                  string                      `json:"kubernetesVersion,omitempty"`
	OSImage                            string                      `json:"osImage,omitempty"`
	KernelVersion                      string                      `json:"kernelVersion,omitempty"`
	Architecture                       string                      `json:"architecture,omitempty"`
	ContainerRuntime                   string                      `json:"containerRuntime,omitempty"`
	CreatedAt                          time.Time                   `json:"createdAt"`
	Conditions                         []kubernetesConditionDetail `json:"conditions"`
	Unschedulable                      bool                        `json:"unschedulable"`
	Taints                             []corev1.Taint              `json:"taints,omitempty"`
	Labels                             map[string]string           `json:"labels,omitempty"`
	Capacity                           corev1.ResourceList         `json:"capacity"`
	Allocatable                        corev1.ResourceList         `json:"allocatable"`
	Usage                              corev1.ResourceList         `json:"usage,omitempty"`
	PodCount                           *int                        `json:"podCount,omitempty"`
	MaxPodCount                        int64                       `json:"maxPodCount,omitempty"`
	RunningPodCount                    *int                        `json:"runningPodCount,omitempty"`
	PendingPodCount                    *int                        `json:"pendingPodCount,omitempty"`
	FailedPodCount                     *int                        `json:"failedPodCount,omitempty"`
	TerminatedPodCount                 *int                        `json:"terminatedPodCount,omitempty"`
	EvictedPodCount                    *int                        `json:"evictedPodCount,omitempty"`
	RestartPodCount                    *int                        `json:"restartPodCount,omitempty"`
	OOMKilledPodCount                  *int                        `json:"oomKilledPodCount,omitempty"`
	ProblemPods                        []kubernetesObjectReference `json:"problemPods"`
	ImpactedPodCount                   *int                        `json:"impactedPodCount,omitempty"`
	ImpactedServiceCount               *int                        `json:"impactedServiceCount,omitempty"`
	SingleReplicaWorkloadCount         *int                        `json:"singleReplicaWorkloadCount,omitempty"`
	LocalStorageDependentWorkloadCount *int                        `json:"localStorageDependentWorkloadCount,omitempty"`
	AssignedWorkloads                  []kubernetesObjectReference `json:"assignedWorkloads,omitempty"`
	RelationshipConfidence             string                      `json:"relationshipConfidence"`
}

type kubernetesNamespaceDetail struct {
	ClusterID                       string            `json:"clusterId"`
	UID                             string            `json:"uid"`
	Name                            string            `json:"name"`
	Phase                           string            `json:"phase"`
	CreatedAt                       time.Time         `json:"createdAt"`
	Labels                          map[string]string `json:"labels,omitempty"`
	Terminating                     bool              `json:"terminating"`
	PodCount                        *int              `json:"podCount,omitempty"`
	ServiceCount                    *int              `json:"serviceCount,omitempty"`
	RunningPodCount                 *int              `json:"runningPodCount,omitempty"`
	PendingPodCount                 *int              `json:"pendingPodCount,omitempty"`
	FailedPodCount                  *int              `json:"failedPodCount,omitempty"`
	CrashLoopPodCount               *int              `json:"crashLoopPodCount,omitempty"`
	OOMKilledPodCount               *int              `json:"oomKilledPodCount,omitempty"`
	EndpointUnavailableServiceCount *int              `json:"endpointUnavailableServiceCount,omitempty"`
	CPURequests                     string            `json:"cpuRequests"`
	CPULimits                       string            `json:"cpuLimits"`
	MemoryRequests                  string            `json:"memoryRequests"`
	MemoryLimits                    string            `json:"memoryLimits"`
}

type kubernetesContainerDetail struct {
	Name                     string                 `json:"name"`
	Type                     string                 `json:"type"`
	Image                    string                 `json:"image,omitempty"`
	ImageID                  string                 `json:"imageId,omitempty"`
	ContainerID              string                 `json:"containerId,omitempty"`
	Ready                    bool                   `json:"ready"`
	Started                  *bool                  `json:"started,omitempty"`
	RestartCount             int32                  `json:"restartCount"`
	State                    string                 `json:"state"`
	WaitingReason            string                 `json:"waitingReason,omitempty"`
	TerminatedReason         string                 `json:"terminatedReason,omitempty"`
	ExitCode                 *int32                 `json:"exitCode,omitempty"`
	StartedAt                *time.Time             `json:"startedAt,omitempty"`
	FinishedAt               *time.Time             `json:"finishedAt,omitempty"`
	LastTerminatedReason     string                 `json:"lastTerminatedReason,omitempty"`
	LastExitCode             *int32                 `json:"lastExitCode,omitempty"`
	CPURequest               string                 `json:"cpuRequest,omitempty"`
	CPULimit                 string                 `json:"cpuLimit,omitempty"`
	MemoryRequest            string                 `json:"memoryRequest,omitempty"`
	MemoryLimit              string                 `json:"memoryLimit,omitempty"`
	Ports                    []corev1.ContainerPort `json:"ports,omitempty"`
	VolumeMounts             []corev1.VolumeMount   `json:"volumeMounts,omitempty"`
	LivenessProbeConfigured  bool                   `json:"livenessProbeConfigured"`
	ReadinessProbeConfigured bool                   `json:"readinessProbeConfigured"`
	StartupProbeConfigured   bool                   `json:"startupProbeConfigured"`
}

type kubernetesPodDetail struct {
	ClusterID              string                      `json:"clusterId"`
	UID                    string                      `json:"uid"`
	Name                   string                      `json:"name"`
	Namespace              string                      `json:"namespace"`
	Phase                  string                      `json:"phase"`
	PodIP                  string                      `json:"podIp,omitempty"`
	HostIP                 string                      `json:"hostIp,omitempty"`
	NodeName               string                      `json:"nodeName,omitempty"`
	QOSClass               string                      `json:"qosClass,omitempty"`
	CreatedAt              time.Time                   `json:"createdAt"`
	StartTime              *time.Time                  `json:"startTime,omitempty"`
	OwnerKind              string                      `json:"ownerKind,omitempty"`
	OwnerName              string                      `json:"ownerName,omitempty"`
	OwnerUID               string                      `json:"ownerUid,omitempty"`
	Conditions             []kubernetesConditionDetail `json:"conditions"`
	RestartCount           int32                       `json:"restartCount"`
	Volumes                []string                    `json:"volumes,omitempty"`
	PVCReferences          []string                    `json:"pvcReferences,omitempty"`
	Containers             []kubernetesContainerDetail `json:"containers"`
	SelectedByServices     []kubernetesObjectReference `json:"selectedByServices"`
	EndpointSlices         []kubernetesObjectReference `json:"endpointSlices"`
	Node                   *kubernetesObjectReference  `json:"node,omitempty"`
	RelationshipConfidence string                      `json:"relationshipConfidence"`
}

type kubernetesEndpointDetail struct {
	Address     string `json:"address"`
	PodUID      string `json:"podUid,omitempty"`
	PodName     string `json:"podName,omitempty"`
	NodeName    string `json:"nodeName,omitempty"`
	Ready       *bool  `json:"ready,omitempty"`
	Serving     *bool  `json:"serving,omitempty"`
	Terminating *bool  `json:"terminating,omitempty"`
	Zone        string `json:"zone,omitempty"`
}

type kubernetesServiceDetail struct {
	ClusterID                   string                       `json:"clusterId"`
	UID                         string                       `json:"uid"`
	Name                        string                       `json:"name"`
	Namespace                   string                       `json:"namespace"`
	Type                        string                       `json:"type"`
	ClusterIPs                  []string                     `json:"clusterIps,omitempty"`
	ExternalIPs                 []string                     `json:"externalIps,omitempty"`
	LoadBalancerIngress         []corev1.LoadBalancerIngress `json:"loadBalancerIngress,omitempty"`
	Selector                    map[string]string            `json:"selector,omitempty"`
	SessionAffinity             string                       `json:"sessionAffinity,omitempty"`
	ExternalTrafficPolicy       string                       `json:"externalTrafficPolicy,omitempty"`
	Ports                       []corev1.ServicePort         `json:"ports,omitempty"`
	EndpointCount               int                          `json:"endpointCount"`
	ReadyEndpointCount          int                          `json:"readyEndpointCount"`
	NotReadyEndpointCount       int                          `json:"notReadyEndpointCount"`
	ServingEndpointCount        int                          `json:"servingEndpointCount"`
	TerminatingEndpointCount    int                          `json:"terminatingEndpointCount"`
	Endpoints                   []kubernetesEndpointDetail   `json:"endpoints"`
	SelectedPods                []kubernetesObjectReference  `json:"selectedPods"`
	CreatedAt                   time.Time                    `json:"createdAt"`
	NoReadyEndpoints            bool                         `json:"noReadyEndpoints"`
	SingleEndpoint              bool                         `json:"singleEndpoint"`
	SingleNodeConcentration     bool                         `json:"singleNodeConcentration"`
	SelectorWithoutMatchingPods bool                         `json:"selectorWithoutMatchingPods"`
	EndpointDataAvailable       bool                         `json:"endpointDataAvailable"`
	RelationshipSource          string                       `json:"relationshipSource"`
}

func conditionTime(value metav1.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	t := value.Time
	return &t
}

func nodeConditions(node *corev1.Node) []kubernetesConditionDetail {
	result := make([]kubernetesConditionDetail, 0, len(node.Status.Conditions))
	for _, condition := range node.Status.Conditions {
		transition := conditionTime(condition.LastTransitionTime)
		duration := int64(0)
		if transition != nil {
			duration = int64(time.Since(*transition).Seconds())
			if duration < 0 {
				duration = 0
			}
		}
		result = append(result, kubernetesConditionDetail{Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: condition.Message, LastHeartbeatTime: conditionTime(condition.LastHeartbeatTime), LastTransitionTime: transition, DurationSeconds: duration})
	}
	return result
}

func podConditions(pod *corev1.Pod) []kubernetesConditionDetail {
	result := make([]kubernetesConditionDetail, 0, len(pod.Status.Conditions))
	for _, condition := range pod.Status.Conditions {
		transition := conditionTime(condition.LastTransitionTime)
		duration := int64(0)
		if transition != nil {
			duration = int64(time.Since(*transition).Seconds())
			if duration < 0 {
				duration = 0
			}
		}
		result = append(result, kubernetesConditionDetail{Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason, Message: condition.Message, LastTransitionTime: transition, DurationSeconds: duration})
	}
	return result
}

func kubernetesNodeRoles(labels map[string]string) []string {
	roles := make([]string, 0)
	for key := range labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
			if role != "" {
				roles = append(roles, role)
			}
		}
	}
	if len(roles) == 0 {
		roles = append(roles, "worker")
	}
	sort.Strings(roles)
	return roles
}

func handleMoldKubernetesNodeDetail(w http.ResponseWriter, r *http.Request) {
	cluster, client, ctx, cancel, ok := kubernetesDetailRequest(w, r)
	if !ok {
		return
	}
	defer cancel()
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeKubernetesDetailError(w, cluster.ID, err)
		return
	}
	var node *corev1.Node
	for i := range nodes.Items {
		if string(nodes.Items[i].UID) == uid {
			node = &nodes.Items[i]
			break
		}
	}
	if node == nil {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	pods, podErr := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + node.Name})
	if podErr != nil && !apierrors.IsForbidden(podErr) {
		writeKubernetesDetailError(w, cluster.ID, podErr)
		return
	}
	detail := kubernetesNodeDetail{ClusterID: cluster.ID, UID: string(node.UID), Name: node.Name, Roles: kubernetesNodeRoles(node.Labels), PodCIDRs: append([]string(nil), node.Spec.PodCIDRs...), KubernetesVersion: node.Status.NodeInfo.KubeletVersion, OSImage: node.Status.NodeInfo.OSImage, KernelVersion: node.Status.NodeInfo.KernelVersion, Architecture: node.Status.NodeInfo.Architecture, ContainerRuntime: node.Status.NodeInfo.ContainerRuntimeVersion, CreatedAt: node.CreationTimestamp.Time, Conditions: nodeConditions(node), Unschedulable: node.Spec.Unschedulable, Taints: append([]corev1.Taint(nil), node.Spec.Taints...), Labels: node.Labels, Capacity: node.Status.Capacity, Allocatable: node.Status.Allocatable, MaxPodCount: node.Status.Allocatable.Pods().Value(), ProblemPods: make([]kubernetesObjectReference, 0), RelationshipConfidence: "UNKNOWN"}
	var nodeMetrics struct {
		Usage corev1.ResourceList `json:"usage"`
	}
	if rawMetrics, metricsErr := client.Discovery().RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/nodes", node.Name).
		Do(ctx).
		Raw(); metricsErr == nil {
		if jsonErr := json.Unmarshal(rawMetrics, &nodeMetrics); jsonErr == nil {
			detail.Usage = nodeMetrics.Usage
		}
	}
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			detail.InternalIP = address.Address
			break
		}
	}
	if podErr == nil {
		podAggregate := aggregateKubernetesPods(pods.Items)
		podCount, running, pending, failed := len(podAggregate.ActivePods), podAggregate.Running, podAggregate.Pending, 0
		for i := range podAggregate.ProblemPods {
			pod := &podAggregate.ProblemPods[i]
			detail.ProblemPods = append(detail.ProblemPods, kubernetesObjectReference{UID: string(pod.UID), Kind: "Pod", Name: pod.Name, Namespace: pod.Namespace})
		}
		detail.PodCount, detail.RunningPodCount, detail.PendingPodCount, detail.FailedPodCount = &podCount, &running, &pending, &failed
		terminated, evicted := len(podAggregate.TerminatedPods), len(podAggregate.EvictedPods)
		detail.TerminatedPodCount, detail.EvictedPodCount = &terminated, &evicted
		detail.RestartPodCount, detail.OOMKilledPodCount = &podAggregate.Restarted, &podAggregate.OOMKilled
		impactedPodCount := len(podAggregate.ProblemPods)
		if !kubernetesNodeReady(node) {
			impactedPodCount = len(podAggregate.ActivePods)
		}
		detail.ImpactedPodCount = &impactedPodCount
		if allPods, allPodsErr := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{}); allPodsErr == nil {
			if services, serviceErr := client.CoreV1().Services("").List(ctx, metav1.ListOptions{}); serviceErr == nil {
				if slices, sliceErr := client.DiscoveryV1().EndpointSlices("").List(ctx, metav1.ListOptions{}); sliceErr == nil {
					notReadyNodes := map[string]bool{}
					if !kubernetesNodeReady(node) {
						notReadyNodes[node.Name] = true
					}
					globalAggregate := aggregateKubernetesPods(allPods.Items)
					impactByService := aggregateKubernetesServiceImpact(services.Items, slices.Items, globalAggregate, notReadyNodes)
					nodePodUIDs := make(map[string]bool, len(podAggregate.ActivePods))
					for i := range podAggregate.ActivePods {
						nodePodUIDs[string(podAggregate.ActivePods[i].UID)] = true
					}
					impactedServices := make(map[string]bool)
					for _, slice := range slices.Items {
						key := slice.Namespace + "/" + slice.Labels[discoveryv1.LabelServiceName]
						if !impactByService[key].Affected {
							continue
						}
						for _, endpoint := range slice.Endpoints {
							if endpoint.TargetRef != nil && nodePodUIDs[string(endpoint.TargetRef.UID)] {
								impactedServices[key] = true
								break
							}
						}
					}
					for i := range services.Items {
						service := &services.Items[i]
						key := service.Namespace + "/" + service.Name
						if !impactByService[key].Affected || impactedServices[key] || len(service.Spec.Selector) == 0 {
							continue
						}
						for p := range podAggregate.ActivePods {
							pod := &podAggregate.ActivePods[p]
							if pod.Namespace == service.Namespace && labelsMatch(service.Spec.Selector, pod.Labels) {
								impactedServices[key] = true
								break
							}
						}
					}
					count := len(impactedServices)
					detail.ImpactedServiceCount = &count
				}
			}
		}
		populateKubernetesNodeWorkloadSummary(ctx, client, podAggregate.ActivePods, &detail)
	}
	setKubernetesCollectionState(cluster.ID, kubernetesHealthy, nil)
	writeJSON(w, detail)
}

func handleMoldKubernetesNamespaceDetail(w http.ResponseWriter, r *http.Request) {
	cluster, client, ctx, cancel, ok := kubernetesDetailRequest(w, r)
	if !ok {
		return
	}
	defer cancel()
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		writeKubernetesDetailError(w, cluster.ID, err)
		return
	}
	var ns *corev1.Namespace
	for i := range namespaces.Items {
		if string(namespaces.Items[i].UID) == uid {
			ns = &namespaces.Items[i]
			break
		}
	}
	if ns == nil {
		http.Error(w, "namespace not found", http.StatusNotFound)
		return
	}
	pods, podErr := client.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{})
	services, serviceErr := client.CoreV1().Services(ns.Name).List(ctx, metav1.ListOptions{})
	slices, sliceErr := client.DiscoveryV1().EndpointSlices(ns.Name).List(ctx, metav1.ListOptions{})
	detail := kubernetesNamespaceDetail{ClusterID: cluster.ID, UID: string(ns.UID), Name: ns.Name, Phase: string(ns.Status.Phase), CreatedAt: ns.CreationTimestamp.Time, Labels: ns.Labels, Terminating: ns.DeletionTimestamp != nil}
	if podErr == nil {
		podAggregate := aggregateKubernetesPods(pods.Items)
		podCount, running, pending, failed, crashLoop, oomKilled := len(podAggregate.ActivePods), podAggregate.Running, podAggregate.Pending, 0, 0, podAggregate.OOMKilled
		var reqCPU, limCPU, reqMem, limMem int64
		for i := range podAggregate.ActivePods {
			pod := &podAggregate.ActivePods[i]
			if classifyKubernetesPod(pod).CrashLoop {
				crashLoop++
			}
			for _, c := range pod.Spec.Containers {
				reqCPU += c.Resources.Requests.Cpu().MilliValue()
				limCPU += c.Resources.Limits.Cpu().MilliValue()
				reqMem += c.Resources.Requests.Memory().Value()
				limMem += c.Resources.Limits.Memory().Value()
			}
		}
		detail.PodCount, detail.RunningPodCount, detail.PendingPodCount, detail.FailedPodCount = &podCount, &running, &pending, &failed
		detail.CrashLoopPodCount, detail.OOMKilledPodCount = &crashLoop, &oomKilled
		detail.CPURequests = fmt.Sprintf("%dm", reqCPU)
		detail.CPULimits = fmt.Sprintf("%dm", limCPU)
		detail.MemoryRequests = fmt.Sprintf("%d", reqMem)
		detail.MemoryLimits = fmt.Sprintf("%d", limMem)
	}
	if serviceErr == nil {
		serviceCount := len(services.Items)
		detail.ServiceCount = &serviceCount
	}
	if sliceErr == nil && serviceErr == nil {
		unavailable := 0
		readyByService := make(map[string]int)
		for _, slice := range slices.Items {
			name := slice.Labels[discoveryv1.LabelServiceName]
			for _, endpoint := range slice.Endpoints {
				if endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready {
					readyByService[name]++
				}
			}
		}
		for _, service := range services.Items {
			if service.Spec.ClusterIP != corev1.ClusterIPNone && readyByService[service.Name] == 0 {
				unavailable++
			}
		}
		detail.EndpointUnavailableServiceCount = &unavailable
	}
	setKubernetesCollectionState(cluster.ID, kubernetesHealthy, nil)
	writeJSON(w, detail)
}

func handleMoldKubernetesPodDetail(w http.ResponseWriter, r *http.Request) {
	cluster, client, ctx, cancel, ok := kubernetesDetailRequest(w, r)
	if !ok {
		return
	}
	defer cancel()
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		writeKubernetesDetailError(w, cluster.ID, err)
		return
	}
	var pod *corev1.Pod
	for i := range pods.Items {
		if string(pods.Items[i].UID) == uid {
			pod = &pods.Items[i]
			break
		}
	}
	if pod == nil {
		http.Error(w, "pod not found", http.StatusNotFound)
		return
	}
	detail := buildKubernetesPodDetail(cluster.ID, pod)
	services, serviceErr := client.CoreV1().Services(pod.Namespace).List(ctx, metav1.ListOptions{})
	slices, sliceErr := client.DiscoveryV1().EndpointSlices(pod.Namespace).List(ctx, metav1.ListOptions{})
	endpointServiceNames := make(map[string]bool)
	servicesWithSlices := make(map[string]bool)
	if sliceErr == nil {
		for _, slice := range slices.Items {
			servicesWithSlices[slice.Labels[discoveryv1.LabelServiceName]] = true
			matched := false
			for _, endpoint := range slice.Endpoints {
				if endpoint.TargetRef != nil && endpoint.TargetRef.UID == pod.UID {
					matched = true
					break
				}
			}
			if matched {
				serviceName := slice.Labels[discoveryv1.LabelServiceName]
				detail.EndpointSlices = append(detail.EndpointSlices, kubernetesObjectReference{UID: string(slice.UID), Kind: "EndpointSlice", Name: slice.Name, Namespace: slice.Namespace})
				if serviceName != "" {
					endpointServiceNames[serviceName] = true
				}
			}
		}
	}
	if serviceErr == nil {
		for i := range services.Items {
			service := &services.Items[i]
			selected := endpointServiceNames[service.Name]
			if !selected && (sliceErr != nil || !servicesWithSlices[service.Name]) && len(service.Spec.Selector) > 0 {
				selected = labelsMatch(service.Spec.Selector, pod.Labels)
			}
			if selected {
				detail.SelectedByServices = append(detail.SelectedByServices, kubernetesObjectReference{UID: string(service.UID), Kind: "Service", Name: service.Name, Namespace: service.Namespace})
			}
		}
	}
	setKubernetesCollectionState(cluster.ID, kubernetesHealthy, nil)
	writeJSON(w, detail)
}

func handleMoldKubernetesServiceDetail(w http.ResponseWriter, r *http.Request) {
	cluster, client, ctx, cancel, ok := kubernetesDetailRequest(w, r)
	if !ok {
		return
	}
	defer cancel()
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	services, err := client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		writeKubernetesDetailError(w, cluster.ID, err)
		return
	}
	var service *corev1.Service
	for i := range services.Items {
		if string(services.Items[i].UID) == uid {
			service = &services.Items[i]
			break
		}
	}
	if service == nil {
		http.Error(w, "service not found", http.StatusNotFound)
		return
	}
	detail := kubernetesServiceDetail{ClusterID: cluster.ID, UID: string(service.UID), Name: service.Name, Namespace: service.Namespace, Type: string(service.Spec.Type), ClusterIPs: append([]string(nil), service.Spec.ClusterIPs...), ExternalIPs: append([]string(nil), service.Spec.ExternalIPs...), LoadBalancerIngress: append([]corev1.LoadBalancerIngress(nil), service.Status.LoadBalancer.Ingress...), Selector: service.Spec.Selector, SessionAffinity: string(service.Spec.SessionAffinity), ExternalTrafficPolicy: string(service.Spec.ExternalTrafficPolicy), Ports: append([]corev1.ServicePort(nil), service.Spec.Ports...), CreatedAt: service.CreationTimestamp.Time, Endpoints: make([]kubernetesEndpointDetail, 0), SelectedPods: make([]kubernetesObjectReference, 0)}
	slices, sliceErr := client.DiscoveryV1().EndpointSlices(service.Namespace).List(ctx, metav1.ListOptions{LabelSelector: discoveryv1.LabelServiceName + "=" + service.Name})
	nodes := make(map[string]bool)
	podUIDs := make(map[string]bool)
	if sliceErr == nil {
		detail.EndpointDataAvailable = true
		detail.RelationshipSource = "ENDPOINT_SLICE"
		for _, slice := range slices.Items {
			for _, endpoint := range slice.Endpoints {
				for _, address := range endpoint.Addresses {
					item := kubernetesEndpointDetail{Address: address, Ready: endpoint.Conditions.Ready, Serving: endpoint.Conditions.Serving, Terminating: endpoint.Conditions.Terminating}
					if endpoint.NodeName != nil {
						item.NodeName = *endpoint.NodeName
						nodes[item.NodeName] = true
					}
					if endpoint.Zone != nil {
						item.Zone = *endpoint.Zone
					}
					if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" {
						item.PodUID = string(endpoint.TargetRef.UID)
						item.PodName = endpoint.TargetRef.Name
						podUIDs[item.PodUID] = true
					}
					detail.Endpoints = append(detail.Endpoints, item)
					detail.EndpointCount++
					if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
						detail.ReadyEndpointCount++
					} else {
						detail.NotReadyEndpointCount++
					}
					if endpoint.Conditions.Serving != nil && *endpoint.Conditions.Serving {
						detail.ServingEndpointCount++
					}
					if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
						detail.TerminatingEndpointCount++
					}
				}
			}
		}
	}
	if len(podUIDs) > 0 {
		pods, podErr := client.CoreV1().Pods(service.Namespace).List(ctx, metav1.ListOptions{})
		if podErr == nil {
			for _, pod := range pods.Items {
				if podUIDs[string(pod.UID)] {
					detail.SelectedPods = append(detail.SelectedPods, kubernetesObjectReference{UID: string(pod.UID), Kind: "Pod", Name: pod.Name, Namespace: pod.Namespace})
				}
			}
		}
	} else if (sliceErr != nil || len(slices.Items) == 0) && len(service.Spec.Selector) > 0 {
		pods, podErr := client.CoreV1().Pods(service.Namespace).List(ctx, metav1.ListOptions{})
		if podErr == nil {
			detail.RelationshipSource = "SELECTOR"
			for _, pod := range pods.Items {
				if labelsMatch(service.Spec.Selector, pod.Labels) {
					detail.SelectedPods = append(detail.SelectedPods, kubernetesObjectReference{UID: string(pod.UID), Kind: "Pod", Name: pod.Name, Namespace: pod.Namespace})
					nodes[pod.Spec.NodeName] = true
				}
			}
		}
	}
	if detail.RelationshipSource == "" {
		detail.RelationshipSource = "UNKNOWN"
	}
	detail.NoReadyEndpoints = detail.EndpointDataAvailable && detail.ReadyEndpointCount == 0
	detail.SingleEndpoint = detail.EndpointDataAvailable && detail.ReadyEndpointCount == 1
	detail.SingleNodeConcentration = detail.EndpointDataAvailable && detail.ReadyEndpointCount > 1 && len(nodes) == 1
	detail.SelectorWithoutMatchingPods = len(service.Spec.Selector) > 0 && len(detail.SelectedPods) == 0
	setKubernetesCollectionState(cluster.ID, kubernetesHealthy, nil)
	writeJSON(w, detail)
}

func kubernetesDetailRequest(w http.ResponseWriter, r *http.Request) (moldKubernetesCluster, kubernetes.Interface, context.Context, context.CancelFunc, bool) {
	clusterID := strings.TrimSpace(r.URL.Query().Get("id"))
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	if clusterID == "" || uid == "" {
		http.Error(w, "cluster id and uid are required", http.StatusBadRequest)
		return moldKubernetesCluster{}, nil, nil, nil, false
	}
	cluster, err := resolveMoldKubernetesCluster(clusterID)
	if err != nil {
		writeMoldKubernetesError(w, err)
		return moldKubernetesCluster{}, nil, nil, nil, false
	}
	client, _, err := getMoldKubernetesClient(cluster)
	if err != nil {
		writeMoldKubernetesError(w, err)
		return moldKubernetesCluster{}, nil, nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	return cluster, client, ctx, cancel, true
}

func writeKubernetesDetailError(w http.ResponseWriter, clusterID string, err error) {
	status := classifyKubernetesConnectionError(err)
	setKubernetesCollectionState(clusterID, status, err)
	code := http.StatusBadGateway
	if status == kubernetesAuthenticationFailed {
		code = http.StatusUnauthorized
	}
	if status == kubernetesPermissionDenied {
		code = http.StatusForbidden
	}
	http.Error(w, "Kubernetes 상세 정보를 수집하지 못했습니다.", code)
}

// populateKubernetesNodeWorkloadSummary keeps the node panel's workload metrics
// on one owner-reference based source:
// Pod -> ReplicaSet -> Deployment, Pod -> StatefulSet/DaemonSet,
// and Pod -> Job -> CronJob. ReplicaSets never become visible workloads.
func populateKubernetesNodeWorkloadSummary(ctx context.Context, client kubernetes.Interface, pods []corev1.Pod, detail *kubernetesNodeDetail) {
	// Relationship and dependency counts are current-state metrics. Reapply the
	// canonical active-Pod rule at this domain boundary so terminated history
	// cannot enter the workload UID sets even if a future caller passes all Pods.
	pods = aggregateKubernetesPods(pods).ActivePods
	deployments, deploymentErr := client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	replicaSets, replicaSetErr := client.AppsV1().ReplicaSets("").List(ctx, metav1.ListOptions{})
	statefulSets, statefulSetErr := client.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	daemonSets, daemonSetErr := client.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	jobs, jobErr := client.BatchV1().Jobs("").List(ctx, metav1.ListOptions{})
	cronJobs, cronJobErr := client.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{})
	if deploymentErr != nil || replicaSetErr != nil || statefulSetErr != nil || daemonSetErr != nil || jobErr != nil || cronJobErr != nil {
		return
	}

	workloads := make(map[string]kubernetesObjectReference)
	singleReplicaUIDs := make(map[string]bool)
	replicaSetOwners := make(map[string]string)
	jobOwners := make(map[string]string)
	for i := range deployments.Items {
		item := &deployments.Items[i]
		uid := string(item.UID)
		workloads[uid] = kubernetesObjectReference{UID: uid, Kind: "Deployment", Name: item.Name, Namespace: item.Namespace}
		singleReplicaUIDs[uid] = item.Spec.Replicas != nil && *item.Spec.Replicas == 1
	}
	for i := range statefulSets.Items {
		item := &statefulSets.Items[i]
		uid := string(item.UID)
		workloads[uid] = kubernetesObjectReference{UID: uid, Kind: "StatefulSet", Name: item.Name, Namespace: item.Namespace}
		singleReplicaUIDs[uid] = item.Spec.Replicas != nil && *item.Spec.Replicas == 1
	}
	for i := range daemonSets.Items {
		item := &daemonSets.Items[i]
		uid := string(item.UID)
		workloads[uid] = kubernetesObjectReference{UID: uid, Kind: "DaemonSet", Name: item.Name, Namespace: item.Namespace}
	}
	for i := range jobs.Items {
		item := &jobs.Items[i]
		uid := string(item.UID)
		workloads[uid] = kubernetesObjectReference{UID: uid, Kind: "Job", Name: item.Name, Namespace: item.Namespace}
		if owner := kubernetesControllerOwner(item.OwnerReferences); owner != nil && strings.EqualFold(owner.Kind, "CronJob") {
			jobOwners[uid] = string(owner.UID)
		}
	}
	for i := range cronJobs.Items {
		item := &cronJobs.Items[i]
		uid := string(item.UID)
		workloads[uid] = kubernetesObjectReference{UID: uid, Kind: "CronJob", Name: item.Name, Namespace: item.Namespace}
	}
	for i := range replicaSets.Items {
		item := &replicaSets.Items[i]
		if owner := kubernetesControllerOwner(item.OwnerReferences); owner != nil && strings.EqualFold(owner.Kind, "Deployment") {
			replicaSetOwners[string(item.UID)] = string(owner.UID)
		}
	}

	claimsByKey := make(map[string]corev1.PersistentVolumeClaim)
	if claims, err := client.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range claims.Items {
			claim := claims.Items[i]
			claimsByKey[claim.Namespace+"/"+claim.Name] = claim
		}
	}
	volumesByName := make(map[string]corev1.PersistentVolume)
	if volumes, err := client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{}); err == nil {
		for i := range volumes.Items {
			volumesByName[volumes.Items[i].Name] = volumes.Items[i]
		}
	}
	localStorageClasses := make(map[string]bool)
	if classes, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{}); err == nil {
		for i := range classes.Items {
			class := classes.Items[i]
			localStorageClasses[class.Name] = strings.Contains(strings.ToLower(class.Name), "local-path") ||
				strings.Contains(strings.ToLower(class.Provisioner), "local-path")
		}
	}

	assigned := make(map[string]kubernetesObjectReference)
	singleReplica := make(map[string]bool)
	localStorage := make(map[string]bool)
	for i := range pods {
		pod := &pods[i]
		owner := kubernetesControllerOwner(pod.OwnerReferences)
		if owner == nil {
			continue
		}
		workloadUID := string(owner.UID)
		switch strings.ToLower(owner.Kind) {
		case "replicaset":
			workloadUID = replicaSetOwners[workloadUID]
		case "job":
			if cronJobUID := jobOwners[workloadUID]; cronJobUID != "" {
				workloadUID = cronJobUID
			}
		}
		workload, found := workloads[workloadUID]
		if !found {
			continue
		}
		assigned[workloadUID] = workload
		if singleReplicaUIDs[workloadUID] {
			singleReplica[workloadUID] = true
		}
		if kubernetesPodUsesNodeLocalStorage(pod, claimsByKey, volumesByName, localStorageClasses) {
			localStorage[workloadUID] = true
		}
	}

	detail.AssignedWorkloads = make([]kubernetesObjectReference, 0, len(assigned))
	for _, workload := range assigned {
		detail.AssignedWorkloads = append(detail.AssignedWorkloads, workload)
	}
	sort.Slice(detail.AssignedWorkloads, func(i, j int) bool {
		if detail.AssignedWorkloads[i].Kind == detail.AssignedWorkloads[j].Kind {
			return detail.AssignedWorkloads[i].Name < detail.AssignedWorkloads[j].Name
		}
		return detail.AssignedWorkloads[i].Kind < detail.AssignedWorkloads[j].Kind
	})
	singleReplicaCount, localStorageCount := len(singleReplica), len(localStorage)
	detail.SingleReplicaWorkloadCount = &singleReplicaCount
	detail.LocalStorageDependentWorkloadCount = &localStorageCount
}

func kubernetesControllerOwner(owners []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range owners {
		if owners[i].Controller != nil && *owners[i].Controller {
			return &owners[i]
		}
	}
	if len(owners) > 0 {
		return &owners[0]
	}
	return nil
}

func kubernetesPodUsesNodeLocalStorage(
	pod *corev1.Pod,
	claims map[string]corev1.PersistentVolumeClaim,
	volumes map[string]corev1.PersistentVolume,
	localStorageClasses map[string]bool,
) bool {
	for _, podVolume := range pod.Spec.Volumes {
		if podVolume.HostPath != nil {
			return true
		}
		if podVolume.PersistentVolumeClaim == nil {
			continue
		}
		claim, found := claims[pod.Namespace+"/"+podVolume.PersistentVolumeClaim.ClaimName]
		if !found {
			continue
		}
		if claim.Spec.StorageClassName != nil && localStorageClasses[*claim.Spec.StorageClassName] {
			return true
		}
		if volume, found := volumes[claim.Spec.VolumeName]; found {
			if volume.Spec.Local != nil || volume.Spec.HostPath != nil || localStorageClasses[volume.Spec.StorageClassName] {
				return true
			}
		}
	}
	return false
}

func podProblemState(pod *corev1.Pod) (problem, restarted, oom bool) {
	classification := classifyKubernetesPod(pod)
	return classification.Problem, classification.Restarted, classification.OOMKilled
}

func buildKubernetesPodDetail(clusterID string, pod *corev1.Pod) kubernetesPodDetail {
	detail := kubernetesPodDetail{ClusterID: clusterID, UID: string(pod.UID), Name: pod.Name, Namespace: pod.Namespace, Phase: string(pod.Status.Phase), PodIP: pod.Status.PodIP, HostIP: pod.Status.HostIP, NodeName: pod.Spec.NodeName, QOSClass: string(pod.Status.QOSClass), CreatedAt: pod.CreationTimestamp.Time, Conditions: podConditions(pod), Containers: make([]kubernetesContainerDetail, 0), SelectedByServices: make([]kubernetesObjectReference, 0), EndpointSlices: make([]kubernetesObjectReference, 0), RelationshipConfidence: "UNKNOWN"}
	if pod.Status.StartTime != nil {
		t := pod.Status.StartTime.Time
		detail.StartTime = &t
	}
	if pod.Spec.NodeName != "" {
		detail.Node = &kubernetesObjectReference{Kind: "Node", Name: pod.Spec.NodeName}
	}
	if len(pod.OwnerReferences) > 0 {
		owner := pod.OwnerReferences[0]
		detail.OwnerKind = owner.Kind
		detail.OwnerName = owner.Name
		detail.OwnerUID = string(owner.UID)
	}
	for _, volume := range pod.Spec.Volumes {
		detail.Volumes = append(detail.Volumes, volume.Name)
		if volume.PersistentVolumeClaim != nil {
			detail.PVCReferences = append(detail.PVCReferences, volume.PersistentVolumeClaim.ClaimName)
		}
	}
	specByName := make(map[string]corev1.Container)
	for _, c := range pod.Spec.InitContainers {
		specByName[c.Name] = c
	}
	for _, c := range pod.Spec.Containers {
		specByName[c.Name] = c
	}
	for _, status := range pod.Status.InitContainerStatuses {
		detail.Containers = append(detail.Containers, buildContainerDetail("INIT", status, specByName[status.Name]))
		detail.RestartCount += status.RestartCount
	}
	for _, status := range pod.Status.ContainerStatuses {
		detail.Containers = append(detail.Containers, buildContainerDetail("APPLICATION", status, specByName[status.Name]))
		detail.RestartCount += status.RestartCount
	}
	for _, status := range pod.Status.EphemeralContainerStatuses {
		detail.Containers = append(detail.Containers, buildContainerDetail("EPHEMERAL", status, corev1.Container{}))
		detail.RestartCount += status.RestartCount
	}
	return detail
}

func buildContainerDetail(containerType string, status corev1.ContainerStatus, spec corev1.Container) kubernetesContainerDetail {
	detail := kubernetesContainerDetail{Name: status.Name, Type: containerType, Image: status.Image, ImageID: status.ImageID, ContainerID: status.ContainerID, Ready: status.Ready, Started: status.Started, RestartCount: status.RestartCount, Ports: append([]corev1.ContainerPort(nil), spec.Ports...), VolumeMounts: append([]corev1.VolumeMount(nil), spec.VolumeMounts...), LivenessProbeConfigured: spec.LivenessProbe != nil, ReadinessProbeConfigured: spec.ReadinessProbe != nil, StartupProbeConfigured: spec.StartupProbe != nil}
	if cpu := spec.Resources.Requests.Cpu(); !cpu.IsZero() {
		detail.CPURequest = cpu.String()
	}
	if cpu := spec.Resources.Limits.Cpu(); !cpu.IsZero() {
		detail.CPULimit = cpu.String()
	}
	if memory := spec.Resources.Requests.Memory(); !memory.IsZero() {
		detail.MemoryRequest = memory.String()
	}
	if memory := spec.Resources.Limits.Memory(); !memory.IsZero() {
		detail.MemoryLimit = memory.String()
	}
	if status.State.Running != nil {
		detail.State = "RUNNING"
		t := status.State.Running.StartedAt.Time
		detail.StartedAt = &t
	} else if status.State.Waiting != nil {
		detail.State = "WAITING"
		detail.WaitingReason = status.State.Waiting.Reason
	} else if status.State.Terminated != nil {
		detail.State = "TERMINATED"
		detail.TerminatedReason = status.State.Terminated.Reason
		code := status.State.Terminated.ExitCode
		detail.ExitCode = &code
		started, finished := status.State.Terminated.StartedAt.Time, status.State.Terminated.FinishedAt.Time
		detail.StartedAt, detail.FinishedAt = &started, &finished
	}
	if status.LastTerminationState.Terminated != nil {
		detail.LastTerminatedReason = status.LastTerminationState.Terminated.Reason
		code := status.LastTerminationState.Terminated.ExitCode
		detail.LastExitCode = &code
	}
	return detail
}
