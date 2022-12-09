// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package linkdiscovery

import (
	gogo "github.com/gogo/protobuf/types"
	"github.com/onosproject/onos-api/go/onos/stratum"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	"github.com/openconfig/gnmi/proto/gnmi"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
	"time"
)

const linkAgentRoleName = "link_local_agent"

var role = newRole()

func newRole() *p4api.Role {
	roleConfig := &stratum.P4RoleConfig{
		PacketInFilter: &stratum.P4RoleConfig_PacketFilter{
			MetadataId: 4,
			Value:      []byte("\x01"),
		},
		ReceivesPacketIns: true,
		CanPushPipeline:   true,
	}
	any, _ := gogo.MarshalAny(roleConfig)
	return &p4api.Role{
		Name: linkAgentRoleName,
		Config: &anypb.Any{
			TypeUrl: any.TypeUrl,
			Value:   any.Value,
		},
	}
}

const (
	connectionRetryPause            = 5 * time.Second
	pipelineConfigurationPause      = 5 * time.Second
	mastershipArbitrationRetryPause = 5 * time.Second
)

func (c *Controller) waitForDeviceConnection() {
	log.Infof("Connecting to stratum agent...")
	for c.running {
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		}

		if conn, err := grpc.Dial(c.TargetAddress, opts...); err == nil {
			c.conn = conn
			c.p4Client = p4api.NewP4RuntimeClient(c.conn)
			c.gnmiClient = gnmi.NewGNMIClient(c.conn)
			c.state = Connected
			log.Infof("Connected")
			return
		}
		c.pauseIfRunning(connectionRetryPause)
	}
}

func (c *Controller) waitForPipelineConfiguration() {
	log.Infof("Retrieving pipeline configuration...")
	for c.running {
		// Ask for the pipeline config P4Infi and cookie
		resp, err := c.p4Client.GetForwardingPipelineConfig(c.ctx, &p4api.GetForwardingPipelineConfigRequest{
			ResponseType: p4api.GetForwardingPipelineConfigRequest_P4INFO_AND_COOKIE,
		})
		if err == nil {
			c.cookie = resp.Config.Cookie.Cookie
			c.info = resp.Config.P4Info // TODO: remove this and replace with NewPacketInFilter(c.info)
			c.codec = p4utils.NewControllerMetadataCodec(c.info)
			c.state = Configured
			log.Infof("Pipeline configuration obtained and processed")
			return
		}
		c.pauseIfRunning(pipelineConfigurationPause)
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
			c.state = Connected
			log.Infof("Pipeline configuration changed")
		}
		return
	}
	log.Warnf("Unable to validate pipeline configuration: %+v", err)
}

func (c *Controller) waitForMastershipArbitration() {
	log.Infof("Running mastership arbitration...")
	var err error
	for c.running { // Establish stream channel
		if c.stream, err = c.p4Client.StreamChannel(c.ctx); err == nil {
			for c.running { // Issue mastership arbitration request
				c.electionID = TimeBasedElectionID()
				if err = c.stream.Send(p4utils.CreateMastershipArbitration(c.electionID, role)); err == nil {
					var mar *p4api.MasterArbitrationUpdate
					for c.running && mar == nil { // Wait for mastership arbitration update
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
						c.state = Elected
						log.Infof("Obtained mastership for role: %s", linkAgentRoleName)
						return
					}
				}
			}
		}
		c.pauseIfRunning(mastershipArbitrationRetryPause)
	}
}

// TODO: Extract to onos-net-lib p4utils

// TimeBasedElectionID returns election ID generated from the UnixNano timestamp
// High contains seconds, Low contains remaining nanos
func TimeBasedElectionID() *p4api.Uint128 {
	now := time.Now()
	t := now.UnixNano()
	return &p4api.Uint128{High: uint64(t / 1e9), Low: uint64(t % 1e9)}
}
