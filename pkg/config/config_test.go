// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parseBoundariesNs", func() {
	It("should parse comma-separated nanosecond boundaries", func() {
		result := parseBoundariesNs("10000000,100000000,1000000000")
		Expect(result).To(HaveLen(3))
		Expect(result[0]).To(Equal(int64(10000000)))
	})
})

var _ = Describe("parseBoundariesSeconds", func() {
	It("should convert nanosecond boundaries to seconds", func() {
		result := parseBoundariesSeconds("10000000,100000000,1000000000")
		Expect(result).To(HaveLen(3))
		Expect(result[0]).To(Equal(0.01))
		Expect(result[1]).To(Equal(0.1))
		Expect(result[2]).To(Equal(1.0))
	})
})

var _ = Describe("ParseNamespaces", func() {
	DescribeTable("should parse namespace strings",
		func(input string, want []string) {
			got := ParseNamespaces(input)
			if want == nil {
				Expect(got).To(BeNil())
			} else {
				Expect(got).To(Equal(want))
			}
		},
		Entry("empty string", "", nil),
		Entry("single namespace", "default", []string{"default"}),
		Entry("two namespaces", "default,production", []string{"default", "production"}),
		Entry("with spaces", " default , production ", []string{"default", "production"}),
		Entry("empty segment skipped", "ns1,,ns2", []string{"ns1", "ns2"}),
	)
})

var _ = Describe("Config.Validate", func() {
	var valid *Config

	BeforeEach(func() {
		valid = &Config{
			NodeName:         "worker-1",
			BoundariesNs:     []int64{10000000},
			Boundaries:       []float64{0.01},
			EBPFScanInterval: 30,
		}
	})

	It("should accept a valid config", func() {
		Expect(valid.Validate()).To(Succeed())
	})

	DescribeTable("should reject invalid configs",
		func(modify func(*Config)) {
			c := *valid
			modify(&c)
			Expect(c.Validate()).To(HaveOccurred())
		},
		Entry("empty node name", func(c *Config) { c.NodeName = "" }),
		Entry("empty boundaries", func(c *Config) { c.BoundariesNs = nil }),
		Entry("zero scan interval with ebpf enabled", func(c *Config) { c.EnableEBPF = true; c.EBPFScanInterval = 0 }),
	)
})
