// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package network

//go:generate stringer -type=ConfigLayer -linecomment

// ConfigLayer describes network configuration layers, with lowest priority first.
type ConfigLayer int

// Configuration layers.
const (
	ConfigDefault              ConfigLayer = iota // default
	ConfigCmdline                                 // cmdline
	ConfigDHCP                                    // dhcp
	ConfigPlatform                                // platform
	ConfigMachineConfiguration                    // configuration
)

// MarshalYAML implements yaml.Marshaler.
func (layer ConfigLayer) MarshalYAML() (interface{}, error) {
	return layer.String(), nil
}
