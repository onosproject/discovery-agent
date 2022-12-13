// SPDX-FileCopyrightText: 2022-present Intel Corporation
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"github.com/onosproject/helmit/pkg/registry"
	"github.com/onosproject/helmit/pkg/test"
	"github.com/onosproject/link-agent/test/basic"
)

func main() {
	registry.RegisterTestSuite("basic", &basic.TestSuite{})
	test.Main()
}
