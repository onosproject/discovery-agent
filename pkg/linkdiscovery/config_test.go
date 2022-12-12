// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package linkdiscovery

import (
	"errors"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func getTestConfig() *Config {
	configFile = "/tmp/config.yaml"
	return loadConfig()
}

func Test_LoadDefaultConfig(t *testing.T) {
	config := getTestConfig()
	assert.Equal(t, int64(5), config.EmitFrequency)
	assert.Equal(t, int64(2), config.LinkPruneFrequency)
}

func Test_SaveAndLoadConfig(t *testing.T) {
	config := getTestConfig()
	assert.Equal(t, int64(5), config.EmitFrequency)

	config.EmitFrequency = 7
	saveConfig(config)
	defer os.Remove(configFile)

	_, err := os.Stat(configFile)
	assert.False(t, errors.Is(err, os.ErrNotExist))

	config = getTestConfig()
	assert.Equal(t, int64(7), config.EmitFrequency)
}

func TestController_UpdateConfig(t *testing.T) {
	configFile = "/tmp/config.yaml"
	defer os.Remove(configFile)
	//controller := NewController("none", "123")

	//controller.Root.AddPath("config/maxLinkAge",
	//	&gnmi.TypedValue{Value: &gnmi.TypedValue_IntVal{IntVal: 42}})
	//controller.UpdateConfig()
	//assert.Equal(t, int64(42), controller.config.MaxLinkAge)
}
