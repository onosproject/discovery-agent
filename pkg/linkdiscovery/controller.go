// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package linkdiscovery implements the link discovery control logic
package linkdiscovery

import (
	"context"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	"github.com/openconfig/gnmi/proto/gnmi"
	p4info "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc"
	"sync"
	"time"
)

var log = logging.GetLogger("linkdiscovery")

// State represents the various states of controller lifecycle
type State int

const (
	// Disconnected represents the default/initial state
	Disconnected State = iota
	// Connected represents state where Stratum connection(s) have been established
	Connected
	// Configured represents state where P4Info has been obtained
	Configured
	// Elected represents state where the link agent established mastership for its role
	Elected
	// PortsDiscovered represents state where the link agent discovered all Stratum ports
	PortsDiscovered
)

// Controller represents the link discovery control
type Controller struct {
	TargetAddress   string
	IngressDeviceID string

	state   State
	lock    sync.RWMutex
	running bool
	config  *Config
	ports   map[string]*Port
	links   map[uint32]*Link

	conn       *grpc.ClientConn
	p4Client   p4api.P4RuntimeClient
	gnmiClient gnmi.GNMIClient

	ctx       context.Context
	ctxCancel context.CancelFunc

	chassisID  uint64
	info       *p4info.P4Info
	codec      *p4utils.ControllerMetadataCodec
	stream     p4api.P4Runtime_StreamChannelClient
	electionID *p4api.Uint128
	cookie     uint64
	role       *p4api.Role
}

// Config contains configuration parameters for the link discovery
type Config struct {
	EmitFrequency               time.Duration
	MaxLinkAge                  time.Duration
	PipelineValidationFrequency time.Duration
	PortRediscoveryFrequency    time.Duration
	LinkPruneFrequency          time.Duration
}

// Port holds data about each discovered switch ports
type Port struct {
	ID     string
	Number uint32
	Status string
}

// Link holds data about each discovered ingress links
type Link struct {
	EgressPort     uint32
	EgressDeviceID string
	IngressPort    uint32
	LastUpdate     time.Time
}

// NewController creates a new link discovery controller
func NewController(targetAddress string, agentID string, config *Config) *Controller {
	return &Controller{
		TargetAddress:   targetAddress,
		IngressDeviceID: agentID,
		config:          config,
		ports:           make(map[string]*Port),
		links:           make(map[uint32]*Link),
	}
}

// Start starts the controller
func (c *Controller) Start() {
	go c.run()
}

// Stop stops the controller
func (c *Controller) Stop() {
	c.running = false
}

func (c *Controller) updateIngressLink(ingressPort uint32, egressPort uint32, egressDeviceID string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	link, ok := c.links[ingressPort]
	if !ok {
		link = &Link{
			EgressPort:     egressPort,
			EgressDeviceID: egressDeviceID,
			IngressPort:    ingressPort,
		}
		c.links[ingressPort] = link
	}
	link.LastUpdate = time.Now()
}

func (c *Controller) getPort(id string) *Port {
	port, ok := c.ports[id]
	if !ok {
		port = &Port{ID: id}
		c.ports[id] = port
	}
	return port
}

func (c *Controller) run() {
	log.Infof("Started")
	c.running = true
	for c.running {
		switch c.state {
		case Disconnected:
			c.waitForDeviceConnection()
		case Connected:
			c.waitForPipelineConfiguration()
		case Configured:
			c.waitForMastershipArbitration()
		case Elected:
			c.discoverPorts()
		case PortsDiscovered:
			c.enterLinkDiscovery()
		}
	}
	log.Infof("Stopped")
}

func (c *Controller) pauseIfRunning(pause time.Duration) {
	if c.running {
		time.Sleep(pause)
	}
}

func (c *Controller) enterLinkDiscovery() {
	// Setup packet-in handler
	go c.handlePackets()

	// Program intercept rule(s)
	c.programPacketInterceptRule()

	tLinks := time.NewTicker(c.config.EmitFrequency)
	tConf := time.NewTicker(c.config.PipelineValidationFrequency)
	tPorts := time.NewTicker(c.config.PortRediscoveryFrequency)
	tPrune := time.NewTicker(c.config.LinkPruneFrequency)

	for c.running && c.state == PortsDiscovered {
		select {
		// Periodically emit LLDP packets
		case <-tLinks.C:
			c.emitLLDPPackets()

		// Periodically re-discover ports
		case <-tPorts.C:
			c.discoverPorts()

		// Periodically validate pipeline config
		case <-tConf.C:
			c.validatePipelineConfiguration()

		// Periodically prune links
		case <-tPrune.C:
			c.pruneLinks()
		}
	}
}
