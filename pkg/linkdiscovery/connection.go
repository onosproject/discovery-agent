// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package linkdiscovery

import (
	gogo "github.com/gogo/protobuf/types"
	"github.com/onosproject/onos-api/go/onos/stratum"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	"github.com/openconfig/gnmi/proto/gnmi"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
)

const linkAgentRoleName = "link-agent"

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

func (c *Controller) establishDeviceConnection() error {
	log.Infof("Connecting to stratum agent...")
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	var err error
	c.conn, err = grpc.Dial(c.targetAddress, opts...)
	if err != nil {
		return err
	}

	c.p4Client = p4api.NewP4RuntimeClient(c.conn)
	c.gnmiClient = gnmi.NewGNMIClient(c.conn)

	// Establish stream and issue mastership
	if c.stream, err = c.p4Client.StreamChannel(c.ctx); err != nil {
		return err
	}

	c.electionID = &p4api.Uint128{Low: 123, High: 0}
	if err = c.stream.Send(p4utils.CreateMastershipArbitration(c.electionID, role)); err != nil {
		return err
	}

	var msg *p4api.StreamMessageResponse
	if msg, err = c.stream.Recv(); err != nil {
		return err
	}
	mar := msg.GetArbitration()
	if mar == nil {
		return errors.NewInvalid("Did not receive mastership arbitration")
	}
	if mar.ElectionId == nil || mar.ElectionId.High != c.electionID.High || mar.ElectionId.Low != c.electionID.Low {
		return errors.NewInvalid("Did not win election")
	}
	return nil
}
