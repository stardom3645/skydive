/*
 * Copyright (C) 2018 IBM, Inc.
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
	"fmt"
	"time"

	"github.com/skydive-project/skydive/graffiti/graph"
	"github.com/skydive-project/skydive/probe"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type persistentVolumeClaimHandler struct {
}

func (h *persistentVolumeClaimHandler) Dump(obj interface{}) string {
	pvc := obj.(*v1.PersistentVolumeClaim)
	return fmt.Sprintf("persistentvolumeclaim{Name: %s}", pvc.GetName())
}

func (h *persistentVolumeClaimHandler) Map(obj interface{}) (graph.Identifier, graph.Metadata) {
	pvc := obj.(*v1.PersistentVolumeClaim)

	m := NewMetadataFields(&pvc.ObjectMeta)
	m.SetFieldAndNormalize("AccessModes", pvc.Spec.AccessModes)
	m.SetFieldAndNormalize("VolumeName", pvc.Spec.VolumeName)
	m.SetFieldAndNormalize("StorageClassName", pvc.Spec.StorageClassName)
	m.SetFieldAndNormalize("VolumeMode", pvc.Spec.VolumeMode)
	m.SetFieldAndNormalize("Status", pvc.Status.Phase)
	m.SetFieldAndNormalize("accessModes", pvc.Spec.AccessModes)
	if pvc.Spec.VolumeName != "" {
		m.SetField("volumeName", pvc.Spec.VolumeName)
	}
	if pvc.Spec.VolumeMode != nil {
		m.SetField("volumeMode", string(*pvc.Spec.VolumeMode))
	}
	if pvc.Spec.StorageClassName != nil {
		m.SetField("storageClassName", *pvc.Spec.StorageClassName)
	}
	if !pvc.CreationTimestamp.IsZero() {
		m.SetField("CreationTimestamp", pvc.CreationTimestamp.Time.UTC().Format(time.RFC3339Nano))
	}
	if requested, found := pvc.Spec.Resources.Requests[v1.ResourceStorage]; found {
		value := requested.String()
		m.SetField("RequestedCapacity", value)
		m.SetField("requestStorage", value)
	}
	if capacity, found := pvc.Status.Capacity[v1.ResourceStorage]; found {
		value := capacity.String()
		m.SetField("StatusCapacity", value)
		m.SetField("actualCapacity", value)
	}

	metadata := NewMetadata(Manager, "persistentvolumeclaim", m, pvc, pvc.Name)
	SetState(&metadata, pvc.Status.Phase == "Bound")

	return graph.Identifier(pvc.GetUID()), metadata
}

func newPersistentVolumeClaimProbe(client interface{}, g *graph.Graph) Subprobe {
	return NewResourceCache(client.(*kubernetes.Clientset).CoreV1().RESTClient(), &v1.PersistentVolumeClaim{}, "persistentvolumeclaims", g, &persistentVolumeClaimHandler{})
}

func pvPVCAreLinked(a, b interface{}) bool {
	pvc := a.(*v1.PersistentVolumeClaim)
	pv := b.(*v1.PersistentVolume)
	return pvc.Spec.VolumeName == pv.Name
}

func newPVPVCLinker(manager string) LinkHandler {
	return func(g *graph.Graph) probe.Handler {
		return NewABLinker(g, manager, "persistentvolumeclaim", manager, "persistentvolume", pvPVCAreLinked)
	}
}
