// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package cgroup

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type vmstatTHP struct {
	splitPMD      uint64
	collapseAlloc uint64
}

// readVMStatTHP reads thp_split_pmd and thp_collapse_alloc from /proc/vmstat.
func readVMStatTHP(procPath string) (*vmstatTHP, error) {
	f, err := os.Open(filepath.Join(procPath, "vmstat"))
	if err != nil {
		return nil, fmt.Errorf("opening vmstat: %w", err)
	}
	defer f.Close()

	var (
		splitPMD      uint64
		collapseAlloc uint64
		splitSet      bool
		collapseSet   bool
	)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 2 {
			continue
		}
		v, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		switch parts[0] {
		case "thp_split_pmd":
			splitPMD = v
			splitSet = true
		case "thp_collapse_alloc":
			collapseAlloc = v
			collapseSet = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading vmstat: %w", err)
	}
	if !splitSet && !collapseSet {
		return nil, fmt.Errorf("vmstat: thp_split_pmd and thp_collapse_alloc not found")
	}

	return &vmstatTHP{
		splitPMD:      splitPMD,
		collapseAlloc: collapseAlloc,
	}, nil
}
