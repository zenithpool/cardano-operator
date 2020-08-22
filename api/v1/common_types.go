package v1

import (
	v1 "k8s.io/api/core/v1"
)

// NodeSpec ...
type NodeSpec struct {
	Replicas            int32                        `json:"replicas"`
	ImagePullSecrets    []v1.LocalObjectReference    `json:"imagePullSecrets,omitempty"`
	Image               string                       `json:"image,omitempty"`
	Storage             v1.PersistentVolumeClaimSpec `json:"storage"`
	Service             NodeServiceSpec              `json:"service,omitempty"`
	Resources           v1.ResourceRequirements      `json:"resources,omitempty"`
	ConfigurationConfig v1.LocalObjectReference      `json:"configuration,omitempty"`
	TopologyConfig      v1.LocalObjectReference      `json:"topology,omitempty"`
}

// NodeServiceSpec ...
type NodeServiceSpec struct {
	Annotations map[string]string `json:"annotations,omitempty"`
	Type        v1.ServiceType    `json:"type,omitempty"`
	Port        int32             `json:"port,omitempty"`
}
