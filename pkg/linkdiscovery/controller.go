// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package linkdiscovery implements the link discovery control logic
package linkdiscovery

// Controller represents the link discovery control
type Controller struct {
	targetAddress   string
	ingressDeviceID string
	ports           map[uint32]*Port
	links           map[uint32]*Link
	config          *Config
}

// Config contains configuration parameters for the link discovery
type Config struct {
	emitFrequency uint32
	maxLinkAge    uint32
}

// Port holds data about each discovered switch ports
type Port struct {
	number  uint32
	enabled bool
}

// Link holds data about each discovered ingress links
type Link struct {
	egressPort     uint32
	egressDeviceID string
	ingressPort    uint32
}

// NewController creates a new link discovery controller
func NewController(targetAddress string) *Controller {
	return &Controller{targetAddress: targetAddress}
}

// Start starts the controller
func (c *Controller) Start() {

}
