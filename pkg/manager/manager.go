// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package manager contains the link agent manager coordinating lifecycle of the NB API and link discovery controller
package manager

import (
	"github.com/google/uuid"
	"github.com/onosproject/link-agent/pkg/linkdiscovery"
	"github.com/onosproject/link-agent/pkg/northbound/gnmi"
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
	GRPCPort      int
	NoTLS         bool
	CAPath        string
	KeyPath       string
	CertPath      string
}

// Manager single point of entry for the link-agent
type Manager struct {
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

// Run runs manager
func (m *Manager) Run() {
	log.Infof("Starting Manager... UUID: %s", m.Config.AgentUUID)

	if err := m.Start(); err != nil {
		log.Fatalw("Unable to run Manager", "error", err)
	}
}

// Start initializes and starts the link controller and the NB gNMI API.
func (m *Manager) Start() error {
	// Load (or generate and save) our UUID
	if len(m.Config.AgentUUID) == 0 {
		m.Config.AgentUUID = m.loadOrCreateUUID()
	}

	// If the incoming configuration is insufficient, attempt to get needed info from file
	if m.Config.GRPCPort == 0 || len(m.Config.TargetAddress) == 0 {
		m.Config.GRPCPort, m.Config.TargetAddress = readArgsFile()
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

// startSouthboundServer starts the northbound gRPC server
func (m *Manager) startNorthboundServer() error {
	cfg := northbound.NewInsecureServerConfig(int16(m.Config.GRPCPort))
	if !m.Config.NoTLS {
		northbound.NewServerCfg(m.Config.CAPath, m.Config.KeyPath, m.Config.CertPath, int16(m.Config.GRPCPort),
			true, northbound.SecurityConfig{})
	}
	s := northbound.NewServer(cfg)
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

// Close kills the manager
func (m *Manager) Close() {
	log.Infow("Closing Manager")
	m.Controller.Stop()
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
