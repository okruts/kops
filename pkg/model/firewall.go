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

package model

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/kops/pkg/apis/kops"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kops/upup/pkg/fi/cloudup/awstasks"
	"strconv"
)

type Protocol int

const (
	ProtocolIPIP Protocol = 4
)

// FirewallModelBuilder configures firewall network objects
type FirewallModelBuilder struct {
	*KopsModelContext
}

var _ fi.ModelBuilder = &FirewallModelBuilder{}

func (b *FirewallModelBuilder) Build(c *fi.ModelBuilderContext) error {
	if err := b.buildNodeRules(c); err != nil {
		return err
	}
	if err := b.buildMasterRules(c); err != nil {
		return err
	}
	return nil
}

func (b *FirewallModelBuilder) buildNodeRules(c *fi.ModelBuilderContext) error {
	name := "nodes." + b.ClusterName()

	{
		t := &awstasks.SecurityGroup{
			Name:             s(name),
			VPC:              b.LinkToVPC(),
			Description:      s("Security group for nodes"),
			RemoveExtraRules: []string{"port=22"},
		}
		c.AddTask(t)
	}

	// Allow full egress
	{
		t := &awstasks.SecurityGroupRule{
			Name:          s("node-egress"),
			SecurityGroup: b.LinkToSecurityGroup(kops.InstanceGroupRoleNode),
			Egress:        fi.Bool(true),
			CIDR:          s("0.0.0.0/0"),
		}
		c.AddTask(t)
	}

	// Nodes can talk to nodes
	{
		t := &awstasks.SecurityGroupRule{
			Name:          s("all-node-to-node"),
			SecurityGroup: b.LinkToSecurityGroup(kops.InstanceGroupRoleNode),
			SourceGroup:   b.LinkToSecurityGroup(kops.InstanceGroupRoleNode),
		}
		c.AddTask(t)
	}

	// TODO: We need to remove the ALL rule
	//W1229 12:32:22.300132    9003 executor.go:109] error running task "SecurityGroupRule/node-to-master-443" (9m58s remaining to succeed): error creating SecurityGroupIngress: InvalidPermission.Duplicate: the specified rule "peer: sg-f6b1a68b, ALL, ALLOW" already exists
	//status code: 400, request id: 6a69627f-9a26-4bd0-b294-a9a96f89bc46

	udpPorts := []int64{}
	tcpPorts := []int64{}
	protocols := []Protocol{}

	// allow access to API
	tcpPorts = append(tcpPorts, 443)

	// allow cadvisor
	tcpPorts = append(tcpPorts, 4194)

	if b.Cluster.Spec.Networking != nil {
		if b.Cluster.Spec.Networking.Kopeio != nil {
			// VXLAN over UDP
			udpPorts = append(udpPorts, 4789)
		}

		if b.Cluster.Spec.Networking.Weave != nil {
			udpPorts = append(udpPorts, 6783)
			tcpPorts = append(tcpPorts, 6783)
			udpPorts = append(udpPorts, 6784)
		}

		if b.Cluster.Spec.Networking.Flannel != nil {
			udpPorts = append(udpPorts, 8285)
		}

		if b.Cluster.Spec.Networking.Calico != nil {
			// Calico needs to access etcd
			// TODO: Remove, replace with etcd in calico manifest
			glog.Warningf("Opening etcd port on masters for access from the nodes, for calico.  This is unsafe in untrusted environments.")
			tcpPorts = append(tcpPorts, 4001)

			tcpPorts = append(tcpPorts, 179)
			protocols = append(protocols, ProtocolIPIP)
		}
	}

	for _, udpPort := range udpPorts {
		c.AddTask(&awstasks.SecurityGroupRule{
			Name:          s(fmt.Sprintf("node-to-master-udp-%d", udpPort)),
			SecurityGroup: b.LinkToSecurityGroup(kops.InstanceGroupRoleMaster),
			SourceGroup:   b.LinkToSecurityGroup(kops.InstanceGroupRoleNode),
			FromPort:      i64(udpPort),
			ToPort:        i64(udpPort),
			Protocol:      s("udp"),
		})
	}
	for _, tcpPort := range tcpPorts {
		c.AddTask(&awstasks.SecurityGroupRule{
			Name:          s(fmt.Sprintf("node-to-master-tcp-%d", tcpPort)),
			SecurityGroup: b.LinkToSecurityGroup(kops.InstanceGroupRoleMaster),
			SourceGroup:   b.LinkToSecurityGroup(kops.InstanceGroupRoleNode),
			FromPort:      i64(tcpPort),
			ToPort:        i64(tcpPort),
			Protocol:      s("tcp"),
		})
	}
	for _, protocol := range protocols {
		awsName := strconv.Itoa(int(protocol))
		name := awsName
		switch protocol {
		case ProtocolIPIP:
			name = "ipip"
		default:
			glog.Warningf("unknown protocol %q - naming by number", awsName)
		}

		c.AddTask(&awstasks.SecurityGroupRule{
			Name:          s("node-to-master-protocol-" + name),
			SecurityGroup: b.LinkToSecurityGroup(kops.InstanceGroupRoleMaster),
			SourceGroup:   b.LinkToSecurityGroup(kops.InstanceGroupRoleNode),
			Protocol:      s(awsName),
		})
	}

	return nil
}

func (b *FirewallModelBuilder) buildMasterRules(c *fi.ModelBuilderContext) error {
	name := "masters." + b.ClusterName()

	{
		t := &awstasks.SecurityGroup{
			Name:        s(name),
			VPC:         b.LinkToVPC(),
			Description: s("Security group for masters"),
			RemoveExtraRules: []string{
				"port=22",   // SSH
				"port=443",  // k8s api
				"port=4001", // etcd main (etcd events is 4002)
				"port=4789", // VXLAN
				"port=179",  // Calico

				// TODO: UDP vs TCP
				// TODO: Protocol 4 for calico
			},
		}
		c.AddTask(t)
	}

	// Allow full egress
	{
		t := &awstasks.SecurityGroupRule{
			Name:          s("master-egress"),
			SecurityGroup: b.LinkToSecurityGroup(kops.InstanceGroupRoleMaster),
			Egress:        fi.Bool(true),
			CIDR:          s("0.0.0.0/0"),
		}
		c.AddTask(t)
	}

	// Masters can talk to masters
	{
		t := &awstasks.SecurityGroupRule{
			Name:          s("all-master-to-master"),
			SecurityGroup: b.LinkToSecurityGroup(kops.InstanceGroupRoleMaster),
			SourceGroup:   b.LinkToSecurityGroup(kops.InstanceGroupRoleMaster),
		}
		c.AddTask(t)
	}

	// Masters can talk to nodes
	{
		t := &awstasks.SecurityGroupRule{
			Name:          s("all-master-to-node"),
			SecurityGroup: b.LinkToSecurityGroup(kops.InstanceGroupRoleNode),
			SourceGroup:   b.LinkToSecurityGroup(kops.InstanceGroupRoleMaster),
		}
		c.AddTask(t)
	}

	return nil
}
