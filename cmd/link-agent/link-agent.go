// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

// Package main is an entry point for launching the link agent
package main

import (
	"github.com/onosproject/link-agent/pkg/manager"
	"github.com/onosproject/onos-lib-go/pkg/cli"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/spf13/cobra"
)

var log = logging.GetLogger()

const (
	uuidFlag          = "uuid"
	targetAddressFlag = "target-address"
)

// The main entry point
func main() {
	cmd := &cobra.Command{
		Use:  "link-agent",
		RunE: runRootCommand,
	}
	cmd.Flags().String(uuidFlag, "", "externally assigned UUID of this agent; if omitted, one will be auto-generated")
	cmd.Flags().String(targetAddressFlag, "", "address:port or just :port of the stratum agent")
	cli.AddServiceEndpointFlags(cmd, "link agent gNMI")
	cli.Run(cmd)
}

func runRootCommand(cmd *cobra.Command, args []string) error {
	agentUUID, _ := cmd.Flags().GetString(uuidFlag)
	targetAddress, _ := cmd.Flags().GetString(targetAddressFlag)

	flags, err := cli.ExtractServiceEndpointFlags(cmd)
	if err != nil {
		return err
	}

	log.Infof("Starting link-agent")
	cfg := manager.Config{
		AgentUUID:     agentUUID,
		TargetAddress: targetAddress,
		ServiceFlags:  flags,
	}
	return cli.RunDaemon(manager.NewManager(cfg))
}
