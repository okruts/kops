package api

import (
	k8sapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
)

type Channel struct {
	unversioned.TypeMeta `json:",inline"`
	k8sapi.ObjectMeta    `json:"metadata,omitempty"`

	Spec ChannelSpec `json:"spec,omitempty"`
}

type ChannelSpec struct {
	Images []*ChannelImageSpec `json:"images,omitempty"`

	Cluster *ClusterSpec `json:"cluster,omitempty"`
}

const (
	ImageLabelCloudprovider = "k8s.io/cloudprovider"
)

type ChannelImageSpec struct {
	Labels map[string]string `json:"labels,omitempty"`

	ProviderID string `json:"providerID,omitempty"`

	Name string `json:"name,omitempty"`
}
