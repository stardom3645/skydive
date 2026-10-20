package server

import (
	"fmt"
	discoveryv1 "k8s.io/api/discovery/v1"
)

// Kubernetes defines an unspecified EndpointSlice Ready condition as ready.
func kubernetesEndpointReady(ready *bool) bool { return ready == nil || *ready }

func kubernetesEndpointIdentity(item kubernetesEndpointDetail) string {
	if item.TargetUID != "" {
		return "uid:" + item.TargetUID
	}
	if item.PodUID != "" {
		return "uid:" + item.PodUID
	}
	return "address:" + item.Address
}

func mergeKubernetesEndpointConditions(previous *kubernetesEndpointDetail, item kubernetesEndpointDetail) {
	ready := kubernetesEndpointReady(previous.Ready) && kubernetesEndpointReady(item.Ready)
	previous.Ready = &ready
	serving := previous.Serving != nil && *previous.Serving && item.Serving != nil && *item.Serving
	previous.Serving = &serving
	terminating := (previous.Terminating != nil && *previous.Terminating) || (item.Terminating != nil && *item.Terminating)
	previous.Terminating = &terminating
	if previous.NodeName != item.NodeName {
		previous.NodeName = ""
	}
}

// Preserve each distinct address and all ports in the detail list without
// rendering duplicate rows when EndpointSlices overlap during reconciliation.
func kubernetesUniqueEndpointRows(items []kubernetesEndpointDetail) []kubernetesEndpointDetail {
	result := make([]kubernetesEndpointDetail, 0, len(items))
	indices := make(map[string]int)
	for _, item := range items {
		key := kubernetesEndpointIdentity(item) + ":" + item.Address
		if index, found := indices[key]; found {
			previous := &result[index]
			mergeKubernetesEndpointConditions(previous, item)
			ports := make(map[string]bool)
			portKey := func(port kubernetesEndpointPort) string {
				var number int32
				if port.Port != nil {
					number = *port.Port
				}
				return fmt.Sprintf("%s:%d:%s", port.Name, number, port.Protocol)
			}
			for _, port := range previous.Ports {
				ports[portKey(port)] = true
			}
			for _, port := range item.Ports {
				if !ports[portKey(port)] {
					previous.Ports = append(previous.Ports, port)
					ports[portKey(port)] = true
				}
			}
			continue
		}
		indices[key] = len(result)
		result = append(result, item)
	}
	return result
}

// Count serving targets rather than duplicate slices, ports or two IP families
// of the same UID. Address rows remain intact for the existing detail view.
// Conflicting duplicate readiness is treated conservatively as not ready.
func kubernetesLogicalEndpoints(items []kubernetesEndpointDetail) []kubernetesEndpointDetail {
	result := make([]kubernetesEndpointDetail, 0)
	indices := make(map[string]int)
	for _, item := range items {
		key := kubernetesEndpointIdentity(item)
		if previous, found := indices[key]; found {
			mergeKubernetesEndpointConditions(&result[previous], item)
			continue
		}
		indices[key] = len(result)
		result = append(result, item)
	}
	return result
}

func kubernetesSliceEndpoints(slices []discoveryv1.EndpointSlice) []kubernetesEndpointDetail {
	items := make([]kubernetesEndpointDetail, 0)
	for _, slice := range slices {
		for _, endpoint := range slice.Endpoints {
			item := kubernetesEndpointDetail{Ready: endpoint.Conditions.Ready}
			if endpoint.NodeName != nil {
				item.NodeName = *endpoint.NodeName
			}
			if endpoint.TargetRef != nil {
				item.TargetKind = endpoint.TargetRef.Kind
				item.TargetUID = string(endpoint.TargetRef.UID)
			}
			for _, address := range endpoint.Addresses {
				copy := item
				copy.Address = address
				items = append(items, copy)
			}
			// A UID still identifies a target when an incomplete fixture/source omits addresses.
			if len(endpoint.Addresses) == 0 && item.TargetUID != "" {
				items = append(items, item)
			}
		}
	}
	return kubernetesLogicalEndpoints(items)
}

func kubernetesReadyEndpointsOnSingleNode(items []kubernetesEndpointDetail) bool {
	nodes := make(map[string]bool)
	for _, item := range kubernetesLogicalEndpoints(items) {
		if !kubernetesEndpointReady(item.Ready) {
			continue
		}
		if item.NodeName == "" {
			return false
		}
		nodes[item.NodeName] = true
	}
	return len(nodes) == 1
}

func applyKubernetesServiceEndpointStatus(detail *kubernetesServiceDetail) {
	detail.Endpoints = kubernetesUniqueEndpointRows(detail.Endpoints)
	detail.EndpointCount, detail.ReadyEndpointCount, detail.NotReadyEndpointCount = 0, 0, 0
	detail.ServingEndpointCount, detail.TerminatingEndpointCount = 0, 0
	for _, item := range kubernetesLogicalEndpoints(detail.Endpoints) {
		detail.EndpointCount++
		if kubernetesEndpointReady(item.Ready) {
			detail.ReadyEndpointCount++
		} else {
			detail.NotReadyEndpointCount++
		}
		if item.Serving != nil && *item.Serving {
			detail.ServingEndpointCount++
		}
		if item.Terminating != nil && *item.Terminating {
			detail.TerminatingEndpointCount++
		}
	}
	applicable := detail.EndpointDataAvailable && detail.Type != "ExternalName"
	detail.NoReadyEndpoints = applicable && detail.ReadyEndpointCount == 0
	detail.SingleEndpoint = applicable && detail.ReadyEndpointCount == 1
	detail.SingleNodeConcentration = applicable && detail.ReadyEndpointCount > 1 && kubernetesReadyEndpointsOnSingleNode(detail.Endpoints)
}
