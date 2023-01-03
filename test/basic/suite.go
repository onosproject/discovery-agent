// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package basic is a suite of basic functionality tests for the fabric simulator
package basic

import (
	"github.com/onosproject/fabric-sim/pkg/topo"
	"github.com/onosproject/helmit/pkg/helm"
	"github.com/onosproject/helmit/pkg/input"
	"github.com/onosproject/helmit/pkg/test"
	"github.com/onosproject/onos-test/pkg/onostest"
)

type testSuite struct {
	test.Suite
}

// TestSuite is the basic test suite
type TestSuite struct {
	testSuite
}

const fabricSimComponentName = "fabric-sim"
const linkLocalAgentComponentName = "link-local-agent"

// SetupTestSuite sets up the link agent basic test suite using fabric-sim
func (s *TestSuite) SetupTestSuite(c *input.Context) error {
	registry := c.GetArg("registry").String("")
	err := helm.Chart(fabricSimComponentName, onostest.OnosChartRepo).
		Release(fabricSimComponentName).
		Set("image.tag", "latest").
		Set("global.image.registry", registry).
		Install(true)
	if err != nil {
		return err
	}

	err = helm.Chart(linkLocalAgentComponentName, onostest.OnosChartRepo).
		Release(linkLocalAgentComponentName).
		Set("image.tag", "latest").
		Set("global.image.registry", registry).
		Set("agent.count", 4). // There are 4 devices in topo.yaml topology file
		Install(true)
	if err != nil {
		return err
	}

	conn, err := CreateInsecureConnection("fabric-sim:5150")
	if err != nil {
		return err
	}
	defer conn.Close()

	// Load topology
	return topo.LoadTopology(conn, "test/basic/topo.yaml")
}
