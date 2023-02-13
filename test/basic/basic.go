// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"context"
	"fmt"
	simapi "github.com/onosproject/onos-api/go/onos/fabricsim"
	"github.com/onosproject/onos-lib-go/pkg/grpc/retry"
	utils "github.com/onosproject/onos-net-lib/pkg/gnmiutils"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	"github.com/openconfig/gnmi/proto/gnmi"
	p4info "github.com/p4lang/p4runtime/go/p4/config/v1"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"sync"
	"testing"
	"time"
)

const onosRoleName = "onoslite"
const onosRoleAgentID = "\x01"

// TestBasics loads fabric simulator with access fabric topology, and tests basic gNMI operations
func (s *TestSuite) TestBasics(t *testing.T) {
	// Retrieve device information from the fabric simulator
	deviceClient := simapi.NewDeviceServiceClient(s.fsimConn)
	ctx := context.Background()
	dresp, err := deviceClient.GetDevices(ctx, &simapi.GetDevicesRequest{})
	assert.NoError(t, err)

	// Load pipeline config and apply it to all devices
	info, err := p4utils.LoadP4Info("test/basic/p4info.txt")
	assert.NoError(t, err)

	wg := sync.WaitGroup{}
	wg.Add(2 * len(dresp.Devices))

	for i, device := range dresp.Devices {
		go func(id int, d *simapi.Device) {
			SetPipelineConfig(t, id, d, info)
			wg.Done()
		}(i, device)

		go func(id int) {
			ValidateLinkDiscovery(t, id)
			wg.Done()
		}(i)
	}

	wg.Wait()

	// Create subscribe connection for links on spine1
	gconn, err := CreateInsecureConnection("discovery-agent-0.discovery-agent:30000")
	assert.NoError(t, err)
	gnmiClient := gnmi.NewGNMIClient(gconn)

	t.Logf("spine1: Subscribing for gNMI notifications...")
	subClient, err := gnmiClient.Subscribe(ctx)
	assert.NoError(t, err)
	err = subClient.Send(&gnmi.SubscribeRequest{
		Request: &gnmi.SubscribeRequest_Subscribe{
			Subscribe: &gnmi.SubscriptionList{
				Subscription:     []*gnmi.Subscription{{Path: utils.ToPath("state/link[port=...]")}},
				Mode:             gnmi.SubscriptionList_STREAM,
				AllowAggregation: true,
				UpdatesOnly:      true,
			}},
	})
	assert.NoError(t, err)
	_, err = subClient.Recv() // Get the sync response first
	assert.NoError(t, err)

	// Disable the spine1/1 port...
	t.Log("Disabling port spine1/1...")
	_, err = deviceClient.DisablePort(ctx, &simapi.DisablePortRequest{ID: "spine1/1"})
	assert.NoError(t, err)

	resp, err := subClient.Recv() // Now get the deletion notification
	assert.NoError(t, err)
	assert.Len(t, resp.GetUpdate().Delete, 1)
	assert.Equal(t, "state/link[port=201]", utils.ToString(resp.GetUpdate().Delete[0]))

	// Now re-enable the spine1/1 port...
	t.Log("Enabling port spine1/1...")
	_, err = deviceClient.EnablePort(ctx, &simapi.EnablePortRequest{ID: "spine1/1"})
	assert.NoError(t, err)

	resp, err = subClient.Recv() // Now get the addition notification
	assert.NoError(t, err)
	assert.Equal(t, "201", resp.GetUpdate().Update[0].Path.Elem[1].Key["port"])

	wg2 := sync.WaitGroup{}
	wg2.Add(5) // setting counter to 5 (until at least 5 hosts are found..)

	for i, device := range dresp.Devices {
		go func(id int, d *simapi.Device) {
			SetPipelineConfig(t, id, d, info)
			wg2.Done()
		}(i, device)

		go func(id int) {
			ValidateHostDiscovery(t, id)
			wg2.Done()
		}(i)
	}
	wg2.Wait()
}

// SetPipelineConfig applies the pipeline configuration to the specified device.
func SetPipelineConfig(t *testing.T, id int, device *simapi.Device, info *p4info.P4Info) {
	t.Logf("%d: Setting pipeline configuration...", id)

	dconn, err := CreateInsecureConnection(fmt.Sprintf("fabric-sim:%d", device.ControlPort))
	assert.NoError(t, err)

	p4Client := p4api.NewP4RuntimeClient(dconn)

	// Establish stream and issue mastership
	ctx := context.Background()
	stream, err := p4Client.StreamChannel(ctx)
	assert.NoError(t, err)

	cookie := time.Now().UnixNano()
	electionID := p4utils.TimeBasedElectionID()
	role := p4utils.NewStratumRole(onosRoleName, 0, []byte(onosRoleAgentID), false, true)
	err = stream.Send(p4utils.CreateMastershipArbitration(electionID, role))
	assert.NoError(t, err)

	_, err = stream.Recv()
	assert.NoError(t, err)

	_, err = p4Client.SetForwardingPipelineConfig(ctx, &p4api.SetForwardingPipelineConfigRequest{
		DeviceId:   device.ChassisID,
		Role:       onosRoleName,
		ElectionId: electionID,
		Action:     p4api.SetForwardingPipelineConfigRequest_VERIFY_AND_COMMIT,
		Config: &p4api.ForwardingPipelineConfig{
			P4Info:         info,
			P4DeviceConfig: []byte{0, 1, 2, 3},
			Cookie:         &p4api.ForwardingPipelineConfig_Cookie{Cookie: uint64(cookie)},
		},
	})
	assert.NoError(t, err)
}

// ValidateLinkDiscovery validates that all links get discovered
func ValidateLinkDiscovery(t *testing.T, id int) {
	t.Logf("Creating gNMI connection for agent %d", id)
	gconn, err := CreateInsecureConnection(fmt.Sprintf("discovery-agent-%d.discovery-agent:30000", id))
	assert.NoError(t, err)
	gnmiClient := gnmi.NewGNMIClient(gconn)

	t.Logf("%d: Subscribing for gNMI notifications...", id)
	ctx := context.Background()
	subClient, err := gnmiClient.Subscribe(ctx)
	assert.NoError(t, err)
	err = subClient.Send(&gnmi.SubscribeRequest{
		Request: &gnmi.SubscribeRequest_Subscribe{
			Subscribe: &gnmi.SubscriptionList{
				Subscription:     []*gnmi.Subscription{{Path: utils.ToPath("state/link[port=...]")}},
				Mode:             gnmi.SubscriptionList_STREAM,
				AllowAggregation: true,
			}},
	})
	assert.NoError(t, err)

	// Wait until we get 8 leaf updates total
	for i := 0; i < 8; {
		sresp, err1 := subClient.Recv()
		assert.NoError(t, err1)
		i += len(sresp.GetUpdate().Update)
		//t.Logf("%d: Received update: %+v", id, sresp)
	}

	// Check basic queries to start
	t.Logf("%d: Getting links via gNMI...", id)
	resp, err := gnmiClient.Get(ctx, &gnmi.GetRequest{
		Path: []*gnmi.Path{utils.ToPath("state/link[port=...]")},
	})
	//t.Logf("%d: Received get response: %+v", id, resp)

	assert.NoError(t, err)
	assert.Len(t, resp.Notification, 1)
	assert.Len(t, resp.Notification[0].Update, 4*3) // 4 links, with 2 leaves each
}

// CreateInsecureConnection creates gRPC connection to the specified gRPC end-point
func CreateInsecureConnection(targetAaddress string) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(retry.RetryingUnaryClientInterceptor()),
	}

	conn, err := grpc.Dial(targetAaddress, opts...)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// ValidateHostDiscovery validates that all hosts get discovered
func ValidateHostDiscovery(t *testing.T, id int) {
	t.Logf("Creating gNMI connection for agent %d", id)
	gconn, err := CreateInsecureConnection(fmt.Sprintf("discovery-agent-%d.discovery-agent:30000", id))
	assert.NoError(t, err)
	gnmiClient := gnmi.NewGNMIClient(gconn)

	t.Logf("%d: Subscribing for gNMI notifications...", id)
	ctx := context.Background()
	subClient, err := gnmiClient.Subscribe(ctx)
	assert.NoError(t, err)
	err = subClient.Send(&gnmi.SubscribeRequest{
		Request: &gnmi.SubscribeRequest_Subscribe{
			Subscribe: &gnmi.SubscriptionList{
				Subscription:     []*gnmi.Subscription{{Path: utils.ToPath("state/host[mac=...]")}},
				Mode:             gnmi.SubscriptionList_STREAM,
				AllowAggregation: true,
			}},
	})
	assert.NoError(t, err)

	// Wait until we get 8 host updates total
	for i := 0; i < 8; {
		sresp, err1 := subClient.Recv()
		assert.NoError(t, err1)
		i += len(sresp.GetUpdate().Update)
		t.Logf("%d: Received update: %+v", id, sresp)
	}

	// Check basic queries to start
	t.Logf("%d: Getting hosts via gNMI...", id)
	resp, err := gnmiClient.Get(ctx, &gnmi.GetRequest{
		Path: []*gnmi.Path{utils.ToPath("state/host[mac=...]")},
	})
	t.Logf("%d: Received get response: %+v", id, resp)

	assert.NoError(t, err)
	assert.Len(t, resp.Notification, 1)
	assert.Len(t, resp.Notification[0].Update, 3) // 3 hosts
}
