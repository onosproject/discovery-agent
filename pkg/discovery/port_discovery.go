// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"fmt"
	"github.com/onosproject/onos-net-lib/pkg/gnmiutils"
	"github.com/openconfig/gnmi/proto/gnmi"
	"io"
)

const (
	portUp   = "UP"
	portDown = "DOWN"
)

// Auxiliary state for tracking port state subscription request
type portMonitor struct {
	ctx       context.Context
	ctxCancel context.CancelFunc
	portCount int
}

func (c *Controller) discoverPorts() {
	log.Infof("Discovering ports...")
	resp, err := c.gnmiClient.Get(c.ctx, &gnmi.GetRequest{
		Path: []*gnmi.Path{gnmiutils.ToPath("interfaces/interface[name=...]/state")},
	})
	if err != nil {
		log.Warn("Unable to issue gNMI request for port list: %+v", err)
		c.setStateIf(Elected, Disconnected)
		return
	}
	if len(resp.Notification) == 0 {
		log.Warn("No port data received")
		return
	}

	ports := make(map[string]*Port)
	for _, update := range resp.Notification[0].Update {
		port := getPort(ports, update.Path.Elem[1].Key["name"])
		last := len(update.Path.Elem) - 1
		switch update.Path.Elem[last].Name {
		case "id":
			port.Number = uint32(update.Val.GetUintVal())
		case "oper-status":
			port.Status = update.Val.GetStringVal()
		case "last-change":
			port.LastChange = update.Val.GetUintVal()
		}
	}

	c.lock.Lock()
	c.ports = ports

	// Once ports are discovered kick off a port-status monitor, if necessary
	c.monitor.start(c, len(ports))
	c.lock.Unlock()

	c.setStateIf(Elected, PortsDiscovered)

	log.Infof("Ports discovered")
}

// Gets the port with the specified key from the given map, inserting a new one if necessary
func getPort(ports map[string]*Port, id string) *Port {
	port, ok := ports[id]
	if !ok {
		port = &Port{ID: id}
		ports[id] = port
	}
	return port
}

// Starts the port monitor if not already started and if the given port change stamp is newer
func (m *portMonitor) start(c *Controller, portCount int) {
	if m.ctxCancel == nil || m.portCount != portCount {
		m.portCount = portCount
		m.stop()
		log.Infof("Starting port status monitor...")
		go m.monitorPortStatus(c)
	}
}

// Stops the port monitor
func (m *portMonitor) stop() {
	if m.ctxCancel != nil {
		log.Infof("Stopping port status monitor...")
		m.ctxCancel()
		m.ctxCancel = nil
	}
}

// Issues subscribe request for port state updates and monitors the stream for update notifications
func (m *portMonitor) monitorPortStatus(c *Controller) {
	log.Infof("Port status monitor started")
	m.ctx, m.ctxCancel = context.WithCancel(context.Background())
	stream, err := c.gnmiClient.Subscribe(m.ctx)
	if err != nil {
		log.Warn("Unable to subscribe for port state updates: %+v", err)
		return
	}

	subscriptions := make([]*gnmi.Subscription, 0, len(c.ports))
	for key := range c.ports {
		subscriptions = append(subscriptions, &gnmi.Subscription{
			Path: gnmiutils.ToPath(fmt.Sprintf("interfaces/interface[name=%s]/state/oper-state", key)),
		})
	}
	if err = stream.Send(&gnmi.SubscribeRequest{
		Request: &gnmi.SubscribeRequest_Subscribe{
			Subscribe: &gnmi.SubscriptionList{Subscription: subscriptions},
		}}); err != nil {
		log.Warn("Unable to send subscription request for port state updates: %+v", err)
		return
	}

	for {
		resp, err := stream.Recv()
		if err != nil {
			if err != io.EOF {
				log.Warn("Unable to read subscription response for port state updates: %+v", err)
			}
			log.Infof("Port status monitor stopped")
			return
		}
		log.Infof("Got port status update %+v", resp.GetUpdate())
		if resp.GetUpdate() != nil {
			for _, update := range resp.GetUpdate().Update {
				if update.Path.Elem[(len(update.Path.Elem)-1)].Name == "oper-status" {
					c.processPortStatusUpdate(update.Path.Elem[1].Key["name"], update.Val.GetStringVal())
				}
			}
		}
	}
}

// If the given port status changes from UP to DOWN, delete any associated link
func (c *Controller) processPortStatusUpdate(portKey string, newPortStatus string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	port := getPort(c.ports, portKey)
	if port.Status == portUp && newPortStatus == portDown {
		log.Infof("Deleting any ingress link for port %d", port.Number)
		c.deleteLink(port.Number)
	}
	port.Status = newPortStatus
}
