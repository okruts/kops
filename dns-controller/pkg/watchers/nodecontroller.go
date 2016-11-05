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

package watchers

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	"k8s.io/kops/dns-controller/pkg/dns"
	"k8s.io/kops/dns-controller/pkg/util"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/v1"
	client "k8s.io/kubernetes/pkg/client/clientset_generated/release_1_3/typed/core/v1"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/watch"
	"strings"
)

// NodeController watches for nodes
type NodeController struct {
	util.Stoppable
	kubeClient *client.CoreClient
	scope      dns.Scope
}

// newNodeController creates a nodeController
func NewNodeController(kubeClient *client.CoreClient, dns dns.Context) (*NodeController, error) {
	scope, err := dns.CreateScope("node")
	if err != nil {
		return nil, fmt.Errorf("error building dns scope: %v", err)
	}
	c := &NodeController{
		kubeClient: kubeClient,
		scope:      scope,
	}

	return c, nil
}

// Run starts the NodeController.
func (c *NodeController) Run() {
	glog.Infof("starting node controller")

	stopCh := c.StopChannel()
	go c.runWatcher(stopCh)

	<-stopCh
	glog.Infof("shutting down node controller")
}

func (c *NodeController) runWatcher(stopCh <-chan struct{}) {
	runOnce := func() (bool, error) {
		var listOpts api.ListOptions

		// Note we need to watch all the nodes, to set up alias targets
		listOpts.LabelSelector = labels.Everything()
		glog.Warningf("querying without field filter")
		listOpts.FieldSelector = fields.Everything()

		nodeList, err := c.kubeClient.Nodes().List(listOpts)
		if err != nil {
			return false, fmt.Errorf("error listing nodes: %v", err)
		}
		for i := range nodeList.Items {
			node := &nodeList.Items[i]
			glog.Infof("node: %v", node.Name)
			c.updateNodeRecords(node)
		}
		c.scope.MarkReady()

		// Note we need to watch all the nodes, to set up alias targets
		listOpts.LabelSelector = labels.Everything()
		glog.Warningf("querying without field filter")
		listOpts.FieldSelector = fields.Everything()

		listOpts.Watch = true
		listOpts.ResourceVersion = nodeList.ResourceVersion
		watcher, err := c.kubeClient.Nodes().Watch(listOpts)
		if err != nil {
			return false, fmt.Errorf("error watching nodes: %v", err)
		}
		ch := watcher.ResultChan()
		for {
			select {
			case <-stopCh:
				glog.Infof("Got stop signal")
				return true, nil
			case event, ok := <-ch:
				if !ok {
					glog.Infof("node watch channel closed")
					return false, nil
				}

				node := event.Object.(*v1.Node)
				glog.V(4).Infof("node changed: %s %v", event.Type, node.Name)

				switch event.Type {
				case watch.Added, watch.Modified:
					c.updateNodeRecords(node)

				case watch.Deleted:
					c.scope.Replace(node.Name, nil)
				}
			}
		}
	}

	for {
		stop, err := runOnce()
		if stop {
			return
		}

		if err != nil {
			glog.Warningf("Unexpected error in event watch, will retry: %v", err)
			time.Sleep(10 * time.Second)
		}
	}
}

func (c *NodeController) updateNodeRecords(node *v1.Node) {
	var records []dns.Record

	//dnsLabel := node.Labels[LabelNameDns]
	//if dnsLabel != "" {
	//	var ips []string
	//	for _, a := range node.Status.Addresses {
	//		if a.Type != v1.NodeExternalIP {
	//			continue
	//		}
	//		ips = append(ips, a.Address)
	//	}
	//	tokens := strings.Split(dnsLabel, ",")
	//	for _, token := range tokens {
	//		token = strings.TrimSpace(token)
	//
	//		// Assume a FQDN A record
	//		fqdn := token
	//		for _, ip := range ips {
	//			records = append(records, dns.Record{
	//				RecordType: dns.RecordTypeA,
	//				FQDN: fqdn,
	//				Value: ip,
	//			})
	//		}
	//	}
	//}
	//
	//dnsLabelInternal := node.Annotations[AnnotationNameDnsInternal]
	//if dnsLabelInternal != "" {
	//	var ips []string
	//	for _, a := range node.Status.Addresses {
	//		if a.Type != v1.NodeInternalIP {
	//			continue
	//		}
	//		ips = append(ips, a.Address)
	//	}
	//	tokens := strings.Split(dnsLabelInternal, ",")
	//	for _, token := range tokens {
	//		token = strings.TrimSpace(token)
	//
	//		// Assume a FQDN A record
	//		fqdn := dns.EnsureDotSuffix(token)
	//		for _, ip := range ips {
	//			records = append(records, dns.Record{
	//				RecordType: dns.RecordTypeA,
	//				FQDN: fqdn,
	//				Value: ip,
	//			})
	//		}
	//	}
	//}

	// Alias targets

	// node/<name>/internal -> InternalIP
	for _, a := range node.Status.Addresses {
		if a.Type != v1.NodeInternalIP {
			continue
		}
		records = append(records, dns.Record{
			RecordType:  dns.RecordTypeA,
			FQDN:        "node/" + node.Name + "/internal",
			Value:       a.Address,
			AliasTarget: true,
		})
	}

	// node/<name>/external -> ExternalIP
	{
		var externalIPs []string

		// If the external ip annotation is explicitly set, use that
		if node.Annotations[AnnotationNameExternalIP] != "" {
			externalIPs = strings.Split(node.Annotations[AnnotationNameExternalIP], ",")
		} else {
			for _, a := range node.Status.Addresses {
				if a.Type != v1.NodeExternalIP {
					continue
				}
				externalIPs = append(externalIPs, a.Address)
			}
		}

		glog.V(4).Infof("Node %s has external ips: %s", node.Name, externalIPs)

		for _, externalIP := range externalIPs {
			records = append(records, dns.Record{
				RecordType:  dns.RecordTypeA,
				FQDN:        "node/" + node.Name + "/external",
				Value:       externalIP,
				AliasTarget: true,
			})
		}
	}

	c.scope.Replace(node.Name, records)
}
