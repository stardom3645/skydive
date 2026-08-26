package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/skydive-project/skydive/common"
	"github.com/skydive-project/skydive/config"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	moldKubernetesListCommand   = "listKubernetesClusters"
	moldKubernetesConfigCommand = "getKubernetesClusterConfig"
)

type moldKubernetesCluster struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	State             string `json:"state"`
	APIServer         string `json:"apiServer"`
	Description       string `json:"description,omitempty"`
	ZoneName          string `json:"zoneName,omitempty"`
	Version           string `json:"version,omitempty"`
	ControlNodes      int64  `json:"controlNodes,omitempty"`
	WorkerNodes       int64  `json:"workerNodes,omitempty"`
	Cores             int64  `json:"cores,omitempty"`
	MemoryMB          int64  `json:"memoryMb,omitempty"`
	NetworkName       string `json:"networkName,omitempty"`
	ServiceOffering   string `json:"serviceOffering,omitempty"`
	IPAddress         string `json:"ipAddress,omitempty"`
	Autoscaling       bool   `json:"autoscaling,omitempty"`
	MinSize           int64  `json:"minSize,omitempty"`
	MaxSize           int64  `json:"maxSize,omitempty"`
	Created           string `json:"created,omitempty"`
	CollectionEnabled bool   `json:"collectionEnabled"`
	CollectionRunning bool   `json:"collectionRunning"`
}

type kubernetesStatusCount struct {
	Total    int `json:"total"`
	Ready    int `json:"ready,omitempty"`
	NotReady int `json:"notReady,omitempty"`
	Running  int `json:"running,omitempty"`
	Pending  int `json:"pending,omitempty"`
	Problem  int `json:"problem,omitempty"`
	Failed   int `json:"failed,omitempty"`
	Unknown  int `json:"unknown,omitempty"`
}

type kubernetesResourceSummary struct {
	CapacityCPUCores       float64                      `json:"capacityCpuCores,omitempty"`
	AllocatableCPUCores    float64                      `json:"allocatableCpuCores,omitempty"`
	CapacityMemoryBytes    int64                        `json:"capacityMemoryBytes,omitempty"`
	AllocatableMemoryBytes int64                        `json:"allocatableMemoryBytes,omitempty"`
	CapacityPods           int64                        `json:"capacityPods,omitempty"`
	AllocatablePods        int64                        `json:"allocatablePods,omitempty"`
	RequestsCPUCores       float64                      `json:"requestsCpuCores,omitempty"`
	LimitsCPUCores         float64                      `json:"limitsCpuCores,omitempty"`
	RequestsMemoryBytes    int64                        `json:"requestsMemoryBytes,omitempty"`
	LimitsMemoryBytes      int64                        `json:"limitsMemoryBytes,omitempty"`
	UsageCPUCores          float64                      `json:"usageCpuCores,omitempty"`
	UsageMemoryBytes       int64                        `json:"usageMemoryBytes,omitempty"`
	CPUUsagePercent        float64                      `json:"cpuUsagePercent,omitempty"`
	MemoryUsagePercent     float64                      `json:"memoryUsagePercent,omitempty"`
	MetricsAvailable       bool                         `json:"metricsAvailable"`
	PodUsage               []kubernetesPodResourceUsage `json:"podUsage,omitempty"`
}

type kubernetesPodResourceUsage struct {
	Namespace        string  `json:"namespace"`
	Name             string  `json:"name"`
	NodeName         string  `json:"nodeName,omitempty"`
	UsageCPUCores    float64 `json:"usageCpuCores,omitempty"`
	UsageMemoryBytes int64   `json:"usageMemoryBytes,omitempty"`
}

type kubernetesRiskItem struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Count    int    `json:"count,omitempty"`
}

type kubernetesRecentChange struct {
	Time     time.Time `json:"time"`
	Resource string    `json:"resource"`
	Message  string    `json:"message"`
	Severity string    `json:"severity"`
}

type moldKubernetesClusterSummary struct {
	Cluster                            moldKubernetesCluster         `json:"cluster"`
	ClusterID                          string                        `json:"clusterId"`
	ClusterUID                         string                        `json:"clusterUid,omitempty"`
	APIServer                          string                        `json:"apiServer,omitempty"`
	Version                            string                        `json:"version,omitempty"`
	KubernetesVersion                  string                        `json:"kubernetesVersion,omitempty"`
	APIConnectionStatus                kubernetesAPIConnectionStatus `json:"apiConnectionStatus"`
	LastSyncAt                         *time.Time                    `json:"lastSyncAt,omitempty"`
	ControlPlane                       kubernetesStatusCount         `json:"controlPlane"`
	ControlPlaneReady                  int                           `json:"controlPlaneReady"`
	ControlPlaneTotal                  int                           `json:"controlPlaneTotal"`
	Nodes                              kubernetesStatusCount         `json:"nodes"`
	NodeReady                          int                           `json:"nodeReady"`
	NodeTotal                          int                           `json:"nodeTotal"`
	Namespaces                         int                           `json:"namespaces"`
	NamespaceCount                     int                           `json:"namespaceCount"`
	Pods                               kubernetesStatusCount         `json:"pods"`
	PodRunning                         int                           `json:"podRunning"`
	PodPending                         int                           `json:"podPending"`
	PodProblem                         int                           `json:"podProblem"`
	PodFailed                          int                           `json:"podFailed"`
	Services                           int                           `json:"services"`
	ServiceCount                       int                           `json:"serviceCount"`
	Resources                          kubernetesResourceSummary     `json:"resources"`
	Risks                              []kubernetesRiskItem          `json:"risks"`
	RecentChanges                      []kubernetesRecentChange      `json:"recentChanges"`
	AffectedServices                   int                           `json:"affectedServices"`
	CurrentlyImpactedServiceCount      int                           `json:"currentlyImpactedServiceCount"`
	ExternalPathCount                  int                           `json:"externalPathCount"`
	ExternalPathDetails                []string                      `json:"externalPathDetails,omitempty"`
	ImpactScore                        int                           `json:"impactScore"`
	CurrentImpactScore                 int                           `json:"currentImpactScore"`
	InfrastructureRiskScore            int                           `json:"infrastructureRiskScore"`
	SingleControlPlane                 bool                          `json:"singleControlPlane"`
	SingleReplicaWorkloadCount         int                           `json:"singleReplicaWorkloadCount"`
	SingleNodeEndpointServiceCount     int                           `json:"singleNodeEndpointServiceCount"`
	LocalStorageDependentWorkloadCount int                           `json:"localStorageDependentWorkloadCount"`
	CollectedAt                        time.Time                     `json:"collectedAt"`
}

type kubernetesNodeMetricsList struct {
	Items []struct {
		Usage corev1.ResourceList `json:"usage"`
	} `json:"items"`
}

type kubernetesPodMetricsList struct {
	Items []struct {
		Metadata struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"metadata"`
		Containers []struct {
			Usage corev1.ResourceList `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

type moldKubernetesSelection struct {
	Enabled      bool      `json:"enabled"`
	ClusterID    string    `json:"clusterId,omitempty"`
	ClusterName  string    `json:"clusterName,omitempty"`
	APIServer    string    `json:"apiServer,omitempty"`
	ClusterIDs   []string  `json:"clusterIds,omitempty"`
	ClusterNames []string  `json:"clusterNames,omitempty"`
	APIServers   []string  `json:"apiServers,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

type moldKubernetesClustersResponse struct {
	Clusters       []moldKubernetesCluster `json:"clusters"`
	SelectedID     string                  `json:"selectedId,omitempty"`
	SelectedIDs    []string                `json:"selectedIds,omitempty"`
	KubeconfigPath string                  `json:"kubeconfigPath"`
	RestartNeeded  bool                    `json:"restartNeeded"`
	ProbeRunning   bool                    `json:"probeRunning"`
}

type moldKubernetesSelectRequest struct {
	ID  string   `json:"id"`
	IDs []string `json:"ids"`
}

type moldKubernetesTestRequest struct {
	ID string `json:"id"`
}

type moldKubernetesCheckResult struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	OK      bool   `json:"ok"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type moldKubernetesTestResponse struct {
	OK      bool                        `json:"ok"`
	Message string                      `json:"message,omitempty"`
	Checks  []moldKubernetesCheckResult `json:"checks"`
}

type moldKubernetesStatusResponse struct {
	OK             bool   `json:"ok"`
	Message        string `json:"message,omitempty"`
	KubeconfigPath string `json:"kubeconfigPath,omitempty"`
	ProbeRunning   bool   `json:"probeRunning"`
}

func handleMoldKubernetesClusters(w http.ResponseWriter, r *http.Request, isProbeRunning moldKubernetesProbeStatus, stopProbe moldKubernetesProbeStopper) {
	clusters, err := listMoldKubernetesClusters()
	if err != nil {
		writeMoldKubernetesError(w, err)
		return
	}

	selection, _ := readMoldKubernetesSelection()
	selectedIDs := normalizedSelectionClusterIDs(selection)
	selectedID := ""
	if selection.Enabled && isMoldKubernetesSelectionCurrent(selection, clusters) {
		if len(selectedIDs) > 0 {
			selectedID = selectedIDs[0]
		}
	} else if selection.Enabled {
		staleSelection := selection
		selection = moldKubernetesSelection{Enabled: false, UpdatedAt: time.Now().UTC()}
		_ = writeMoldKubernetesSelection(selection)
		_ = removeMoldKubernetesManagedKubeconfigs(staleSelection)
		if stopProbe != nil {
			go stopProbe()
		}
		selectedIDs = nil
	}
	probeRunning := false
	if isProbeRunning != nil {
		probeRunning = isProbeRunning()
	}
	for i := range clusters {
		clusters[i].CollectionEnabled = selection.Enabled && containsString(selectedIDs, clusters[i].ID)
		clusters[i].CollectionRunning = clusters[i].CollectionEnabled && probeRunning
	}

	writeJSON(w, moldKubernetesClustersResponse{
		Clusters:       clusters,
		SelectedID:     selectedID,
		SelectedIDs:    selectedIDs,
		KubeconfigPath: moldKubernetesKubeconfigPath(),
		RestartNeeded:  false,
		ProbeRunning:   probeRunning,
	})
}

type moldKubernetesProbeStarter func() error
type moldKubernetesProbeStopper func()
type moldKubernetesProbeStatus func() bool

func handleMoldKubernetesSelect(w http.ResponseWriter, r *http.Request, startProbe moldKubernetesProbeStarter) {
	var req moldKubernetesSelectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	selectedIDs := normalizeClusterIDs(req.IDs, req.ID)
	if len(selectedIDs) == 0 {
		http.Error(w, "cluster id is required", http.StatusBadRequest)
		return
	}

	clusters, err := listMoldKubernetesClusters()
	if err != nil {
		writeMoldKubernetesError(w, err)
		return
	}
	clusterByID := make(map[string]moldKubernetesCluster, len(clusters))
	for i := range clusters {
		clusterByID[clusters[i].ID] = clusters[i]
	}
	selectedClusters := make([]moldKubernetesCluster, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		selected, ok := clusterByID[id]
		if !ok {
			http.Error(w, "cluster not found", http.StatusNotFound)
			return
		}
		selectedClusters = append(selectedClusters, selected)
	}

	previousSelection, _ := readMoldKubernetesSelection()
	clusterNames := make([]string, 0, len(selectedClusters))
	apiServers := make([]string, 0, len(selectedClusters))
	var activeKubeconfig []byte
	for index, selected := range selectedClusters {
		kubeconfig, err := getMoldKubernetesConfig(selected.ID)
		if err != nil {
			writeMoldKubernetesError(w, err)
			return
		}
		clusterPath := moldKubernetesClusterKubeconfigPath(selected.ID)
		if err := writeSecureFile(clusterPath, []byte(kubeconfig)); err != nil {
			http.Error(w, "failed to save kubeconfig", http.StatusInternalServerError)
			return
		}
		if index == 0 {
			activeKubeconfig = []byte(kubeconfig)
		}
		clusterNames = append(clusterNames, selected.Name)
		apiServers = append(apiServers, selected.APIServer)
	}
	path := moldKubernetesKubeconfigPath()
	if len(activeKubeconfig) > 0 {
		if err := writeSecureFile(path, activeKubeconfig); err != nil {
			http.Error(w, "failed to save active kubeconfig", http.StatusInternalServerError)
			return
		}
	}

	selection := moldKubernetesSelection{
		Enabled:      true,
		ClusterID:    selectedClusters[0].ID,
		ClusterName:  selectedClusters[0].Name,
		APIServer:    selectedClusters[0].APIServer,
		ClusterIDs:   selectedIDs,
		ClusterNames: clusterNames,
		APIServers:   apiServers,
		UpdatedAt:    time.Now().UTC(),
	}
	if err := writeMoldKubernetesSelection(selection); err != nil {
		http.Error(w, "failed to save collection state", http.StatusInternalServerError)
		return
	}
	if err := removeDeselectedKubernetesManagedKubeconfigs(previousSelection, selection); err != nil {
		http.Error(w, "failed to clean stale kubeconfigs", http.StatusInternalServerError)
		return
	}
	stopDeselectedKubernetesClients(previousSelection, selection)

	message := "kubeconfig saved. k8s probe started."
	probeRunning := false
	if startProbe != nil {
		if err := startProbe(); err != nil {
			message = "kubeconfig saved, but k8s probe start failed. check analyzer logs."
		} else {
			probeRunning = true
		}
	} else {
		message = "kubeconfig saved. restart analyzer to apply k8s probe if it is not running."
	}

	writeJSON(w, moldKubernetesStatusResponse{
		OK:             probeRunning || startProbe == nil,
		Message:        message,
		KubeconfigPath: path,
		ProbeRunning:   probeRunning,
	})
}

func handleMoldKubernetesDisable(w http.ResponseWriter, r *http.Request, stopProbe moldKubernetesProbeStopper) {
	previousSelection, _ := readMoldKubernetesSelection()
	selection := moldKubernetesSelection{Enabled: false, UpdatedAt: time.Now().UTC()}
	if err := writeMoldKubernetesSelection(selection); err != nil {
		http.Error(w, "failed to save collection state", http.StatusInternalServerError)
		return
	}
	if err := removeMoldKubernetesManagedKubeconfigs(previousSelection); err != nil {
		http.Error(w, "failed to remove kubeconfig", http.StatusInternalServerError)
		return
	}
	for _, clusterID := range normalizedSelectionClusterIDs(previousSelection) {
		stopKubernetesClient(clusterID)
	}
	if stopProbe != nil {
		stopProbe()
	}
	writeJSON(w, moldKubernetesStatusResponse{OK: true, Message: "kubernetes collection disabled."})
}

func handleMoldKubernetesTest(w http.ResponseWriter, r *http.Request) {
	var req moldKubernetesTestRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	checks := make([]moldKubernetesCheckResult, 0, 8)
	addCheck := func(key, label string, ok bool, reason, message string) {
		checks = append(checks, moldKubernetesCheckResult{Key: key, Label: label, OK: ok, Reason: reason, Message: message})
	}
	fail := func(key, label string, err error, message string) {
		addCheck(key, label, false, sanitizeKubernetesTestError(err), message)
	}

	var kubeconfigData []byte
	if strings.TrimSpace(req.ID) != "" {
		configText, err := getMoldKubernetesConfig(req.ID)
		if err != nil {
			fail("kubeconfig", "kubeconfig 생성/읽기", err, "Mold에서 kubeconfig를 가져오지 못했습니다.")
			writeJSON(w, moldKubernetesTestResponse{OK: false, Message: "connection test failed", Checks: checks})
			return
		}
		kubeconfigData = []byte(configText)
	} else {
		path := moldKubernetesKubeconfigPath()
		data, err := os.ReadFile(path)
		if err != nil {
			fail("kubeconfig", "kubeconfig 생성/읽기", err, "저장된 kubeconfig를 읽지 못했습니다.")
			writeJSON(w, moldKubernetesTestResponse{OK: false, Message: "connection test failed", Checks: checks})
			return
		}
		kubeconfigData = data
	}

	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		fail("kubeconfig", "kubeconfig 생성/읽기", err, "kubeconfig 형식이 올바르지 않습니다.")
		writeJSON(w, moldKubernetesTestResponse{OK: false, Message: "connection test failed", Checks: checks})
		return
	}
	clientConfig.Timeout = 10 * time.Second
	addCheck("kubeconfig", "kubeconfig 생성/읽기", true, "", "kubeconfig를 읽었습니다.")
	addCheck("apiserver", "API Server 접근", true, "", "API Server 설정을 확인했습니다.")

	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		fail("client", "Kubernetes client 확인", err, "Kubernetes client를 생성하지 못했습니다.")
		writeJSON(w, moldKubernetesTestResponse{OK: false, Message: "connection test failed", Checks: checks})
		return
	}
	addCheck("client", "Kubernetes client 확인", true, "", "Kubernetes client를 생성했습니다.")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if version, err := clientset.Discovery().ServerVersion(); err != nil {
		fail("version", "/version 호출", err, "Kubernetes 버전 정보를 조회하지 못했습니다.")
	} else {
		addCheck("version", "/version 호출", true, "", fmt.Sprintf("%s", version.GitVersion))
	}
	if _, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("namespaces", "namespace 목록 조회", err, "namespace 목록을 조회하지 못했습니다.")
	} else {
		addCheck("namespaces", "namespace 목록 조회", true, "", "namespace 조회 권한을 확인했습니다.")
	}
	if _, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("nodes", "node 목록 조회", err, "node 목록을 조회하지 못했습니다.")
	} else {
		addCheck("nodes", "node 목록 조회", true, "", "node 조회 권한을 확인했습니다.")
	}
	if _, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("pods", "pod 목록 조회", err, "pod 목록을 조회하지 못했습니다.")
	} else {
		addCheck("pods", "pod 목록 조회", true, "", "pod 조회 권한을 확인했습니다.")
	}
	if _, err := clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("services", "service 목록 조회", err, "service 목록을 조회하지 못했습니다.")
	} else {
		addCheck("services", "service 목록 조회", true, "", "service 조회 권한을 확인했습니다.")
	}
	if _, err := clientset.NetworkingV1().NetworkPolicies("").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		fail("networkpolicies", "networkpolicy 목록 조회", err, "networkpolicy 목록을 조회하지 못했습니다.")
	} else {
		addCheck("networkpolicies", "networkpolicy 목록 조회", true, "", "networkpolicy 조회 권한을 확인했습니다.")
	}

	ok := true
	for _, check := range checks {
		if !check.OK {
			ok = false
			break
		}
	}
	message := "connection test succeeded"
	if !ok {
		message = "connection test failed"
	}
	writeJSON(w, moldKubernetesTestResponse{OK: ok, Message: message, Checks: checks})
}

func handleMoldKubernetesClusterSummary(w http.ResponseWriter, r *http.Request, isProbeRunning moldKubernetesProbeStatus) {
	clusterID := strings.TrimSpace(r.URL.Query().Get("id"))
	if clusterID == "" {
		http.Error(w, "cluster id is required", http.StatusBadRequest)
		return
	}

	clusters, err := listMoldKubernetesClusters()
	if err != nil {
		writeMoldKubernetesError(w, err)
		return
	}
	var selected *moldKubernetesCluster
	for i := range clusters {
		if clusters[i].ID == clusterID {
			selected = &clusters[i]
			break
		}
	}
	if selected == nil {
		http.Error(w, "cluster not found", http.StatusNotFound)
		return
	}
	selection, _ := readMoldKubernetesSelection()
	selected.CollectionEnabled = selection.Enabled && containsString(normalizedSelectionClusterIDs(selection), selected.ID)
	selected.CollectionRunning = selected.CollectionEnabled && isProbeRunning != nil && isProbeRunning()

	summary, err := collectKubernetesClusterSummary(*selected)
	if err != nil {
		writeMoldKubernetesError(w, err)
		return
	}
	writeJSON(w, summary)
}

func collectKubernetesClusterSummary(cluster moldKubernetesCluster) (moldKubernetesClusterSummary, error) {
	clientset, _, err := getMoldKubernetesClient(cluster)
	if err != nil {
		return moldKubernetesClusterSummary{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return moldKubernetesClusterSummary{}, newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes node 목록을 조회하지 못했습니다.", err)
	}
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return moldKubernetesClusterSummary{}, newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes pod 목록을 조회하지 못했습니다.", err)
	}
	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return moldKubernetesClusterSummary{}, newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes namespace 목록을 조회하지 못했습니다.", err)
	}
	services, err := clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return moldKubernetesClusterSummary{}, newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes service 목록을 조회하지 못했습니다.", err)
	}
	endpointSlices, endpointSliceErr := clientset.DiscoveryV1().EndpointSlices("").List(ctx, metav1.ListOptions{})

	summary := moldKubernetesClusterSummary{
		Cluster:        cluster,
		ClusterID:      cluster.ID,
		APIServer:      cluster.APIServer,
		Namespaces:     len(namespaces.Items),
		NamespaceCount: len(namespaces.Items),
		Services:       len(services.Items),
		ServiceCount:   len(services.Items),
		Risks:          make([]kubernetesRiskItem, 0),
		RecentChanges:  make([]kubernetesRecentChange, 0),
		CollectedAt:    time.Now().UTC(),
	}
	for i := range namespaces.Items {
		if namespaces.Items[i].Name == metav1.NamespaceSystem {
			summary.ClusterUID = string(namespaces.Items[i].UID)
			break
		}
	}
	if version, versionErr := clientset.Discovery().ServerVersion(); versionErr == nil {
		summary.Version = version.GitVersion
	}
	if summary.Version == "" {
		summary.Version = cluster.Version
	}
	summary.KubernetesVersion = summary.Version

	notReadyNodeNames := make(map[string]bool)
	pressureCount := 0
	for i := range nodes.Items {
		node := &nodes.Items[i]
		ready := kubernetesNodeReady(node)
		controlPlane := isKubernetesControlPlaneNode(node)
		summary.Nodes.Total++
		if ready {
			summary.Nodes.Ready++
		} else {
			summary.Nodes.NotReady++
			notReadyNodeNames[node.Name] = true
		}
		if controlPlane {
			summary.ControlPlane.Total++
			if ready {
				summary.ControlPlane.Ready++
			} else {
				summary.ControlPlane.NotReady++
			}
		}

		addResourceList(&summary.Resources, node.Status.Capacity, true)
		addResourceList(&summary.Resources, node.Status.Allocatable, false)
		for _, condition := range node.Status.Conditions {
			if !condition.LastTransitionTime.IsZero() {
				summary.RecentChanges = append(summary.RecentChanges, kubernetesRecentChange{
					Time: condition.LastTransitionTime.Time, Resource: node.Name,
					Message:  fmt.Sprintf("%s: %s", condition.Type, condition.Status),
					Severity: nodeConditionSeverity(condition),
				})
			}
			if condition.Status == corev1.ConditionTrue && (condition.Type == corev1.NodeMemoryPressure || condition.Type == corev1.NodeDiskPressure || condition.Type == corev1.NodePIDPressure || condition.Type == corev1.NodeNetworkUnavailable) {
				pressureCount++
			}
		}
	}

	localStorageWorkloads := make(map[string]bool)
	podAggregate := aggregateKubernetesPods(pods.Items)
	summary.Pods.Total = len(podAggregate.ActivePods)
	summary.Pods.Running = podAggregate.Running
	summary.Pods.Pending = podAggregate.Pending
	summary.Pods.Problem = len(podAggregate.ProblemPods)
	summary.Pods.Failed = 0
	summary.Pods.Unknown = 0
	for i := range podAggregate.ActivePods {
		pod := &podAggregate.ActivePods[i]
		for _, volume := range pod.Spec.Volumes {
			if volume.HostPath != nil {
				workloadID := string(pod.UID)
				if len(pod.OwnerReferences) > 0 {
					workloadID = string(pod.OwnerReferences[0].UID)
				}
				localStorageWorkloads[workloadID] = true
				break
			}
		}
		addPodResources(&summary.Resources, pod)
	}
	// Pod conditions are event/history data and remain available independently
	// from the active resource count above.
	for i := range pods.Items {
		pod := &pods.Items[i]
		for _, condition := range pod.Status.Conditions {
			if condition.LastTransitionTime.IsZero() {
				continue
			}
			severity := "info"
			if condition.Status == corev1.ConditionFalse && (condition.Type == corev1.PodReady || condition.Type == corev1.ContainersReady) {
				severity = "warning"
			}
			summary.RecentChanges = append(summary.RecentChanges, kubernetesRecentChange{
				Time: condition.LastTransitionTime.Time, Resource: pod.Namespace + "/" + pod.Name,
				Message: fmt.Sprintf("%s: %s", condition.Type, condition.Status), Severity: severity,
			})
		}
	}

	externalPaths := make(map[string]bool)
	endpointNodes := make(map[string]map[string]bool)
	if endpointSliceErr == nil {
		for i := range endpointSlices.Items {
			slice := &endpointSlices.Items[i]
			serviceName := slice.Labels[discoveryv1.LabelServiceName]
			if serviceName == "" {
				continue
			}
			serviceKey := slice.Namespace + "/" + serviceName
			if endpointNodes[serviceKey] == nil {
				endpointNodes[serviceKey] = make(map[string]bool)
			}
			for _, endpoint := range slice.Endpoints {
				if (endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready) && endpoint.NodeName != nil {
					endpointNodes[serviceKey][*endpoint.NodeName] = true
				}
			}
		}
	}
	for i := range services.Items {
		service := &services.Items[i]
		serviceKey := service.Namespace + "/" + service.Name
		if service.Spec.Type == corev1.ServiceTypeNodePort || service.Spec.Type == corev1.ServiceTypeLoadBalancer || service.Spec.Type == corev1.ServiceTypeExternalName || len(service.Spec.ExternalIPs) > 0 {
			externalPaths[fmt.Sprintf("Service %s/%s (%s)", service.Namespace, service.Name, service.Spec.Type)] = true
		}
		if len(endpointNodes[serviceKey]) == 1 {
			summary.SingleNodeEndpointServiceCount++
		}
	}
	serviceImpact := aggregateKubernetesServiceImpact(services.Items, endpointSlices.Items, podAggregate, notReadyNodeNames)
	impactedServices := make(map[string]bool)
	for key, impact := range serviceImpact {
		if impact.Affected {
			impactedServices[key] = true
		}
	}
	if ingresses, ingressErr := clientset.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{}); ingressErr == nil {
		for i := range ingresses.Items {
			ingress := &ingresses.Items[i]
			externalPaths[fmt.Sprintf("Ingress %s/%s", ingress.Namespace, ingress.Name)] = true
		}
	}
	if deployments, deploymentErr := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{}); deploymentErr == nil {
		for i := range deployments.Items {
			if deployments.Items[i].Spec.Replicas != nil && *deployments.Items[i].Spec.Replicas == 1 {
				summary.SingleReplicaWorkloadCount++
			}
		}
	}
	if statefulSets, statefulSetErr := clientset.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{}); statefulSetErr == nil {
		for i := range statefulSets.Items {
			if statefulSets.Items[i].Spec.Replicas != nil && *statefulSets.Items[i].Spec.Replicas == 1 {
				summary.SingleReplicaWorkloadCount++
			}
		}
	}

	collectKubernetesNodeMetrics(ctx, clientset, &summary.Resources)
	collectKubernetesPodMetrics(ctx, clientset, &summary.Resources, podAggregate.ActivePods)
	summary.AffectedServices = len(impactedServices)
	summary.CurrentlyImpactedServiceCount = summary.AffectedServices
	summary.LocalStorageDependentWorkloadCount = len(localStorageWorkloads)
	for detail := range externalPaths {
		summary.ExternalPathDetails = append(summary.ExternalPathDetails, detail)
	}
	sort.Strings(summary.ExternalPathDetails)
	summary.ExternalPathCount = len(summary.ExternalPathDetails)
	if summary.Nodes.NotReady > 0 {
		summary.Risks = append(summary.Risks, kubernetesRiskItem{Severity: "critical", Title: "NotReady 노드", Message: "Ready 상태가 아닌 노드가 있습니다.", Count: summary.Nodes.NotReady})
	}
	if summary.ControlPlane.NotReady > 0 {
		summary.Risks = append(summary.Risks, kubernetesRiskItem{Severity: "critical", Title: "Control Plane 이상", Message: "Ready 상태가 아닌 Control Plane 노드가 있습니다.", Count: summary.ControlPlane.NotReady})
	}
	if summary.Pods.Problem > 0 {
		summary.Risks = append(summary.Risks, kubernetesRiskItem{Severity: "critical", Title: "현재 문제 Pod", Message: "현재 조치가 필요한 활성 Pod가 있습니다.", Count: summary.Pods.Problem})
	}
	if summary.Pods.Pending > 0 {
		summary.Risks = append(summary.Risks, kubernetesRiskItem{Severity: "warning", Title: "Pending Pod", Message: "스케줄링 또는 시작을 기다리는 Pod가 있습니다.", Count: summary.Pods.Pending})
	}
	if pressureCount > 0 {
		summary.Risks = append(summary.Risks, kubernetesRiskItem{Severity: "warning", Title: "노드 압박 상태", Message: "메모리·디스크·PID 또는 네트워크 압박 조건이 감지되었습니다.", Count: pressureCount})
	}
	if summary.AffectedServices > 0 {
		summary.Risks = append(summary.Risks, kubernetesRiskItem{Severity: "critical", Title: "서비스 영향", Message: "장애 노드 또는 Failed Pod가 서비스 엔드포인트에 영향을 줍니다.", Count: summary.AffectedServices})
	}
	if !summary.Resources.MetricsAvailable {
		summary.Risks = append(summary.Risks, kubernetesRiskItem{Severity: "info", Title: "사용률 데이터 미수집", Message: "metrics-server에서 CPU·메모리 사용률을 가져오지 못했습니다."})
	}
	summary.ImpactScore = minInt(100, summary.Nodes.NotReady*25+summary.ControlPlane.NotReady*35+summary.Pods.Problem*8+summary.Pods.Pending*2+summary.AffectedServices*10)
	summary.CurrentImpactScore = summary.ImpactScore
	summary.SingleControlPlane = summary.ControlPlane.Total == 1
	summary.InfrastructureRiskScore = minInt(100,
		boolScore(summary.SingleControlPlane, 15)+
			boolScore(summary.SingleReplicaWorkloadCount > 0, 15)+
			boolScore(summary.SingleNodeEndpointServiceCount > 0, 15)+
			boolScore(summary.LocalStorageDependentWorkloadCount > 0, 15)+
			boolScore(summary.ExternalPathCount == 1, 10))
	summary.ControlPlaneReady = summary.ControlPlane.Ready
	summary.ControlPlaneTotal = summary.ControlPlane.Total
	summary.NodeReady = summary.Nodes.Ready
	summary.NodeTotal = summary.Nodes.Total
	summary.PodRunning = summary.Pods.Running
	summary.PodPending = summary.Pods.Pending
	summary.PodProblem = summary.Pods.Problem
	summary.PodFailed = summary.Pods.Failed
	setKubernetesCollectionState(cluster.ID, kubernetesHealthy, nil)
	collectionState := getKubernetesCollectionState(cluster.ID)
	summary.APIConnectionStatus = collectionState.Status
	summary.LastSyncAt = collectionState.LastSyncAt
	sort.Slice(summary.RecentChanges, func(i, j int) bool { return summary.RecentChanges[i].Time.After(summary.RecentChanges[j].Time) })
	if len(summary.RecentChanges) > 8 {
		summary.RecentChanges = summary.RecentChanges[:8]
	}
	return summary, nil
}

func kubernetesNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func isKubernetesControlPlaneNode(node *corev1.Node) bool {
	_, controlPlane := node.Labels["node-role.kubernetes.io/control-plane"]
	_, master := node.Labels["node-role.kubernetes.io/master"]
	return controlPlane || master
}

func nodeConditionSeverity(condition corev1.NodeCondition) string {
	if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
		return "critical"
	}
	if condition.Status == corev1.ConditionTrue && condition.Type != corev1.NodeReady {
		return "warning"
	}
	return "info"
}

func addResourceList(summary *kubernetesResourceSummary, resources corev1.ResourceList, capacity bool) {
	cpu := resources.Cpu()
	memory := resources.Memory()
	pods := resources.Pods()
	if capacity {
		summary.CapacityCPUCores += float64(cpu.MilliValue()) / 1000
		summary.CapacityMemoryBytes += memory.Value()
		summary.CapacityPods += pods.Value()
	} else {
		summary.AllocatableCPUCores += float64(cpu.MilliValue()) / 1000
		summary.AllocatableMemoryBytes += memory.Value()
		summary.AllocatablePods += pods.Value()
	}
}

func addPodResources(summary *kubernetesResourceSummary, pod *corev1.Pod) {
	for _, container := range pod.Spec.Containers {
		summary.RequestsCPUCores += float64(container.Resources.Requests.Cpu().MilliValue()) / 1000
		summary.RequestsMemoryBytes += container.Resources.Requests.Memory().Value()
		summary.LimitsCPUCores += float64(container.Resources.Limits.Cpu().MilliValue()) / 1000
		summary.LimitsMemoryBytes += container.Resources.Limits.Memory().Value()
	}
}

func collectKubernetesNodeMetrics(ctx context.Context, clientset kubernetes.Interface, summary *kubernetesResourceSummary) {
	raw, err := clientset.CoreV1().RESTClient().Get().AbsPath("/apis/metrics.k8s.io/v1beta1/nodes").DoRaw(ctx)
	if err != nil {
		return
	}
	var metrics kubernetesNodeMetricsList
	if err := json.Unmarshal(raw, &metrics); err != nil || len(metrics.Items) == 0 {
		return
	}
	for _, item := range metrics.Items {
		summary.UsageCPUCores += float64(item.Usage.Cpu().MilliValue()) / 1000
		summary.UsageMemoryBytes += item.Usage.Memory().Value()
	}
	summary.MetricsAvailable = true
	if summary.AllocatableCPUCores > 0 {
		summary.CPUUsagePercent = summary.UsageCPUCores / summary.AllocatableCPUCores * 100
	}
	if summary.AllocatableMemoryBytes > 0 {
		summary.MemoryUsagePercent = float64(summary.UsageMemoryBytes) / float64(summary.AllocatableMemoryBytes) * 100
	}
}

// collectKubernetesPodMetrics augments the cluster aggregate with drill-down
// data only. It deliberately does not modify the existing cluster usage totals.
func collectKubernetesPodMetrics(ctx context.Context, clientset kubernetes.Interface, summary *kubernetesResourceSummary, activePods []corev1.Pod) {
	raw, err := clientset.CoreV1().RESTClient().Get().AbsPath("/apis/metrics.k8s.io/v1beta1/pods").DoRaw(ctx)
	if err != nil {
		return
	}
	var metrics kubernetesPodMetricsList
	if err := json.Unmarshal(raw, &metrics); err != nil {
		return
	}
	activeByKey := make(map[string]*corev1.Pod, len(activePods))
	for i := range activePods {
		pod := &activePods[i]
		activeByKey[pod.Namespace+"/"+pod.Name] = pod
	}
	for _, item := range metrics.Items {
		pod := activeByKey[item.Metadata.Namespace+"/"+item.Metadata.Name]
		if pod == nil {
			continue
		}
		usage := kubernetesPodResourceUsage{
			Namespace: item.Metadata.Namespace,
			Name:      item.Metadata.Name,
			NodeName:  pod.Spec.NodeName,
		}
		for _, container := range item.Containers {
			usage.UsageCPUCores += float64(container.Usage.Cpu().MilliValue()) / 1000
			usage.UsageMemoryBytes += container.Usage.Memory().Value()
		}
		summary.PodUsage = append(summary.PodUsage, usage)
	}
}

func labelsMatch(selector, labels map[string]string) bool {
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func boolScore(condition bool, score int) int {
	if condition {
		return score
	}
	return 0
}

func listMoldKubernetesClusters() ([]moldKubernetesCluster, error) {
	body, _, err := requestMoldAPI(moldKubernetesListCommand, []apiParam{
		{Key: "command", Value: moldKubernetesListCommand},
		{Key: "response", Value: "json"},
	})
	if err != nil {
		return nil, err
	}
	return parseMoldKubernetesClusters(body)
}

// MoldKubernetesClusterRuntimeStates exposes only the Mold lifecycle state
// needed by the topology probe supervisor. Stopped clusters are intentionally
// treated as inactive: any detail informer already watching them is stopped so
// client-go does not keep retrying an API server that Mold has shut down.
func MoldKubernetesClusterRuntimeStates() (map[string]string, error) {
	clusters, err := listMoldKubernetesClusters()
	if err != nil {
		return nil, err
	}
	states := make(map[string]string, len(clusters))
	for _, cluster := range clusters {
		states[cluster.ID] = cluster.State
		if isStoppedMoldKubernetesState(cluster.State) {
			pauseKubernetesClient(cluster.ID)
			setKubernetesCollectionState(cluster.ID, kubernetesInactive, nil)
		}
	}
	return states, nil
}

func isStoppedMoldKubernetesState(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "stopped")
}

func isTransitioningMoldKubernetesState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "starting", "stopping":
		return true
	default:
		return false
	}
}

func getMoldKubernetesConfig(clusterID string) (string, error) {
	body, _, err := requestMoldAPI(moldKubernetesConfigCommand, []apiParam{
		{Key: "command", Value: moldKubernetesConfigCommand},
		{Key: "response", Value: "json"},
		{Key: "id", Value: clusterID},
	})
	if err != nil {
		return "", err
	}
	configData := findStringByKey(body, "configdata")
	if strings.TrimSpace(configData) == "" {
		return "", newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes kubeconfig 응답이 비어 있습니다.", nil)
	}
	return configData, nil
}

func requestMoldAPI(command string, params []apiParam) ([]byte, int, error) {
	apiCfg := common.GetMoldAPIConfig()
	if apiCfg.Endpoint == "" {
		return nil, 0, newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API endpoint 설정이 비어 있습니다.", nil)
	}
	apiKey, secretKey, err := common.ReadMoldAPIKeys()
	if err != nil {
		return nil, 0, newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API Key/Secret 로드 실패", err)
	}
	baseURL, err := url.Parse(apiCfg.Endpoint)
	if err != nil {
		return nil, 0, newVMConsoleAPIError(http.StatusServiceUnavailable, "Mold API endpoint 설정이 올바르지 않습니다.", err)
	}

	reqParams := make([]apiParam, 0, len(params)+1)
	reqParams = append(reqParams, params...)
	reqParams = append(reqParams, apiParam{Key: "apikey", Value: apiKey})

	u := *baseURL
	u.RawQuery = buildSignedRawQuery(reqParams, secretKey)
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext},
	}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, 0, newVMConsoleAPIError(http.StatusBadGateway, "Mold API 호출 실패", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, newVMConsoleAPIError(http.StatusBadGateway, "Mold API 응답 읽기 실패", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return body, resp.StatusCode, newVMConsoleAPIError(http.StatusBadGateway, fmt.Sprintf("Mold API %s 호출 실패", command), nil)
	}
	return body, resp.StatusCode, nil
}

func parseMoldKubernetesClusters(body []byte) ([]moldKubernetesCluster, error) {
	var payload interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, newVMConsoleAPIError(http.StatusBadGateway, "Kubernetes 클러스터 목록 응답 파싱 실패", err)
	}

	items := findArrayByKey(payload, "kubernetescluster")
	clusters := make([]moldKubernetesCluster, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		clusters = append(clusters, moldKubernetesCluster{
			ID:              valueAsString(m["id"]),
			Name:            valueAsString(m["name"]),
			State:           valueAsString(m["state"]),
			APIServer:       firstNonEmpty(valueAsString(m["endpoint"]), valueAsString(m["apiserver"]), valueAsString(m["apiServer"])),
			Description:     valueAsString(m["description"]),
			ZoneName:        firstNonEmpty(valueAsString(m["zonename"]), valueAsString(m["zoneName"])),
			Version:         firstNonEmpty(valueAsString(m["kubernetesversionname"]), valueAsString(m["kubernetesVersionName"])),
			ControlNodes:    valueAsInt64(firstNonNil(m["controlnodes"], m["controlNodes"], m["masternodes"])),
			WorkerNodes:     valueAsInt64(firstNonNil(m["size"], m["workernodes"], m["workerNodes"])),
			Cores:           valueAsInt64(firstNonNil(m["cpunumber"], m["cores"])),
			MemoryMB:        valueAsInt64(m["memory"]),
			NetworkName:     firstNonEmpty(valueAsString(m["associatednetworkname"]), valueAsString(m["associatedNetworkName"])),
			ServiceOffering: firstNonEmpty(valueAsString(m["serviceofferingname"]), valueAsString(m["serviceOfferingName"])),
			IPAddress:       firstNonEmpty(valueAsString(m["ipaddress"]), valueAsString(m["ipAddress"])),
			Autoscaling:     valueAsBool(firstNonNil(m["autoscalingenabled"], m["autoscalingEnabled"])),
			MinSize:         valueAsInt64(firstNonNil(m["minsize"], m["minSize"])),
			MaxSize:         valueAsInt64(firstNonNil(m["maxsize"], m["maxSize"])),
			Created:         valueAsString(m["created"]),
		})
	}
	return clusters, nil
}

func findArrayByKey(v interface{}, key string) []interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			if strings.EqualFold(k, key) {
				if arr, ok := vv.([]interface{}); ok {
					return arr
				}
			}
			if arr := findArrayByKey(vv, key); arr != nil {
				return arr
			}
		}
	case []interface{}:
		for _, item := range t {
			if arr := findArrayByKey(item, key); arr != nil {
				return arr
			}
		}
	}
	return nil
}

func findStringByKey(body []byte, key string) string {
	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return findStringValue(payload, key)
}

func findStringValue(v interface{}, key string) string {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, vv := range t {
			if strings.EqualFold(k, key) {
				return valueAsString(vv)
			}
			if found := findStringValue(vv, key); found != "" {
				return found
			}
		}
	case []interface{}:
		for _, item := range t {
			if found := findStringValue(item, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func valueAsString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case fmt.Stringer:
		return t.String()
	default:
		if t == nil {
			return ""
		}
		return fmt.Sprintf("%v", t)
	}
}

func valueAsInt64(v interface{}) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(valueAsString(v)), 10, 64)
	return value
}

func valueAsBool(v interface{}) bool {
	switch value := v.(type) {
	case bool:
		return value
	case json.Number:
		return value.String() != "0"
	default:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(valueAsString(v)))
		return parsed
	}
}

func firstNonNil(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func isMoldKubernetesSelectionCurrent(selection moldKubernetesSelection, clusters []moldKubernetesCluster) bool {
	selectedIDs := normalizedSelectionClusterIDs(selection)
	if !selection.Enabled || len(selectedIDs) == 0 {
		return false
	}
	apisByID := make(map[string]string, len(clusters))
	for _, cluster := range clusters {
		apisByID[cluster.ID] = cluster.APIServer
	}
	for index, id := range selectedIDs {
		apiServer, ok := apisByID[id]
		if !ok {
			return false
		}
		if index < len(selection.APIServers) && strings.TrimSpace(selection.APIServers[index]) != "" && strings.TrimSpace(apiServer) != "" && selection.APIServers[index] != apiServer {
			return false
		}
	}
	return true
}

func moldKubernetesKubeconfigPath() string {
	path := config.GetString("analyzer.topology.k8s.config_file")
	if path != "" {
		return path
	}
	return config.GetString("mold.kubernetes.kubeconfigPath")
}

func moldKubernetesClusterKubeconfigPath(clusterID string) string {
	basePath := moldKubernetesKubeconfigPath()
	safeID := sanitizeKubernetesFileName(clusterID)
	if safeID == "" {
		safeID = "selected"
	}
	return filepath.Join(filepath.Dir(basePath), safeID+".kubeconfig")
}

func removeMoldKubernetesManagedKubeconfigs(selection moldKubernetesSelection) error {
	if !config.GetBool("mold.kubernetes.enforceSelection") {
		return nil
	}
	paths := []string{moldKubernetesKubeconfigPath()}
	for _, clusterID := range normalizedSelectionClusterIDs(selection) {
		paths = append(paths, moldKubernetesClusterKubeconfigPath(clusterID))
	}
	seen := make(map[string]bool)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func removeDeselectedKubernetesManagedKubeconfigs(previous, current moldKubernetesSelection) error {
	currentSet := make(map[string]bool)
	for _, id := range normalizedSelectionClusterIDs(current) {
		currentSet[id] = true
	}
	for _, id := range normalizedSelectionClusterIDs(previous) {
		if currentSet[id] {
			continue
		}
		path := moldKubernetesClusterKubeconfigPath(id)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func sanitizeKubernetesFileName(value string) string {
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

func sanitizeKubernetesTestError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	blocked := []string{"token", "client-key-data", "client-certificate-data", "certificate-authority-data", "authorization", "bearer"}
	lower := strings.ToLower(msg)
	for _, keyword := range blocked {
		if strings.Contains(lower, keyword) {
			return "민감정보를 포함할 수 있는 오류입니다. analyzer 로그와 kubeconfig 권한을 확인하세요."
		}
	}
	if len(msg) > 220 {
		msg = msg[:220] + "..."
	}
	return msg
}

func moldKubernetesStatePath() string {
	path := config.GetString("mold.kubernetes.stateFile")
	if path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(moldKubernetesKubeconfigPath()), "selected-cluster.json")
}

func readMoldKubernetesSelection() (moldKubernetesSelection, error) {
	var selection moldKubernetesSelection
	data, err := os.ReadFile(moldKubernetesStatePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return selection, nil
		}
		return selection, err
	}
	if err := json.Unmarshal(data, &selection); err != nil {
		return moldKubernetesSelection{}, err
	}
	return selection, nil
}

func writeMoldKubernetesSelection(selection moldKubernetesSelection) error {
	selection.ClusterIDs = normalizedSelectionClusterIDs(selection)
	if len(selection.ClusterIDs) == 0 {
		selection.ClusterID = ""
		selection.ClusterName = ""
		selection.APIServer = ""
		selection.ClusterNames = nil
		selection.APIServers = nil
	} else {
		selection.ClusterID = selection.ClusterIDs[0]
		if len(selection.ClusterNames) > 0 {
			selection.ClusterName = selection.ClusterNames[0]
		}
		if len(selection.APIServers) > 0 {
			selection.APIServer = selection.APIServers[0]
		}
	}
	data, err := json.MarshalIndent(selection, "", "  ")
	if err != nil {
		return err
	}
	return writeSecureFile(moldKubernetesStatePath(), data)
}

func normalizedSelectionClusterIDs(selection moldKubernetesSelection) []string {
	return normalizeClusterIDs(selection.ClusterIDs, selection.ClusterID)
}

func normalizeClusterIDs(ids []string, fallback ...string) []string {
	normalized := make([]string, 0, len(ids)+len(fallback))
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
	for _, id := range fallback {
		appendID(id)
	}
	return normalized
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func writeSecureFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeMoldKubernetesError(w http.ResponseWriter, err error) {
	apiErr := &vmConsoleAPIError{StatusCode: http.StatusBadGateway, Message: "Kubernetes 클러스터 연동 실패", Err: err}
	if errors.As(err, &apiErr) {
		http.Error(w, apiErr.Message, apiErr.StatusCode)
		return
	}
	http.Error(w, apiErr.Message, apiErr.StatusCode)
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func RegisterMoldKubernetesAPI(httpServer *shttp.Server, startProbe moldKubernetesProbeStarter, stopProbe moldKubernetesProbeStopper, isProbeRunning moldKubernetesProbeStatus) {
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters", func(w http.ResponseWriter, r *http.Request) {
		handleMoldKubernetesClusters(w, r, isProbeRunning, stopProbe)
	}).Methods("GET")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/select", func(w http.ResponseWriter, r *http.Request) {
		handleMoldKubernetesSelect(w, r, startProbe)
	}).Methods("POST")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/disable", func(w http.ResponseWriter, r *http.Request) {
		handleMoldKubernetesDisable(w, r, stopProbe)
	}).Methods("POST")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/test", handleMoldKubernetesTest).Methods("POST")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/summary", func(w http.ResponseWriter, r *http.Request) {
		handleMoldKubernetesClusterSummary(w, r, isProbeRunning)
	}).Methods("GET")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/nodes/detail", handleMoldKubernetesNodeDetail).Methods("GET")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/namespaces/detail", handleMoldKubernetesNamespaceDetail).Methods("GET")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/pods/detail", handleMoldKubernetesPodDetail).Methods("GET")
	httpServer.Router.HandleFunc("/api/mold/kubernetes-clusters/services/detail", handleMoldKubernetesServiceDetail).Methods("GET")
}
