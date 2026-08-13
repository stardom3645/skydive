package server

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

var kubernetesActionablePodWaitingReasons = map[string]struct{}{
	"CrashLoopBackOff":           {},
	"ImagePullBackOff":           {},
	"ErrImagePull":               {},
	"CreateContainerConfigError": {},
	"CreateContainerError":       {},
	"RunContainerError":          {},
	"ContainerStatusUnknown":     {},
}

// kubernetesPodScope keeps the domain aggregate independent from an API or UI.
// Namespace and workload detail APIs can reuse it later without redefining Pod
// phase/reason rules.
type kubernetesPodScope struct {
	NodeName       string
	Namespace      string
	OwnerUID       string
	OwnerUIDForPod func(*corev1.Pod) string
	Predicate      func(*corev1.Pod) bool
}

// kubernetesPodClassification is the single Analyzer domain rule used by
// cluster, node and namespace APIs. Current resource counts include only
// non-deleting Pending/Running Pods; terminated objects are history only.
type kubernetesPodClassification struct {
	Active     bool
	Running    bool
	Pending    bool
	Problem    bool
	Terminated bool
	Evicted    bool
	Restarted  bool
	OOMKilled  bool
	CrashLoop  bool
}

type kubernetesPodAggregate struct {
	AllPods              []corev1.Pod
	ActivePods           []corev1.Pod
	TerminatedPods       []corev1.Pod
	EvictedPods          []corev1.Pod
	ProblemPods          []corev1.Pod
	RestartHistoryPods   []corev1.Pod
	CurrentOOMKilledPods []corev1.Pod
	Running              int
	Pending              int
	Restarted            int
	OOMKilled            int
	UnscheduledPending   int
}

func classifyKubernetesPod(pod *corev1.Pod) kubernetesPodClassification {
	deleting := pod.DeletionTimestamp != nil
	terminalReason := pod.Status.Reason == "Evicted" || pod.Status.Reason == "Completed"
	active := !deleting && !terminalReason && (pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodUnknown)
	result := kubernetesPodClassification{
		Active:     active,
		Running:    active && pod.Status.Phase == corev1.PodRunning,
		Pending:    active && pod.Status.Phase == corev1.PodPending,
		Terminated: deleting || terminalReason || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed,
		Evicted:    pod.Status.Reason == "Evicted",
	}
	if !active {
		return result
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionFalse {
			result.Problem = true
			break
		}
	}
	statuses := append(append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...), pod.Status.EphemeralContainerStatuses...)
	for _, status := range statuses {
		if status.RestartCount > 0 {
			result.Restarted = true
		}
		if status.State.Waiting != nil {
			if _, actionable := kubernetesActionablePodWaitingReasons[status.State.Waiting.Reason]; actionable {
				result.Problem = true
			}
			if status.State.Waiting.Reason == "CrashLoopBackOff" {
				result.CrashLoop = true
			}
		}
		if status.State.Terminated != nil && status.State.Terminated.Reason == "OOMKilled" {
			result.OOMKilled = true
			result.Problem = true
		}
	}
	return result
}

func aggregateKubernetesPods(pods []corev1.Pod) kubernetesPodAggregate {
	return aggregateKubernetesPodsInScope(pods, kubernetesPodScope{})
}

func aggregateKubernetesPodsInScope(pods []corev1.Pod, scope kubernetesPodScope) kubernetesPodAggregate {
	result := kubernetesPodAggregate{
		AllPods:              make([]corev1.Pod, 0, len(pods)),
		ActivePods:           make([]corev1.Pod, 0, len(pods)),
		TerminatedPods:       make([]corev1.Pod, 0),
		EvictedPods:          make([]corev1.Pod, 0),
		ProblemPods:          make([]corev1.Pod, 0),
		RestartHistoryPods:   make([]corev1.Pod, 0),
		CurrentOOMKilledPods: make([]corev1.Pod, 0),
	}
	seen := make(map[string]struct{}, len(pods))
	for i := range pods {
		pod := &pods[i]
		if scope.NodeName != "" && pod.Spec.NodeName != scope.NodeName {
			continue
		}
		if scope.Namespace != "" && pod.Namespace != scope.Namespace {
			continue
		}
		if scope.OwnerUID != "" && (scope.OwnerUIDForPod == nil || scope.OwnerUIDForPod(pod) != scope.OwnerUID) {
			continue
		}
		if scope.Predicate != nil && !scope.Predicate(pod) {
			continue
		}
		identity := string(pod.UID)
		if identity == "" {
			identity = strings.Join([]string{pod.Namespace, pod.Name}, "/")
		}
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		result.AllPods = append(result.AllPods, *pod)
		classification := classifyKubernetesPod(pod)
		if classification.Terminated {
			result.TerminatedPods = append(result.TerminatedPods, *pod)
		}
		if classification.Evicted {
			result.EvictedPods = append(result.EvictedPods, *pod)
		}
		if !classification.Active {
			continue
		}
		result.ActivePods = append(result.ActivePods, *pod)
		if classification.Running {
			result.Running++
		}
		if classification.Pending {
			result.Pending++
			if pod.Spec.NodeName == "" {
				result.UnscheduledPending++
			}
		}
		if classification.Problem {
			result.ProblemPods = append(result.ProblemPods, *pod)
		}
		if classification.Restarted {
			result.Restarted++
			result.RestartHistoryPods = append(result.RestartHistoryPods, *pod)
		}
		if classification.OOMKilled {
			result.OOMKilled++
			result.CurrentOOMKilledPods = append(result.CurrentOOMKilledPods, *pod)
		}
	}
	return result
}
