// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDevice(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Device Suite")
}
