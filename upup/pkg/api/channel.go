package api

import (
	"fmt"
	"k8s.io/kops/util/pkg/vfs"
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

// LoadChannel loads a Channel object from the specified VFS location
func LoadChannel(location string) (*Channel, error) {
	channel := &Channel{}
	channelBytes, err := vfs.Context.ReadFile(location)
	if err != nil {
		return nil, fmt.Errorf("error reading channel %q: %v", location, err)
	}
	err = ParseYaml(channelBytes, channel)
	if err != nil {
		return nil, fmt.Errorf("error parsing channel %q: %v", location, err)
	}
	return channel, nil
}
