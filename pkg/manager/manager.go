// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package manager contains the link agent manager coordinating lifecycle of the NB API and link discovery controller
package manager

import (
	"github.com/google/uuid"
	"github.com/onosproject/link-agent/pkg/linkdiscovery"
	"github.com/onosproject/link-agent/pkg/northbound/gnmi"
	"github.com/onosproject/onos-lib-go/pkg/cli"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-lib-go/pkg/northbound"
	"os"
	"strconv"
	"strings"
)

var log = logging.GetLogger("manager")

// Config is a manager configuration
type Config struct {
	AgentUUID     string
	TargetAddress string
	ServiceFlags  *cli.ServiceEndpointFlags
}

// Manager single point of entry for the link-agent
type Manager struct {
	cli.Daemon
	Config     Config
	Controller *linkdiscovery.Controller
}

// NewManager initializes the application manager
func NewManager(cfg Config) *Manager {
	log.Infow("Creating manager")
	mgr := Manager{
		Config: cfg,
	}
	return &mgr
}

// Start initializes and starts the link controller and the NB gNMI API.
func (m *Manager) Start() error {
	log.Info("Starting Manager")

	// Load (or generate and save) our UUID
	if len(m.Config.AgentUUID) == 0 {
		m.Config.AgentUUID = m.loadOrCreateUUID()
	}

	// If the incoming configuration is insufficient, attempt to get needed info from file
	if m.Config.ServiceFlags.BindPort == 0 || len(m.Config.TargetAddress) == 0 {
		m.Config.ServiceFlags.BindPort, m.Config.TargetAddress = readArgsFile()
	}

	// Initialize and start the link discovery controller
	m.Controller = linkdiscovery.NewController(m.Config.TargetAddress, m.Config.AgentUUID)
	m.Controller.Start()

	// Starts NB server
	err := m.startNorthboundServer()
	if err != nil {
		return err
	}
	return nil
}

// Stop stops the manager
func (m *Manager) Stop() {
	log.Infow("Stopping Manager")
	m.Controller.Stop()
}

// startSouthboundServer starts the northbound gRPC server
func (m *Manager) startNorthboundServer() error {
	s := northbound.NewServer(cli.ServerConfigFromFlags(m.Config.ServiceFlags, northbound.SecurityConfig{}))
	s.AddService(logging.Service{})
	s.AddService(gnmi.NewService(m.Controller))

	doneCh := make(chan error)
	go func() {
		err := s.Serve(func(started string) {
			log.Info("Started NBI on ", started)
			close(doneCh)
		})
		if err != nil {
			doneCh <- err
		}
	}()
	return <-doneCh
}

const argsFile = "/etc/link-agent/args"
const uuidFile = "/etc/link-agent/uuid"

func (m *Manager) loadOrCreateUUID() string {
	if b, err := os.ReadFile(uuidFile); err == nil {
		return string(b)
	}

	newUUID := uuid.New().String()
	if err := os.WriteFile(uuidFile, []byte(newUUID), 0644); err != nil {
		log.Fatalf("Unable to save UUID: %+v", err)
	}
	return newUUID
}

func readArgsFile() (int, string) {
	log.Infof("Reading args from file: %s", argsFile)
	b, err := os.ReadFile(argsFile)
	if err != nil {
		log.Fatalf("Unable to read args file: %+v", err)
	}
	args := strings.Split(strings.Trim(string(b), " \n"), " ")
	bindPort, _ := strconv.ParseInt(args[0], 10, 16)
	return int(bindPort), args[1]
}
