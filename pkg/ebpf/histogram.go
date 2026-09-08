// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package ebpf

import "math"

const MaxSlots = 26

var slotCeiling [MaxSlots]float64

func init() {
	for i := 0; i < MaxSlots; i++ {
		slotCeiling[i] = float64(uint64(1)<<uint(i)) / 1_000_000.0
	}
}

func SlotsToConstHistogram(slots [MaxSlots]uint64, promBuckets []float64) (count uint64, sum float64, buckets map[float64]uint64) {
	var runningTotal [MaxSlots]uint64
	var cumulative uint64

	for i := 0; i < MaxSlots; i++ {
		cumulative += slots[i]
		count += slots[i]
		runningTotal[i] = cumulative

		var midpointUS float64
		if i == 0 {
			midpointUS = 0.5
		} else {
			midpointUS = float64(uint64(1)<<uint(i-1)) * math.Sqrt2
		}
		sum += float64(slots[i]) * (midpointUS / 1_000_000.0)
	}

	buckets = make(map[float64]uint64, len(promBuckets))
	for _, b := range promBuckets {
		slot := -1
		for i := 0; i < MaxSlots; i++ {
			if slotCeiling[i] <= b {
				slot = i
			}
		}
		if slot >= 0 {
			buckets[b] = runningTotal[slot]
		} else {
			buckets[b] = 0
		}
	}

	return count, sum, buckets
}
