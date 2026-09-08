// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package vmi

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestVMI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VMI Suite")
}
