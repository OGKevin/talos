// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package network

import (
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"inet.af/netaddr"

	"github.com/talos-systems/talos/pkg/resources/network/nethelpers"
)

// AddressSpecType is type of AddressSpec resource.
const AddressSpecType = resource.Type("AddressSpecs.net.talos.dev")

// AddressSpec resource holds physical network link status.
type AddressSpec struct {
	md   resource.Metadata
	spec AddressSpecSpec
}

// AddressSpecSpec describes status of rendered secrets.
type AddressSpecSpec struct {
	Address  netaddr.IPPrefix        `yaml:"address"`
	LinkName string                  `yaml:"linkName"`
	Family   nethelpers.Family       `yaml:"family"`
	Scope    nethelpers.Scope        `yaml:"scope"`
	Flags    nethelpers.AddressFlags `yaml:"flags"`
	Layer    ConfigLayer             `yaml:"layer"`
}

// NewAddressSpec initializes a SecretsStatus resource.
func NewAddressSpec(namespace resource.Namespace, id resource.ID) *AddressSpec {
	r := &AddressSpec{
		md:   resource.NewMetadata(namespace, AddressSpecType, id, resource.VersionUndefined),
		spec: AddressSpecSpec{},
	}

	r.md.BumpVersion()

	return r
}

// Metadata implements resource.Resource.
func (r *AddressSpec) Metadata() *resource.Metadata {
	return &r.md
}

// Spec implements resource.Resource.
func (r *AddressSpec) Spec() interface{} {
	return r.spec
}

func (r *AddressSpec) String() string {
	return fmt.Sprintf("network.AddressSpec(%q)", r.md.ID())
}

// DeepCopy implements resource.Resource.
func (r *AddressSpec) DeepCopy() resource.Resource {
	return &AddressSpec{
		md:   r.md,
		spec: r.spec,
	}
}

// ResourceDefinition implements meta.ResourceDefinitionProvider interface.
func (r *AddressSpec) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             AddressSpecType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}

// Status sets pod status.
func (r *AddressSpec) Status() *AddressSpecSpec {
	return &r.spec
}
