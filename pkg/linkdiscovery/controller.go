// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package linkdiscovery implements the link discovery control logic
package linkdiscovery

import (
	"context"
	"github.com/onosproject/fabric-sim/pkg/utils"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	"github.com/openconfig/gnmi/proto/gnmi"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc"
	"sync"
)

var log = logging.GetLogger("linkdiscovery")

// Controller represents the link discovery control
type Controller struct {
	targetAddress   string
	ingressDeviceID string

	lock sync.RWMutex
	//config *Config
	ports map[string]*Port
	//links  map[uint32]*Link

	ctx        context.Context
	conn       *grpc.ClientConn
	p4Client   p4api.P4RuntimeClient
	gnmiClient gnmi.GNMIClient

	codec      *p4utils.ControllerMetadataCodec
	stream     p4api.P4Runtime_StreamChannelClient
	electionID *p4api.Uint128
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
func NewController(targetAddress string) *Controller {
	return &Controller{targetAddress: targetAddress}
}

// Start starts the controller
func (c *Controller) Start() {
	log.Infof("Starting...")
	// FIXME: temporary invocation
	_ = c.establishDeviceConnection()
	_ = c.discoverPorts()
	_ = c.discoverLinks()
}

func (c *Controller) discoverPorts() error {
	resp, err := c.gnmiClient.Get(c.ctx, &gnmi.GetRequest{
		Path: []*gnmi.Path{utils.ToPath("interfaces/interface[name=...]/state")},
	})
	if err != nil {
		return err
	}
	if len(resp.Notification) == 0 {
		return errors.NewInvalid("No port data received")
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.ports = make(map[string]*Port)
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
	return nil
}

func (c *Controller) getPort(id string) *Port {
	port, ok := c.ports[id]
	if !ok {
		port = &Port{ID: id}
		c.ports[id] = port
	}
	return port
}

func (c *Controller) discoverLinks() error {
	for _, port := range c.ports {
		lldpBytes, err := utils.ControllerLLDPPacket(c.ingressDeviceID, port.Number)
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
	return nil
}
