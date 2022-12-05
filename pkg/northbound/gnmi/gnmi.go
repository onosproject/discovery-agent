// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package gnmi implements gNMI service for accessing discovered links
package gnmi

import (
	"context"
	"github.com/openconfig/gnmi/proto/gnmi"
)

// Capabilities allows the client to retrieve the set of capabilities that
// is supported by the target. This allows the target to validate the
// service version that is implemented and retrieve the set of models that
// the target supports. The models can then be specified in subsequent RPCs
// to restrict the set of data that is utilized.
// Reference: gNMI Specification Section 3.2
func (s *Server) Capabilities(ctx context.Context, request *gnmi.CapabilityRequest) (*gnmi.CapabilityResponse, error) {
	// TODO: populate appropriately with supported models; for now, this is not required
	modelData := make([]*gnmi.ModelData, 0)
	return &gnmi.CapabilityResponse{
		SupportedModels:    modelData,
		SupportedEncodings: []gnmi.Encoding{gnmi.Encoding_PROTO, gnmi.Encoding_JSON_IETF},
		GNMIVersion:        "0.8.0",
	}, nil
}

// Get retrieves a snapshot of data from the target. A Get RPC requests that the
// target snapshots a subset of the data tree as specified by the paths
// included in the message and serializes this to be returned to the
// client using the specified encoding.
// Reference: gNMI Specification Section 3.3
func (s *Server) Get(ctx context.Context, request *gnmi.GetRequest) (*gnmi.GetResponse, error) {
	return &gnmi.GetResponse{
		Notification: nil,
	}, nil
}

// Set allows the client to modify the state of data on the target. The
// paths to modified along with the new values that the client wishes
// to set the value to.
// Reference: gNMI Specification Section 3.4
func (s *Server) Set(ctx context.Context, request *gnmi.SetRequest) (*gnmi.SetResponse, error) {
	return &gnmi.SetResponse{
		Prefix:    request.Prefix,
		Response:  nil,
		Timestamp: 0,
	}, nil
}

// State related to a single message stream
type streamState struct {
	stream          gnmi.GNMI_SubscribeServer
	req             *gnmi.SubscribeRequest
	streamResponses chan *gnmi.SubscribeResponse
}

// Subscribe allows a client to request the target to send it values
// of particular paths within the data tree. These values may be streamed
// at a particular cadence (STREAM), sent one off on a long-lived channel
// (POLL), or sent as a one-off retrieval (ONCE).
// Reference: gNMI Specification Section 3.5
func (s *Server) Subscribe(server gnmi.GNMI_SubscribeServer) error {
	return nil
}
