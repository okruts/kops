/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"github.com/golang/glog"
	"k8s.io/kops/protokube/pkg/protokube"
	"k8s.io/kops/protokube/pkg/protokube/baremetal"
	"os"
	"strings"
)

func main() {
	master := false
	flag.BoolVar(&master, "master", master, "Act as master")

	populateExternalIP := false
	flag.BoolVar(&populateExternalIP, "populate-external-ip", populateExternalIP, "If set, will populate the external IP when starting up")

	cloud := ""
	flag.StringVar(&cloud, "cloud", cloud, "Cloud provider to use - gce, aws, baremetal")

	containerized := false
	flag.BoolVar(&containerized, "containerized", containerized, "Set if we are running containerized.")

	dnsZoneName := ""
	flag.StringVar(&dnsZoneName, "dns-zone-name", dnsZoneName, "Name of zone to use for DNS")

	dnsInternalSuffix := ""
	flag.StringVar(&dnsInternalSuffix, "dns-internal-suffix", dnsInternalSuffix, "DNS suffix for internal domain names")

	clusterID := ""
	flag.StringVar(&clusterID, "cluster-id", clusterID, "Cluster ID")

	flagChannels := ""
	flag.StringVar(&flagChannels, "channels", flagChannels, "channels to install")

	flag.Set("logtostderr", "true")
	flag.Parse()

	var volumes protokube.Volumes
	var err error

	switch cloud {
	case "aws":
		volumes, err = protokube.NewAWSVolumes()
		if err != nil {
			glog.Errorf("Error initializing AWS: %q", err)
			os.Exit(1)
		}
	case "baremetal":
		basedir := "/volumes"
		volumes, err = baremetal.NewVolumes(basedir)
		if err != nil {
			glog.Errorf("Error initializing AWS: %q", err)
			os.Exit(1)
		}
	default:
		glog.Errorf("unknown cloud: %v", cloud)
		os.Exit(1)
	}

	if clusterID == "" {
		clusterID = volumes.ClusterID()
		if clusterID == "" {
			glog.Errorf("cluster-id is required (cannot be determined from cloud)")
			os.Exit(1)
		} else {
			glog.Infof("Setting cluster-id from cloud: %s", clusterID)
		}
	}

	if dnsInternalSuffix == "" {
		// TODO: Maybe only master needs DNS?
		dnsInternalSuffix = ".internal." + clusterID
		glog.Infof("Setting dns-internal-suffix to %q", dnsInternalSuffix)
	}

	// Make sure it's actually a suffix (starts with .)
	if !strings.HasPrefix(dnsInternalSuffix, ".") {
		dnsInternalSuffix = "." + dnsInternalSuffix
	}

	if dnsZoneName == "" {
		tokens := strings.Split(dnsInternalSuffix, ".")
		dnsZoneName = strings.Join(tokens[len(tokens)-2:], ".")
	}

	// Get internal IP from cloud, to avoid problems if we're in a container
	// TODO: Just run with --net=host ??
	//internalIP, err := findInternalIP()
	//if err != nil {
	//	glog.Errorf("Error finding internal IP: %q", err)
	//	os.Exit(1)
	//}

	internalIP, err := protokube.FindInternalIP()
	if err != nil {
		glog.Errorf("Error finding internal IP: %q", err)
		os.Exit(1)
	}

	dns, err := protokube.NewRoute53DNSProvider(dnsZoneName)
	if err != nil {
		glog.Errorf("Error initializing DNS: %q", err)
		os.Exit(1)
	}

	rootfs := "/"
	if containerized {
		rootfs = "/rootfs/"
	}
	protokube.RootFS = rootfs
	protokube.Containerized = containerized

	modelDir := "model/etcd"

	var channels []string
	if flagChannels != "" {
		channels = strings.Split(flagChannels, ",")
	}

	k := &protokube.KubeBoot{
		Master:            master,
		InternalDNSSuffix: dnsInternalSuffix,
		InternalIP:        internalIP,
		//MasterID          : fromVolume
		//EtcdClusters   : fromVolume

		PopulateExternalIP: populateExternalIP,

		ModelDir: modelDir,
		DNS:      dns,

		Channels: channels,

		Kubernetes: protokube.NewKubernetesContext(),
	}
	k.Init(volumes)

	k.RunSyncLoop()

	glog.Infof("Unexpected exit")
	os.Exit(1)
}
