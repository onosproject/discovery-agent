// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package linkdiscovery implements the link discovery control logic
package linkdiscovery

import (
	"context"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/gnmiutils"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	"github.com/onosproject/onos-net-lib/pkg/packet"
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
	//config *Config
	ports map[string]*Port
	links map[uint32]*Link

	ctx        context.Context
	conn       *grpc.ClientConn
	p4Client   p4api.P4RuntimeClient
	gnmiClient gnmi.GNMIClient

	codec      *p4utils.ControllerMetadataCodec
	stream     p4api.P4Runtime_StreamChannelClient
	electionID *p4api.Uint128
	cookie     uint64
	info       *p4info.P4Info
}

// Config contains configuration parameters for the link discovery
type Config struct {
	EmitFrequency uint32
	MaxLinkAge    uint32
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
}

// NewController creates a new link discovery controller
func NewController(targetAddress string, agentID string) *Controller {
	return &Controller{
		TargetAddress:   targetAddress,
		IngressDeviceID: agentID,
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
	go handlePackets()

	// Program intercept rule(s)
	c.programPacketInterceptRule()

	tLinks := time.NewTicker(5 * time.Second)
	tConf := time.NewTicker(60 * time.Second)
	tPorts := time.NewTicker(60 * time.Second)
	tPrune := time.NewTicker(2 * time.Second)

	for c.running {
		select {
		// Periodically emit LLDP packets
		case <-tLinks.C:
			if err := c.emitLLDPPackets(); err != nil {
				log.Warn("Unable to emit LLDP packets: %+v", err)
			}

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

func (c *Controller) discoverPorts() {
	log.Infof("Discovering ports...")
	resp, err := c.gnmiClient.Get(c.ctx, &gnmi.GetRequest{
		Path: []*gnmi.Path{gnmiutils.ToPath("interfaces/interface[name=...]/state")},
	})
	if err != nil {
		log.Warn("Unable to issue gNMI request for port list: %+v", err)
		return
	}
	if len(resp.Notification) == 0 {
		log.Warn("No port data received")
		return
	}

	ports := make(map[string]*Port)
	for _, update := range resp.Notification[0].Update {
		port := c.getPort(update.Path.Elem[1].Key["name"])
		last := len(update.Path.Elem) - 1
		switch update.Path.Elem[last].Name {
		case "id":
			port.Number = uint32(update.Val.GetUintVal())
		case "oper-status":
			port.Status = update.Val.GetStringVal()
		}
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.ports = ports
	c.state = PortsDiscovered
	log.Infof("Ports discovered")
}

func (c *Controller) getPort(id string) *Port {
	port, ok := c.ports[id]
	if !ok {
		port = &Port{ID: id}
		c.ports[id] = port
	}
	return port
}

func handlePackets() {
	// TODO: implement LLDP packet handling
}

func (c *Controller) programPacketInterceptRule() {
	// TODO: implement programming the LLDP ethType intercept
}

func (c *Controller) emitLLDPPackets() error {
	log.Infof("Sending LLDP packets...")
	for _, port := range c.ports {
		lldpBytes, err := packet.ControllerLLDPPacket(c.IngressDeviceID, port.Number)
		if err != nil {
			return err
		}

		err = c.stream.Send(&p4api.StreamMessageRequest{
			Update: &p4api.StreamMessageRequest_Packet{
				Packet: &p4api.PacketOut{
					Payload:  lldpBytes,
					Metadata: c.codec.EncodePacketOutMetadata(&p4utils.PacketOutMetadata{EgressPort: port.Number}),
				}},
		})
		if err != nil {
			return err
		}
	}
	log.Info("LLDP packets emitted")
	return nil
}

func (c *Controller) pruneLinks() {
	// TODO: implement this
}
