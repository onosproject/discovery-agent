// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package linkdiscovery

import (
	"context"
	"encoding/binary"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/onosproject/onos-net-lib/pkg/gnmiutils"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	"github.com/onosproject/onos-net-lib/pkg/packet"
	"github.com/openconfig/gnmi/proto/gnmi"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"strconv"
	"time"
)

const (
	linkAgentRoleName = "link_local_agent"
	linkAgentRoleID   = "\x03"

	connectionRetryPause            = 5 * time.Second
	pipelineFetchRetryPause         = 5 * time.Second
	mastershipArbitrationRetryPause = 5 * time.Second
)

func (c *Controller) waitForDeviceConnection() {
	log.Infof("Connecting to stratum agent at %s...", c.TargetAddress)
	for c.getState() == Disconnected {
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		}

		if conn, err := grpc.Dial(c.TargetAddress, opts...); err == nil {
			c.conn = conn
			c.p4Client = p4api.NewP4RuntimeClient(c.conn)
			c.gnmiClient = gnmi.NewGNMIClient(c.conn)
			c.ctx, c.ctxCancel = context.WithCancel(context.Background())
			c.setState(Connected)
			log.Infof("Connected")
		} else {
			log.Warnf("Unable to connect to stratum agent: %+v", err)
			c.pauseIf(Disconnected, connectionRetryPause)
		}
	}
}

func (c *Controller) waitForPipelineConfiguration() {
	log.Infof("Retrieving pipeline configuration...")
	for c.getState() == Connected {
		// Ask for the pipeline config P4Infi and cookie
		resp, err := c.p4Client.GetForwardingPipelineConfig(c.ctx, &p4api.GetForwardingPipelineConfigRequest{
			ResponseType: p4api.GetForwardingPipelineConfigRequest_P4INFO_AND_COOKIE,
		})
		if err == nil {
			if resp.Config.Cookie.Cookie != 0 {
				c.cookie = resp.Config.Cookie.Cookie
				c.info = resp.Config.P4Info
				c.codec = p4utils.NewControllerMetadataCodec(c.info)
				c.role = p4utils.NewStratumRole(linkAgentRoleName, c.codec.RoleAgentIDMetadataID(), []byte(linkAgentRoleID), true, false)
				c.setState(PipelineConfigAvailable)
				log.Infof("Pipeline configuration obtained and processed")
			} else {
				log.Warnf("Pipeline configuration not set yet on the stratum device")
			}
		} else {
			log.Warnf("Unable to retrieve pipeline configuration: %+v", err)
		}
		c.pauseIf(Connected, pipelineFetchRetryPause)
	}
}

func (c *Controller) validatePipelineConfiguration() {
	log.Infof("Validating pipeline configuration...")
	// Ask for the pipeline config cookie
	resp, err := c.p4Client.GetForwardingPipelineConfig(c.ctx, &p4api.GetForwardingPipelineConfigRequest{
		ResponseType: p4api.GetForwardingPipelineConfigRequest_COOKIE_ONLY,
	})
	if err == nil {
		// If the cookie changed, transition back to connected state
		if c.cookie != resp.Config.Cookie.Cookie {
			c.setState(Connected)
			log.Infof("Pipeline configuration changed")
		}
		return
	}
	log.Warnf("Unable to validate pipeline configuration: %+v", err)
}

func (c *Controller) waitForMastershipArbitration() {
	log.Infof("Running mastership arbitration...")
	var err error
	for c.getState() == PipelineConfigAvailable {
		// Establish stream channel
		if c.stream, err = c.p4Client.StreamChannel(c.ctx); err == nil {
			for c.getState() == PipelineConfigAvailable {
				// Issue mastership arbitration request
				c.electionID = p4utils.TimeBasedElectionID()
				if err = c.stream.Send(p4utils.CreateMastershipArbitration(c.electionID, c.role)); err == nil {
					var mar *p4api.MasterArbitrationUpdate
					for c.getState() == PipelineConfigAvailable && mar == nil {
						// Wait for mastership arbitration update
						var msg *p4api.StreamMessageResponse
						if msg, err = c.stream.Recv(); err != nil {
							log.Warnf("Unable to receive stream response: %+v", err)
						} else {
							mar = msg.GetArbitration()
							if mar == nil {
								log.Warnf("Did not receive mastership arbitration: %+v", msg)
							}
						}
					}

					// If we got mastership arbitration with a winning election ID matching ours, return
					if mar != nil && mar.ElectionId != nil &&
						mar.ElectionId.High == c.electionID.High && mar.ElectionId.Low == c.electionID.Low {
						c.setState(Elected)
						log.Infof("Obtained mastership for role: %s", linkAgentRoleName)
						return
					}
				}
			}
		}
		c.pauseIf(PipelineConfigAvailable, mastershipArbitrationRetryPause)
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
		port := getPort(ports, update.Path.Elem[1].Key["name"])
		last := len(update.Path.Elem) - 1
		switch update.Path.Elem[last].Name {
		case "id":
			port.Number = uint32(update.Val.GetUintVal())
		case "oper-status":
			port.Status = update.Val.GetStringVal()
		}
	}

	c.setStateIf(Elected, PortsDiscovered)
	c.lock.Lock()
	defer c.lock.Unlock()
	c.ports = ports
	log.Infof("Ports discovered")
}

func getPort(ports map[string]*Port, id string) *Port {
	port, ok := ports[id]
	if !ok {
		port = &Port{ID: id}
		ports[id] = port
	}
	return port
}

func (c *Controller) handlePackets() {
	log.Infof("Monitoring message stream")
	for {
		msg, err := c.stream.Recv()
		if err != nil {
			log.Warnf("Unable to read stream response: %+v", err)
			return
		}

		if msg.GetPacket() != nil {
			c.processPacket(msg.GetPacket())
		}
		// TODO: deal with mastership arbitration update in case we got demoted

		state := c.getState()
		if state != Configured && state != Reconfigured {
			return
		}
	}
}

func (c *Controller) processPacket(packetIn *p4api.PacketIn) {
	rawPacket := gopacket.NewPacket(packetIn.Payload, layers.LayerTypeEthernet, gopacket.Default)
	lldpLayer := rawPacket.Layer(layers.LayerTypeLinkLayerDiscovery)
	if lldpLayer != nil {
		pim := c.codec.DecodePacketInMetadata(packetIn.Metadata)
		lldp := lldpLayer.(*layers.LinkLayerDiscovery)
		egressPort, err := strconv.ParseUint(string(lldp.PortID.ID), 10, 32)
		if err != nil {
			log.Warn("Unable to parse egress port ID: %+v", err)
			return
		}
		c.updateIngressLink(pim.IngressPort, uint32(egressPort), string(lldp.ChassisID.ID))
	}
}

func (c *Controller) programPacketInterceptRule() {
	aclTable := p4utils.FindTable(c.info, "FabricIngress.acl.acl")
	puntAction := p4utils.FindAction(c.info, "FabricIngress.acl.punt_to_cpu")

	if aclTable == nil {
		log.Warnf("Unable to find FabricIngress.acl.acl table or FabricIngress.acl.punt_to_cpu action")
		return
	}

	ethTypeMatchField := p4utils.FindTableMatchField(aclTable, "eth_type")
	if ethTypeMatchField == nil {
		log.Warnf("Unable to find eth_type match field")
	}

	setAgentRoleActionParam := p4utils.FindActionParam(puntAction, "set_role_agent_id")
	if aclTable == nil {
		log.Warnf("Unable to find set_role_agent_id action param")
		return
	}

	if err := c.installPuntRule(aclTable.Preamble.Id, puntAction.Preamble.Id, ethTypeMatchField.Id, setAgentRoleActionParam.Id); err != nil {
		log.Warnf("Unable to install LLDP intercept rule: %+v", err)
	}
}

func (c *Controller) emitLLDPPackets() {
	log.Infof("Sending LLDP packets...")
	for _, port := range c.ports {
		lldpBytes, err := packet.ControllerLLDPPacket(c.IngressDeviceID, port.Number)
		if err != nil {
			log.Warnf("Unable to create LLDP packet: %+v", err)
		} else {
			err = c.stream.Send(&p4api.StreamMessageRequest{
				Update: &p4api.StreamMessageRequest_Packet{
					Packet: &p4api.PacketOut{
						Payload:  lldpBytes,
						Metadata: c.codec.EncodePacketOutMetadata(&p4utils.PacketOutMetadata{EgressPort: port.Number}),
					}},
			})
			if err != nil {
				log.Warnf("Unable to emit LLDP packet-out: %+v", err)
			}
		}
	}
	log.Info("LLDP packets emitted")
}

func (c *Controller) installPuntRule(tableID uint32, actionID uint32, ethTypeMatchFieldID uint32, setRoleAgentParamID uint32) error {
	ethTypeValue := []byte{0, 0}
	binary.BigEndian.PutUint16(ethTypeValue, uint16(layers.EthernetTypeLinkLayerDiscovery))

	_, err := c.p4Client.Write(c.ctx, &p4api.WriteRequest{
		DeviceId:   c.chassisID,
		Role:       linkAgentRoleName,
		ElectionId: c.electionID,
		Updates: []*p4api.Update{{
			Type: p4api.Update_INSERT,
			Entity: &p4api.Entity{Entity: &p4api.Entity_TableEntry{
				TableEntry: &p4api.TableEntry{
					TableId: tableID,
					Match: []*p4api.FieldMatch{{
						FieldId: ethTypeMatchFieldID,
						FieldMatchType: &p4api.FieldMatch_Ternary_{
							Ternary: &p4api.FieldMatch_Ternary{
								Value: ethTypeValue,
								Mask:  []byte{0xff, 0xff},
							},
						},
					}},
					Action: &p4api.TableAction{
						Type: &p4api.TableAction_Action{
							Action: &p4api.Action{
								ActionId: actionID,
								Params:   []*p4api.Action_Param{{ParamId: setRoleAgentParamID, Value: []byte(linkAgentRoleID)}},
							},
						},
					},
				}}},
		}},
	})
	return err
}
