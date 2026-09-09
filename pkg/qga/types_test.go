// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package qga

import (
	"math"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const realWMICOutput = `Node,AvgDiskReadQueueLength,AvgDiskWriteQueueLength,DiskReadsPerSec,DiskWritesPerSec,Name,Timestamp_Sys100NS
WIN-L5SM2M3JFBO,336223056,161462865009,42946,752955,0 C:,134280977011680000
WIN-L5SM2M3JFBO,63287115,9573783964885,63,610780,1 E:,134280977011680000
WIN-L5SM2M3JFBO,434565,256605565134,42,1678184,2 F:,134280977011680000
WIN-L5SM2M3JFBO,399944736,9991852395028,43051,3041919,_Total,134280977011680000
`

var _ = Describe("ParseWMICSV", func() {
	It("should parse real WMIC output and exclude _Total", func() {
		counters, err := ParseWMICSV([]byte(realWMICOutput))
		Expect(err).NotTo(HaveOccurred())
		Expect(counters).To(HaveLen(3))

		expected := map[string]DiskCounters{
			"0 C:": {Name: "0 C:", RdQueueLen: 336223056, WrQueueLen: 161462865009, RdOps: 42946, WrOps: 752955, Timestamp100ns: 134280977011680000},
			"1 E:": {Name: "1 E:", RdQueueLen: 63287115, WrQueueLen: 9573783964885, RdOps: 63, WrOps: 610780, Timestamp100ns: 134280977011680000},
			"2 F:": {Name: "2 F:", RdQueueLen: 434565, WrQueueLen: 256605565134, RdOps: 42, WrOps: 1678184, Timestamp100ns: 134280977011680000},
		}
		for _, dc := range counters {
			Expect(expected).To(HaveKey(dc.Name))
			Expect(dc).To(Equal(expected[dc.Name]))
		}
	})

	It("should skip _Total rows", func() {
		counters, err := ParseWMICSV([]byte(realWMICOutput))
		Expect(err).NotTo(HaveOccurred())
		for _, dc := range counters {
			Expect(dc.Name).NotTo(Equal("_Total"))
		}
	})

	It("should accept case-insensitive headers", func() {
		input := `NODE,AVGDISKREADQUEUELENGTH,AVGDISKWRITEQUEUELENGTH,DISKREADSPERSEC,DISKWRITESPERSEC,NAME,TIMESTAMP_SYS100NS
HOST1,1000,2000,10,20,0 C:,100000000
`
		counters, err := ParseWMICSV([]byte(input))
		Expect(err).NotTo(HaveOccurred())
		Expect(counters).To(HaveLen(1))
		Expect(counters[0].RdQueueLen).To(Equal(uint64(1000)))
		Expect(counters[0].WrQueueLen).To(Equal(uint64(2000)))
	})

	It("should handle different column order", func() {
		input := `Node,Name,Timestamp_Sys100NS,DiskWritesPerSec,AvgDiskWriteQueueLength,DiskReadsPerSec,AvgDiskReadQueueLength
HOST1,0 C:,100000000,20,2000,10,1000
`
		counters, err := ParseWMICSV([]byte(input))
		Expect(err).NotTo(HaveOccurred())
		Expect(counters).To(HaveLen(1))
		dc := counters[0]
		Expect(dc.Name).To(Equal("0 C:"))
		Expect(dc.RdQueueLen).To(Equal(uint64(1000)))
		Expect(dc.WrQueueLen).To(Equal(uint64(2000)))
		Expect(dc.RdOps).To(Equal(uint64(10)))
		Expect(dc.WrOps).To(Equal(uint64(20)))
		Expect(dc.Timestamp100ns).To(Equal(uint64(100000000)))
	})

	It("should handle Windows line endings", func() {
		input := "Node,Name,AvgDiskReadQueueLength,AvgDiskWriteQueueLength,DiskReadsPerSec,DiskWritesPerSec,Timestamp_Sys100NS\r\nHOST1,0 C:,1000,2000,10,20,100000000\r\n"
		counters, err := ParseWMICSV([]byte(input))
		Expect(err).NotTo(HaveOccurred())
		Expect(counters).To(HaveLen(1))
	})

	It("should handle PowerShell quoted format", func() {
		input := `"Name","AvgDiskReadQueueLength","AvgDiskWriteQueueLength","DiskReadsPerSec","DiskWritesPerSec","Timestamp_Sys100NS"
"0 C:","336223056","161462865009","42946","752955","134280977011680000"
"1 E:","63287115","9573783964885","63","610780","134280977011680000"
"_Total","399944736","9991852395028","43051","3041919","134280977011680000"
`
		counters, err := ParseWMICSV([]byte(input))
		Expect(err).NotTo(HaveOccurred())
		Expect(counters).To(HaveLen(2))
		Expect(counters[0].Name).To(Equal("0 C:"))
		Expect(counters[0].RdQueueLen).To(Equal(uint64(336223056)))
		Expect(counters[1].WrQueueLen).To(Equal(uint64(9573783964885)))
	})

	It("should return error for missing columns", func() {
		input := `Node,Name,AvgDiskReadQueueLength,DiskReadsPerSec
HOST1,0 C:,1000,10
`
		_, err := ParseWMICSV([]byte(input))
		Expect(err).To(HaveOccurred())
	})

	It("should return error when there are no data rows", func() {
		input := `Node,Name,AvgDiskReadQueueLength,AvgDiskWriteQueueLength,DiskReadsPerSec,DiskWritesPerSec,Timestamp_Sys100NS
`
		_, err := ParseWMICSV([]byte(input))
		Expect(err).To(HaveOccurred())
	})

	It("should return error when only _Total rows exist", func() {
		input := `Node,Name,AvgDiskReadQueueLength,AvgDiskWriteQueueLength,DiskReadsPerSec,DiskWritesPerSec,Timestamp_Sys100NS
HOST1,_Total,1000,2000,10,20,100000000
`
		_, err := ParseWMICSV([]byte(input))
		Expect(err).To(HaveOccurred())
	})

	It("should return error for empty input", func() {
		_, err := ParseWMICSV([]byte(""))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ComputeMetrics", func() {
	It("should compute latency and IOPS correctly", func() {
		prev := DiskCounters{
			Name: "0 C:", RdQueueLen: 10000000, WrQueueLen: 20000000,
			RdOps: 100, WrOps: 200, Timestamp100ns: 100000000000,
		}
		curr := DiskCounters{
			Name: "0 C:", RdQueueLen: 11000000, WrQueueLen: 24000000,
			RdOps: 200, WrOps: 400, Timestamp100ns: 100050000000,
		}

		m := ComputeMetrics(prev, curr)

		Expect(math.Abs(m.ElapsedSec - 5.0)).To(BeNumerically("<", 0.001))
		Expect(math.Abs(m.RdLatSec - 1000000.0/100.0/1e7)).To(BeNumerically("<", 1e-12))
		Expect(math.Abs(m.RdIOPS - 20.0)).To(BeNumerically("<", 0.001))
		Expect(math.Abs(m.WrLatSec - 4000000.0/200.0/1e7)).To(BeNumerically("<", 1e-12))
		Expect(math.Abs(m.WrIOPS - 40.0)).To(BeNumerically("<", 0.001))
	})

	It("should match real-world high-latency write scenario", func() {
		prev := DiskCounters{
			Name: "1 E:", RdQueueLen: 63287115, WrQueueLen: 9573783964885,
			RdOps: 63, WrOps: 610780, Timestamp100ns: 134280977011680000,
		}
		curr := DiskCounters{
			Name: "1 E:", RdQueueLen: 63287115, WrQueueLen: 9684684916507,
			RdOps: 63, WrOps: 617717, Timestamp100ns: 134280977445284998,
		}

		m := ComputeMetrics(prev, curr)

		Expect(m.ElapsedSec).To(BeNumerically("~", 43.36, 1.0))
		Expect(m.WrLatSec).To(BeNumerically("~", 1.6, 0.1))
		Expect(m.WrIOPS).To(BeNumerically("~", 160.0, 5.0))
		Expect(m.RdIOPS).To(Equal(0.0))
		Expect(m.RdLatSec).To(Equal(0.0))
	})

	It("should produce zero metrics when there is no activity", func() {
		prev := DiskCounters{
			Name: "0 C:", RdQueueLen: 1000, WrQueueLen: 2000,
			RdOps: 100, WrOps: 200, Timestamp100ns: 100000000000,
		}
		curr := DiskCounters{
			Name: "0 C:", RdQueueLen: 1000, WrQueueLen: 2000,
			RdOps: 100, WrOps: 200, Timestamp100ns: 100050000000,
		}

		m := ComputeMetrics(prev, curr)

		Expect(m.RdLatSec).To(Equal(0.0))
		Expect(m.WrLatSec).To(Equal(0.0))
		Expect(m.RdIOPS).To(Equal(0.0))
		Expect(m.WrIOPS).To(Equal(0.0))
		Expect(math.Abs(m.ElapsedSec - 5.0)).To(BeNumerically("<", 0.001))
	})

	It("should produce zero metrics when timestamps are equal", func() {
		prev := DiskCounters{Name: "0 C:", Timestamp100ns: 100000000000}
		curr := DiskCounters{Name: "0 C:", Timestamp100ns: 100000000000}

		m := ComputeMetrics(prev, curr)

		Expect(m.ElapsedSec).To(Equal(0.0))
		Expect(m.RdIOPS).To(Equal(0.0))
		Expect(m.WrIOPS).To(Equal(0.0))
	})

	It("should produce non-zero read metrics and zero write metrics for read-only activity", func() {
		prev := DiskCounters{Name: "1 E:", RdQueueLen: 0, RdOps: 0, WrQueueLen: 0, WrOps: 0, Timestamp100ns: 100000000000}
		curr := DiskCounters{Name: "1 E:", RdQueueLen: 500000, RdOps: 50, WrQueueLen: 0, WrOps: 0, Timestamp100ns: 100100000000}

		m := ComputeMetrics(prev, curr)

		Expect(m.RdIOPS).NotTo(Equal(0.0))
		Expect(m.WrIOPS).To(Equal(0.0))
		Expect(m.WrLatSec).To(Equal(0.0))
	})
})
