package server

import corev1 "k8s.io/api/core/v1"

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
	ActivePods     []corev1.Pod
	TerminatedPods []corev1.Pod
	ProblemPods    []corev1.Pod
	Running        int
	Pending        int
	Restarted      int
	OOMKilled      int
}

func classifyKubernetesPod(pod *corev1.Pod) kubernetesPodClassification {
	deleting := pod.DeletionTimestamp != nil
	active := !deleting && (pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning)
	result := kubernetesPodClassification{
		Active:     active,
		Running:    active && pod.Status.Phase == corev1.PodRunning,
		Pending:    active && pod.Status.Phase == corev1.PodPending,
		Terminated: deleting || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed,
		Evicted:    pod.Status.Phase == corev1.PodFailed && pod.Status.Reason == "Evicted",
	}
	if !active {
		return result
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status != corev1.ConditionTrue {
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
			switch status.State.Waiting.Reason {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError", "CreateContainerError", "RunContainerError":
				result.Problem = true
			}
			if status.State.Waiting.Reason == "CrashLoopBackOff" {
				result.CrashLoop = true
			}
		}
		if (status.State.Terminated != nil && status.State.Terminated.Reason == "OOMKilled") ||
			(status.LastTerminationState.Terminated != nil && status.LastTerminationState.Terminated.Reason == "OOMKilled") {
			result.OOMKilled = true
			result.Problem = true
		}
	}
	return result
}

func aggregateKubernetesPods(pods []corev1.Pod) kubernetesPodAggregate {
	result := kubernetesPodAggregate{
		ActivePods:     make([]corev1.Pod, 0, len(pods)),
		TerminatedPods: make([]corev1.Pod, 0),
		ProblemPods:    make([]corev1.Pod, 0),
	}
	for i := range pods {
		classification := classifyKubernetesPod(&pods[i])
		if classification.Terminated {
			result.TerminatedPods = append(result.TerminatedPods, pods[i])
		}
		if !classification.Active {
			continue
		}
		result.ActivePods = append(result.ActivePods, pods[i])
		if classification.Running {
			result.Running++
		}
		if classification.Pending {
			result.Pending++
		}
		if classification.Problem {
			result.ProblemPods = append(result.ProblemPods, pods[i])
		}
		if classification.Restarted {
			result.Restarted++
		}
		if classification.OOMKilled {
			result.OOMKilled++
		}
	}
	return result
}
