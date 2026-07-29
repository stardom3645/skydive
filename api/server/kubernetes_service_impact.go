package server

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
)

const (
	kubernetesServiceImpactNoReadyEndpoint = "NO_READY_ENDPOINT"
	kubernetesServiceImpactNoHealthyTarget = "NO_HEALTHY_TARGET"
)

// kubernetesServiceImpact is a current-state result. Historical/terminated Pods
// are intentionally absent because they cannot make a currently ready Service
// unavailable.
type kubernetesServiceImpact struct {
	Affected           bool
	Reason             string
	ReadyEndpoints     int
	CandidateEndpoints int
}

func kubernetesCurrentlyImpactedPodUIDs(aggregate kubernetesPodAggregate, notReadyNodes map[string]bool) map[string]bool {
	impacted := make(map[string]bool)
	for i := range aggregate.ActivePods {
		pod := &aggregate.ActivePods[i]
		classification := classifyKubernetesPod(pod)
		if classification.Problem || notReadyNodes[pod.Spec.NodeName] {
			impacted[string(pod.UID)] = true
		}
	}
	return impacted
}

// aggregateKubernetesServiceImpact fixes the Service contract used by cluster
// and node detail APIs:
//  1. a Ready endpoint backed by a non-impacted Pod keeps the Service healthy;
//  2. EndpointSlice data with no healthy Ready endpoint is current impact;
//  3. selector fallback is used only when EndpointSlice evidence is absent;
//  4. selector-less Services remain unevaluated rather than false-positive.
func aggregateKubernetesServiceImpact(
	services []corev1.Service,
	endpointSlices []discoveryv1.EndpointSlice,
	pods kubernetesPodAggregate,
	notReadyNodes map[string]bool,
) map[string]kubernetesServiceImpact {
	impactedPodUIDs := kubernetesCurrentlyImpactedPodUIDs(pods, notReadyNodes)
	slicesByService := make(map[string][]discoveryv1.EndpointSlice)
	for i := range endpointSlices {
		slice := endpointSlices[i]
		serviceName := slice.Labels[discoveryv1.LabelServiceName]
		if serviceName == "" {
			continue
		}
		key := slice.Namespace + "/" + serviceName
		slicesByService[key] = append(slicesByService[key], slice)
	}

	result := make(map[string]kubernetesServiceImpact)
	for i := range services {
		service := &services[i]
		key := service.Namespace + "/" + service.Name
		if slices, found := slicesByService[key]; found {
			impact := kubernetesServiceImpact{}
			for _, slice := range slices {
				for _, endpoint := range slice.Endpoints {
					impact.CandidateEndpoints++
					ready := endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready
					if !ready {
						continue
					}
					if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" {
						uid := string(endpoint.TargetRef.UID)
						if impactedPodUIDs[uid] {
							continue
						}
					}
					impact.ReadyEndpoints++
				}
			}
			impact.Affected = impact.ReadyEndpoints == 0
			if impact.Affected {
				impact.Reason = kubernetesServiceImpactNoReadyEndpoint
			}
			result[key] = impact
			continue
		}

		if len(service.Spec.Selector) == 0 {
			continue
		}
		impact := kubernetesServiceImpact{}
		for p := range pods.ActivePods {
			pod := &pods.ActivePods[p]
			if pod.Namespace != service.Namespace || !labelsMatch(service.Spec.Selector, pod.Labels) {
				continue
			}
			impact.CandidateEndpoints++
			classification := classifyKubernetesPod(pod)
			if classification.Running && !classification.Problem && !notReadyNodes[pod.Spec.NodeName] {
				impact.ReadyEndpoints++
			}
		}
		impact.Affected = impact.ReadyEndpoints == 0
		if impact.Affected {
			impact.Reason = kubernetesServiceImpactNoHealthyTarget
		}
		result[key] = impact
	}
	return result
}
