// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package gnmi implements gNMI service for accessing discovered links
package gnmi

import (
	"github.com/onosproject/discovery-agent/pkg/discovery"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-lib-go/pkg/northbound"
	"github.com/onosproject/onos-net-lib/pkg/gnmiserver"
	gnmiapi "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
)

var log = logging.GetLogger("northbound", "gnmi")

// Service implements the link agent NB gRPC
type Service struct {
	northbound.Service
	controller *discovery.Controller
}

// NewService allocates a Service struct with the given parameters
func NewService(controller *discovery.Controller) Service {
	return Service{controller: controller}
}

// Register registers the server with grpc
func (s Service) Register(r *grpc.Server) {
	server := gnmiserver.NewGNMIServer(&s.controller.GNMIConfigurable, "discovery-agent")
	gnmiapi.RegisterGNMIServer(r, server)
	log.Debug("gNMI API services registered")
}
