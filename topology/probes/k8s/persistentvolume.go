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

	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

type persistentVolumeHandler struct {
}

func persistentVolumeSourceDetails(pv *v1.PersistentVolume) (sourceType, driver, path, volumeHandle string) {
	source := pv.Spec.PersistentVolumeSource
	switch {
	case source.CSI != nil:
		return "CSI", source.CSI.Driver, "", source.CSI.VolumeHandle
	case source.Local != nil:
		return "Local", "", source.Local.Path, ""
	case source.HostPath != nil:
		return "HostPath", "", source.HostPath.Path, ""
	case source.NFS != nil:
		return "NFS", "", source.NFS.Path, ""
	case source.RBD != nil:
		return "RBD", "", source.RBD.RBDImage, ""
	case source.CephFS != nil:
		return "CephFS", "", source.CephFS.Path, ""
	case source.ISCSI != nil:
		return "iSCSI", "", source.ISCSI.TargetPortal, ""
	case source.AWSElasticBlockStore != nil:
		return "AWSElasticBlockStore", "", "", source.AWSElasticBlockStore.VolumeID
	case source.GCEPersistentDisk != nil:
		return "GCEPersistentDisk", "", "", source.GCEPersistentDisk.PDName
	case source.AzureDisk != nil:
		return "AzureDisk", "", "", source.AzureDisk.DataDiskURI
	}
	return "", "", "", ""
}

func (h *persistentVolumeHandler) Dump(obj interface{}) string {
	pv := obj.(*v1.PersistentVolume)
	return fmt.Sprintf("persistentvolume{Name: %s}", pv.Name)
}

func (h *persistentVolumeHandler) Map(obj interface{}) (graph.Identifier, graph.Metadata) {
	pv := obj.(*v1.PersistentVolume)

	m := NewMetadataFields(&pv.ObjectMeta)
	if capacity, found := pv.Spec.Capacity[v1.ResourceStorage]; found {
		value := capacity.String()
		m.SetField("Capacity", map[string]string{"storage": value})
		m.SetField("capacity", value)
	}
	if !pv.CreationTimestamp.IsZero() {
		m.SetField("CreationTimestamp", pv.CreationTimestamp.Time.UTC().Format(time.RFC3339Nano))
	}
	m.SetFieldAndNormalize("VolumeMode", pv.Spec.VolumeMode)
	m.SetFieldAndNormalize("StorageClassName", pv.Spec.StorageClassName)
	m.SetFieldAndNormalize("Status", pv.Status.Phase)
	m.SetFieldAndNormalize("AccessModes", pv.Spec.AccessModes)
	m.SetFieldAndNormalize("ReclaimPolicy", pv.Spec.PersistentVolumeReclaimPolicy)
	m.SetFieldAndNormalize("NodeAffinity", pv.Spec.NodeAffinity)
	m.SetFieldAndNormalize("accessModes", pv.Spec.AccessModes)
	if pv.Spec.VolumeMode != nil {
		m.SetField("volumeMode", string(*pv.Spec.VolumeMode))
	}
	if pv.Spec.StorageClassName != "" {
		m.SetField("storageClassName", pv.Spec.StorageClassName)
	}
	m.SetField("reclaimPolicy", string(pv.Spec.PersistentVolumeReclaimPolicy))
	sourceType, driver, path, volumeHandle := persistentVolumeSourceDetails(pv)
	if sourceType != "" {
		m.SetField("volumeSourceType", sourceType)
	}
	if driver != "" {
		m.SetField("driver", driver)
	}
	if path != "" {
		m.SetField("path", path)
	}
	if volumeHandle != "" {
		m.SetField("volumeHandle", volumeHandle)
	}
	if pv.Spec.ClaimRef != nil {
		m.SetFieldAndNormalize("ClaimRef", pv.Spec.ClaimRef.Name)
		m.SetFieldAndNormalize("ClaimNamespace", pv.Spec.ClaimRef.Namespace)
	}

	metadata := NewMetadata(Manager, "persistentvolume", m, pv, pv.Name)
	SetState(&metadata, pv.Status.Phase != "Failed")

	return graph.Identifier(pv.GetUID()), metadata
}

func pvNodeAreLinked(a, b interface{}) bool {
	pv := a.(*v1.PersistentVolume)
	node := b.(*v1.Node)
	if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
		return false
	}
	for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
		if len(term.MatchExpressions) == 0 && len(term.MatchFields) == 0 {
			continue
		}
		matches := true
		for _, expression := range term.MatchExpressions {
			value := node.Labels[expression.Key]
			switch expression.Operator {
			case v1.NodeSelectorOpIn:
				found := false
				for _, expected := range expression.Values {
					if value == expected {
						found = true
						break
					}
				}
				matches = matches && found
			case v1.NodeSelectorOpNotIn:
				_, exists := node.Labels[expression.Key]
				matches = matches && exists
				for _, blocked := range expression.Values {
					if value == blocked {
						matches = false
					}
				}
			case v1.NodeSelectorOpExists:
				_, exists := node.Labels[expression.Key]
				matches = matches && exists
			case v1.NodeSelectorOpDoesNotExist:
				_, exists := node.Labels[expression.Key]
				matches = matches && !exists
			default:
				matches = false
			}
		}
		for _, field := range term.MatchFields {
			if field.Key != "metadata.name" {
				matches = false
				continue
			}
			found := false
			for _, expected := range field.Values {
				if node.Name == expected {
					found = true
					break
				}
			}
			switch field.Operator {
			case v1.NodeSelectorOpIn:
				matches = matches && found
			case v1.NodeSelectorOpNotIn:
				matches = matches && !found
			default:
				matches = false
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func newPVNodeLinker(manager string) LinkHandler {
	return func(g *graph.Graph) probe.Handler {
		return NewABLinker(g, manager, "persistentvolume", manager, "node", pvNodeAreLinked)
	}
}

func newPersistentVolumeProbe(client interface{}, g *graph.Graph) Subprobe {
	return NewResourceCache(client.(*kubernetes.Clientset).CoreV1().RESTClient(), &v1.PersistentVolume{}, "persistentvolumes", g, &persistentVolumeHandler{})
}
