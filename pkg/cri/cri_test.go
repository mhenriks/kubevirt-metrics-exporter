// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package cri

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCRI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CRI Suite")
}

var _ = Describe("parsePID", func() {
	Context("with CRI-O format", func() {
		It("should extract PID from top-level pid field", func() {
			info := map[string]string{
				"info": `{"pid": 12345, "sandboxID": "abc123"}`,
			}
			pid, err := parsePID(info)
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(12345))
		})
	})

	Context("with containerd format", func() {
		It("should extract PID from init_pid field", func() {
			info := map[string]string{
				"info": `{"init_pid": 67890}`,
			}
			pid, err := parsePID(info)
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(67890))
		})
	})

	Context("with missing info key", func() {
		It("should return an error", func() {
			info := map[string]string{
				"other": `{"pid": 12345}`,
			}
			_, err := parsePID(info)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no 'info' key"))
		})
	})

	Context("with no PID field", func() {
		It("should return an error", func() {
			info := map[string]string{
				"info": `{"sandboxID": "abc123"}`,
			}
			_, err := parsePID(info)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("with zero PID", func() {
		It("should return an error", func() {
			info := map[string]string{
				"info": `{"pid": 0}`,
			}
			_, err := parsePID(info)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("with invalid JSON", func() {
		It("should return an error", func() {
			info := map[string]string{
				"info": `not json`,
			}
			_, err := parsePID(info)
			Expect(err).To(HaveOccurred())
		})
	})
})
