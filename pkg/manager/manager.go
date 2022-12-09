// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	"github.com/google/uuid"
	"github.com/onosproject/link-agent/pkg/linkdiscovery"
	"github.com/onosproject/link-agent/pkg/northbound/gnmi"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/onosproject/onos-lib-go/pkg/northbound"
	"io/ioutil"
	"time"
)

var log = logging.GetLogger("manager")

// Config is a manager configuration
type Config struct {
	CAPath        string
	KeyPath       string
	CertPath      string
	GRPCPort      int
	NoTLS         bool
	TargetAddress string
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
	log.Infow("Starting Manager")

	if err := m.Start(); err != nil {
		log.Fatalw("Unable to run Manager", "error", err)
	}
}

// Start initializes and starts the link controller and the NB gNMI API.
func (m *Manager) Start() error {
	// Load (or generate and save) our UUID
	agentID := m.loadOrCreateUUID()

	// TODO: make this retrievable from a file
	config := &linkdiscovery.Config{
		EmitFrequency:               5 * time.Second,
		MaxLinkAge:                  30 * time.Second,
		PipelineValidationFrequency: 60 * time.Second,
		PortRediscoveryFrequency:    60 * time.Second,
		LinkPruneFrequency:          2 * time.Second,
	}

	// Initialize and start the link discovery controller
	m.Controller = linkdiscovery.NewController(m.Config.TargetAddress, agentID, config)
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

const uuidFile = "/opt/link-agent/uuid"

func (m *Manager) loadOrCreateUUID() string {
	if b, err := ioutil.ReadFile(uuidFile); err == nil {
		return string(b)
	}

	newUUID := uuid.New().String()
	if err := ioutil.WriteFile(uuidFile, []byte(newUUID), 0644); err != nil {
		log.Fatalf("Unable to save UUID: %+v", err)
	}
	return newUUID
}
