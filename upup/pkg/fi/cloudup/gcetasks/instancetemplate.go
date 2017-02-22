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

package gcetasks

import (
	"fmt"
	"github.com/golang/glog"
	compute "google.golang.org/api/compute/v0.beta"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kops/upup/pkg/fi/cloudup/gce"
	"k8s.io/kops/upup/pkg/fi/cloudup/terraform"
	"strings"
)

// InstanceTemplate represents a GCE InstanceTemplate
//go:generate fitask -type=InstanceTemplate
type InstanceTemplate struct {
	Name    *string
	Network *Network
	Tags    []string
	//Labels      map[string]string
	Preemptible *bool

	BootDiskImage  *string
	BootDiskSizeGB *int64
	BootDiskType   *string

	CanIPForward *bool
	Subnet       *Subnet

	Scopes []string

	Metadata    map[string]*fi.ResourceHolder
	MachineType *string
}

var _ fi.CompareWithID = &InstanceTemplate{}

func (e *InstanceTemplate) CompareWithID() *string {
	return e.Name
}

func (e *InstanceTemplate) Find(c *fi.Context) (*InstanceTemplate, error) {
	cloud := c.Cloud.(*gce.GCECloud)

	r, err := cloud.Compute.InstanceTemplates.Get(cloud.Project, *e.Name).Do()
	if err != nil {
		if gce.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("error listing InstanceTemplates: %v", err)
	}

	actual := &InstanceTemplate{}
	actual.Name = &r.Name

	p := r.Properties

	for _, tag := range p.Tags.Items {
		actual.Tags = append(actual.Tags, tag)
	}
	actual.MachineType = fi.String(lastComponent(p.MachineType))
	actual.CanIPForward = &p.CanIpForward

	bootDiskImage, err := ShortenImageURL(cloud.Project, p.Disks[0].InitializeParams.SourceImage)
	if err != nil {
		return nil, fmt.Errorf("error parsing source image URL: %v", err)
	}
	actual.BootDiskImage = fi.String(bootDiskImage)
	actual.BootDiskType = &p.Disks[0].InitializeParams.DiskType
	actual.BootDiskSizeGB = &p.Disks[0].InitializeParams.DiskSizeGb

	if p.Scheduling != nil {
		actual.Preemptible = &p.Scheduling.Preemptible
	}
	if len(p.NetworkInterfaces) != 0 {
		ni := p.NetworkInterfaces[0]
		actual.Network = &Network{Name: fi.String(lastComponent(ni.Network))}
	}

	for _, serviceAccount := range p.ServiceAccounts {
		for _, scope := range serviceAccount.Scopes {
			actual.Scopes = append(actual.Scopes, scopeToShortForm(scope))
		}
	}

	//for i, disk := range p.Disks {
	//	if i == 0 {
	//		source := disk.Source
	//
	//		// TODO: Parse source URL instead of assuming same project/zone?
	//		name := lastComponent(source)
	//		d, err := cloud.Compute.Disks.Get(cloud.Project, *e.Zone, name).Do()
	//		if err != nil {
	//			if gce.IsNotFound(err) {
	//				return nil, fmt.Errorf("disk not found %q: %v", source, err)
	//			}
	//			return nil, fmt.Errorf("error querying for disk %q: %v", source, err)
	//		} else {
	//			imageURL, err := gce.ParseGoogleCloudURL(d.SourceImage)
	//			if err != nil {
	//				return nil, fmt.Errorf("unable to parse image URL: %q", d.SourceImage)
	//			}
	//			actual.Image = fi.String(imageURL.Project + "/" + imageURL.Name)
	//		}
	//	}
	//}

	if p.Metadata != nil {
		actual.Metadata = make(map[string]*fi.ResourceHolder)
		for _, meta := range p.Metadata.Items {
			actual.Metadata[meta.Key] = fi.WrapResource(fi.NewStringResource(meta.Value))
		}
	}

	return actual, nil
}

func (e *InstanceTemplate) Run(c *fi.Context) error {
	return fi.DefaultDeltaRunMethod(e, c)
}

func (_ *InstanceTemplate) CheckChanges(a, e, changes *InstanceTemplate) error {
	if fi.StringValue(e.BootDiskImage) == "" {
		return fi.RequiredField("BootDiskImage")
	}
	if fi.StringValue(e.MachineType) == "" {
		return fi.RequiredField("MachineType")
	}
	return nil
}

func (e *InstanceTemplate) mapToGCE(project string) (*compute.InstanceTemplate, error) {
	// TODO: This is similar to Instance...
	var scheduling *compute.Scheduling

	if fi.BoolValue(e.Preemptible) {
		scheduling = &compute.Scheduling{
			AutomaticRestart:  false,
			OnHostMaintenance: "TERMINATE",
			Preemptible:       true,
		}
	} else {
		scheduling = &compute.Scheduling{
			AutomaticRestart: true,
			// TODO: Migrate or terminate?
			OnHostMaintenance: "MIGRATE",
			Preemptible:       false,
		}
	}

	glog.Infof("We should be using NVME for GCE")

	var disks []*compute.AttachedDisk
	disks = append(disks, &compute.AttachedDisk{
		InitializeParams: &compute.AttachedDiskInitializeParams{
			SourceImage: BuildImageURL(project, *e.BootDiskImage),
			DiskSizeGb:  *e.BootDiskSizeGB,
			DiskType:    *e.BootDiskType,
		},
		Boot:       true,
		DeviceName: "persistent-disks-0",
		Index:      0,
		AutoDelete: true,
		Mode:       "READ_WRITE",
		Type:       "PERSISTENT",
	})

	var tags *compute.Tags
	if e.Tags != nil {
		tags = &compute.Tags{
			Items: e.Tags,
		}
	}

	var networkInterfaces []*compute.NetworkInterface
	ni := &compute.NetworkInterface{
		AccessConfigs: []*compute.AccessConfig{{
			//NatIP: *e.IPAddress.Address,
			Type: "ONE_TO_ONE_NAT",
		}},
		Network: e.Network.URL(project),
	}
	if e.Subnet != nil {
		ni.Subnetwork = *e.Subnet.Name
	}
	networkInterfaces = append(networkInterfaces, ni)

	var serviceAccounts []*compute.ServiceAccount
	if e.Scopes != nil {
		var scopes []string
		for _, s := range e.Scopes {
			s = scopeToLongForm(s)

			scopes = append(scopes, s)
		}
		serviceAccounts = append(serviceAccounts, &compute.ServiceAccount{
			Email:  "default",
			Scopes: scopes,
		})
	}

	var metadataItems []*compute.MetadataItems
	for key, r := range e.Metadata {
		v, err := r.AsString()
		if err != nil {
			return nil, fmt.Errorf("error rendering InstanceTemplate metadata %q: %v", key, err)
		}
		metadataItems = append(metadataItems, &compute.MetadataItems{
			Key:   key,
			Value: v,
		})
	}

	i := &compute.InstanceTemplate{
		Name: *e.Name,
		Properties: &compute.InstanceProperties{
			CanIpForward: *e.CanIPForward,

			Disks: disks,

			MachineType: *e.MachineType,

			Metadata: &compute.Metadata{
				Items: metadataItems,
			},

			NetworkInterfaces: networkInterfaces,

			Scheduling: scheduling,

			ServiceAccounts: serviceAccounts,

			Tags: tags,
		},
	}

	return i, nil
}

func (_ *InstanceTemplate) RenderGCE(t *gce.GCEAPITarget, a, e, changes *InstanceTemplate) error {
	project := t.Cloud.Project

	i, err := e.mapToGCE(project)
	if err != nil {
		return err
	}

	if a == nil {
		glog.V(4).Infof("Creating InstanceTemplate %v", i)

		_, err := t.Cloud.Compute.InstanceTemplates.Insert(t.Cloud.Project, i).Do()
		if err != nil {
			return fmt.Errorf("error creating InstanceTemplate: %v", err)
		}

	} else {
		// TODO: Make error again
		glog.Errorf("Cannot apply changes to InstanceTemplate: %v", changes)
		//return fmt.Errorf("Cannot apply changes to InstanceTemplate: %v", changes)
	}

	return nil
}

type terraformInstanceTemplate struct {
	Name                  string                       `json:"name"`
	CanIPForward          bool                         `json:"can_ip_forward"`
	MachineType           string                       `json:"machine_type,omitempty"`
	ServiceAccount        *terraformServiceAccount     `json:"service_account,omitempty"`
	Scheduling            *terraformScheduling         `json:"scheduling,omitempty"`
	Disks                 []*terraformAttachedDisk     `json:"disk,omitempty"`
	NetworkInterfaces     []*terraformNetworkInterface `json:"network_interface,omitempty"`
	Metadata              map[string]string            `json:"metadata,omitempty"`
	MetadataStartupScript string                       `json:"metadata_startup_script,omitempty"`
	Tags                  []string                     `json:"tags,omitempty"`

	// Only for instances:
	Zone string `json:"zone,omitempty"`
}

type terraformServiceAccount struct {
	Scopes []string `json:"scopes"`
}

type terraformScheduling struct {
	AutomaticRestart  bool   `json:"automatic_restart"`
	OnHostMaintenance string `json:"on_host_maintenance,omitempty"`
	Preemptible       bool   `json:"preemptible"`
}

type terraformAttachedDisk struct {
	// These values are common
	AutoDelete bool   `json:"auto_delete,omitempty"`
	DeviceName string `json:"device_name,omitempty"`

	// DANGER - common but different meaning:
	//   for an instance template this is scratch vs persistent
	//   for an instance this is 'pd-standard', 'pd-ssd', 'local-ssd' etc
	Type string `json:"type,omitempty"`

	// These values are only for instance templates:
	Boot        bool   `json:"boot,omitempty"`
	DiskName    string `json:"disk_name,omitempty"`
	SourceImage string `json:"source_image,omitempty"`
	Source      string `json:"source,omitempty"`
	Interface   string `json:"interface,omitempty"`
	Mode        string `json:"mode,omitempty"`
	DiskType    string `json:"disk_type,omitempty"`
	DiskSizeGB  int64  `json:"disk_size_gb,omitempty"`

	// These values are only for instances:
	Disk    string `json:"disk,omitempty"`
	Image   string `json:"image,omitempty"`
	Scratch bool   `json:"scratch,omitempty"`
	Size    int64  `json:"size,omitempty"`
}

type terraformNetworkInterface struct {
	Network      *terraform.Literal       `json:"network,omitempty"`
	Subnetwork   *terraform.Literal       `json:"subnetwork,omitempty"`
	AccessConfig []*terraformAccessConfig `json:"access_config"`
}

type terraformAccessConfig struct {
	NatIP *terraform.Literal `json:"nat_ip,omitempty"`
}

func (t *terraformInstanceTemplate) AddNetworks(network *Network, subnet *Subnet, networkInterfacs []*compute.NetworkInterface) {
	for _, g := range networkInterfacs {
		tf := &terraformNetworkInterface{}
		if network != nil {
			tf.Network = network.TerraformName()
		}
		if subnet != nil {
			tf.Subnetwork = subnet.TerraformName()
		}
		for _, gac := range g.AccessConfigs {
			tac := &terraformAccessConfig{}
			natIP := gac.NatIP
			if strings.HasPrefix(natIP, "${") {
				tac.NatIP = terraform.LiteralExpression(natIP)
			} else if natIP != "" {
				tac.NatIP = terraform.LiteralFromStringValue(natIP)
			}

			tf.AccessConfig = append(tf.AccessConfig, tac)
		}

		t.NetworkInterfaces = append(t.NetworkInterfaces, tf)
	}
}

func (t *terraformInstanceTemplate) AddMetadata(metadata *compute.Metadata) {
	if metadata != nil {
		if t.Metadata == nil {
			t.Metadata = make(map[string]string)
		}
		for _, g := range metadata.Items {
			value := g.Value
			tfValue := strings.Replace(value, "${", "$${", -1)
			t.Metadata[g.Key] = tfValue
		}
	}
}

func (t *terraformInstanceTemplate) AddServiceAccounts(serviceAccounts []*compute.ServiceAccount) {
	for _, g := range serviceAccounts {
		for _, scope := range g.Scopes {
			if t.ServiceAccount == nil {
				t.ServiceAccount = &terraformServiceAccount{}
			}
			t.ServiceAccount.Scopes = append(t.ServiceAccount.Scopes, scope)
		}
	}
}

func (_ *InstanceTemplate) RenderTerraform(t *terraform.TerraformTarget, a, e, changes *InstanceTemplate) error {
	project := t.Project

	i, err := e.mapToGCE(project)
	if err != nil {
		return err
	}

	tf := &terraformInstanceTemplate{
		Name:         i.Name,
		CanIPForward: i.Properties.CanIpForward,
		//Description: i.Properties.Description,
		MachineType: i.Properties.MachineType,
		Tags:        i.Properties.Tags.Items,
	}

	tf.AddServiceAccounts(i.Properties.ServiceAccounts)

	for _, d := range i.Properties.Disks {
		tfd := &terraformAttachedDisk{
			AutoDelete:  d.AutoDelete,
			Boot:        d.Boot,
			DeviceName:  d.DeviceName,
			DiskName:    d.InitializeParams.DiskName,
			SourceImage: d.InitializeParams.SourceImage,
			Source:      d.Source,
			Interface:   d.Interface,
			Mode:        d.Mode,
			DiskType:    d.InitializeParams.DiskType,
			DiskSizeGB:  d.InitializeParams.DiskSizeGb,
			Type:        d.Type,
		}
		tf.Disks = append(tf.Disks, tfd)
	}

	tf.AddNetworks(e.Network, e.Subnet, i.Properties.NetworkInterfaces)

	tf.AddMetadata(i.Properties.Metadata)

	if i.Properties.Scheduling != nil {
		tf.Scheduling = &terraformScheduling{
			AutomaticRestart:  i.Properties.Scheduling.AutomaticRestart,
			OnHostMaintenance: i.Properties.Scheduling.OnHostMaintenance,
			Preemptible:       i.Properties.Scheduling.Preemptible,
		}
	}

	return t.RenderResource("google_compute_instance_template", i.Name, tf)
}

func (i *InstanceTemplate) TerraformLink() *terraform.Literal {
	return terraform.LiteralSelfLink("google_compute_instance_template", *i.Name)
}
