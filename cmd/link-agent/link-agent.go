// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/onosproject/link-agent/pkg/manager"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"syscall"
)

var log = logging.GetLogger()

// The main entry point
func main() {
	if err := getRootCommand().Execute(); err != nil {
		println(err)
		os.Exit(1)
	}
}

func getRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link-agent",
		Short: "link-agent",
		RunE:  runRootCommand,
	}
	cmd.Flags().String("uuid", "", "externally assigned UUID of this agent; if omitted, one will be auto-generated")
	cmd.Flags().String("target-address", "", "address:port or just :port of the stratum agent")
	cmd.Flags().Int("bind-port", 0, "listen TCP port of the link agent gNMI service")
	cmd.Flags().Bool("no-tls", true, "if set, do not use TLS for link agent gNMI service")
	cmd.Flags().String("caPath", "", "path to CA certificate")
	cmd.Flags().String("keyPath", "", "path to client private key")
	cmd.Flags().String("certPath", "", "path to client certificate")
	return cmd
}

func runRootCommand(cmd *cobra.Command, args []string) error {
	agentUUID, _ := cmd.Flags().GetString("uuid")
	tcpPort, _ := cmd.Flags().GetInt("bind-port")
	targetAddress, _ := cmd.Flags().GetString("target-address")
	noTLS, _ := cmd.Flags().GetBool("no-tls")
	caPath, _ := cmd.Flags().GetString("caPath")
	keyPath, _ := cmd.Flags().GetString("keyPath")
	certPath, _ := cmd.Flags().GetString("certPath")

	log.Infof("Starting link-agent")

	cfg := manager.Config{
		AgentUUID:     agentUUID,
		CAPath:        caPath,
		KeyPath:       keyPath,
		CertPath:      certPath,
		GRPCPort:      tcpPort,
		NoTLS:         noTLS,
		TargetAddress: targetAddress,
	}

	mgr := manager.NewManager(cfg)

	mgr.Run()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	mgr.Close()
	return nil
}
