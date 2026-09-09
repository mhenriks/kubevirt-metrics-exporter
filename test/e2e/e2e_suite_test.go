// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-virtualization/kubevirt-metrics-exporter/test/utils"
)

const (
	exporterNamespace = "kubevirt-metrics-exporter"
	testNamespace     = "e2e-test"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = BeforeSuite(func() {
	By("creating test namespace")
	_, err := utils.Kubectl("create", "namespace", testNamespace)
	Expect(err).NotTo(HaveOccurred())

	By("deploying test PVC and I/O generator pod")
	Expect(utils.KubectlApply("test/e2e/testdata/test-pvc-pod.yaml")).To(Succeed())

	By("waiting for I/O generator pod to be running")
	Expect(utils.WaitForPodRunning("e2e-io-generator", testNamespace, 120*time.Second)).To(Succeed())
})

var _ = AfterSuite(func() {
	By("cleaning up test namespace")
	utils.Kubectl("delete", "namespace", testNamespace, "--ignore-not-found")
})
