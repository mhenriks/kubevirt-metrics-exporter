// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package cgroup

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCgroup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cgroup Suite")
}
