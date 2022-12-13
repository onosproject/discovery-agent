// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package basic

import (
	"context"
	"fmt"
	"github.com/onosproject/fabric-sim/pkg/topo"
	simapi "github.com/onosproject/onos-api/go/onos/fabricsim"
	"github.com/onosproject/onos-lib-go/pkg/grpc/retry"
	"github.com/onosproject/onos-net-lib/pkg/p4utils"
	p4api "github.com/p4lang/p4runtime/go/p4/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"testing"
	"time"
)

const onosRoleName = "onoslite"

// TestBasics loads fabric simulator with access fabric topology, and tests basic gNMI operations
func (s *TestSuite) TestBasics(t *testing.T) {
	conn, err := CreateInsecureConnection("fabric-sim:5150")
	assert.NoError(t, err)
	defer conn.Close()

	// Wipe-out and load topology
	err = topo.ClearTopology(conn)
	assert.NoError(t, err)
	err = topo.LoadTopology(conn, "test/basic/plain_small.yaml")
	assert.NoError(t, err)

	// Retrieve device information from the fabric simulator
	deviceClient := simapi.NewDeviceServiceClient(conn)
	ctx := context.Background()
	dresp, err := deviceClient.GetDevices(ctx, &simapi.GetDevicesRequest{})
	assert.NoError(t, err)

	// Load pipeline config and apply it to all devices
	info, err := p4utils.LoadP4Info("test/basic/p4info.txt")
	assert.NoError(t, err)

	for _, device := range dresp.Devices {
		dconn, err := CreateInsecureConnection(fmt.Sprintf("fabric-sim:%d", device.ControlPort))
		assert.NoError(t, err)

		p4Client := p4api.NewP4RuntimeClient(dconn)

		// Establish stream and issue mastership
		stream, err := p4Client.StreamChannel(ctx)
		assert.NoError(t, err)

		cookie := time.Now().UnixNano()
		electionID := p4utils.TimeBasedElectionID()
		role := p4utils.NewStratumRole(onosRoleName, 0, []byte("\x01"), false, true)
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

	//startAgents(t, dresp.Devices, 30000)
}

//func startAgents(t *testing.T, devices []*simapi.Device, basePort int) {
//	for i, device := range devices {
//		err := startAgent(i, device.ControlPort, basePort+i)
//		assert.NoError(t, err)
//	}
//}
//
//func stopAgents(t *testing.T, devices []*simapi.Device) {
//	for i, _ := range devices {
//		err := stopAgent(i)
//		assert.NoError(t, err)
//	}
//}
//
//var (
//	startCommand = "-n test run link-local-agent-%d --image=onosproject/link-agent:latest --image-pull-policy=Never -- --bind-port=%d --target-address=fabric-sim:%d"
//	stopCommand  = "-n test delete pod link-local-agent-%d"
//)
//
//func startAgent(id int, controlPort int32, gNMIPort int) error {
//	args := strings.Split(fmt.Sprintf(startCommand, id, gNMIPort, controlPort), " ")
//	cmd := exec.Command("kubectl", args...)
//	return cmd.Wait()
//}
//
//func stopAgent(id int) error {
//	args := strings.Split(fmt.Sprintf(stopCommand, id), " ")
//	cmd := exec.Command("kubectl", args...)
//	return cmd.Wait()
//}

// CreateInsecureConnection creates gRPC connection to the specified gRPC end-point
func CreateInsecureConnection(targetAaddress string) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		//grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(retry.RetryingUnaryClientInterceptor()),
	}

	conn, err := grpc.Dial(targetAaddress, opts...)
	if err != nil {
		return nil, err
	}

	return conn, nil
}
