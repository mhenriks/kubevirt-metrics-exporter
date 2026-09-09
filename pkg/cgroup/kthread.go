// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// userHZ is the clock tick rate for values in /proc/<pid>/stat.
// It is 100 on all Linux architectures and has been for decades.
const userHZ = 100

// findKernelThreadPID scans procPath for a kernel thread whose
// /proc/<pid>/comm matches the given name exactly.
func findKernelThreadPID(name string, procPath string) (int, error) {
	entries, err := os.ReadDir(procPath)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", procPath, err)
	}

	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		comm, err := os.ReadFile(filepath.Join(procPath, entry.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == name {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("kernel thread %q not found in %s", name, procPath)
}

// readProcessCPUSeconds reads the cumulative CPU time (user + system) in
// seconds for a process from /proc/<pid>/stat. The values are in clock ticks
// (USER_HZ = 100).
func readProcessCPUSeconds(pid int, procPath string) (float64, error) {
	data, err := os.ReadFile(filepath.Join(procPath, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}

	// /proc/<pid>/stat format: pid (comm) state fields...
	// comm can contain spaces and parentheses, so find the last ')'.
	s := string(data)
	closeIdx := strings.LastIndex(s, ")")
	if closeIdx < 0 || closeIdx+2 >= len(s) {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}

	// Fields after "(comm) " are space-separated.
	// Index 11 = utime, index 12 = stime (0-based after the ')').
	fields := strings.Fields(s[closeIdx+2:])
	if len(fields) < 13 {
		return 0, fmt.Errorf("/proc/%d/stat: expected >=13 fields after comm, got %d", pid, len(fields))
	}

	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("/proc/%d/stat: parsing utime: %w", pid, err)
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("/proc/%d/stat: parsing stime: %w", pid, err)
	}

	return float64(utime+stime) / userHZ, nil
}

// readKSMGeneralProfit reads /sys/kernel/mm/ksm/general_profit and returns
// the value in bytes. Returns (0, false) if the file does not exist (kernel
// < 6.4 or KSM not enabled).
func readKSMGeneralProfit(sysPath string) (int64, bool) {
	path := filepath.Join(sysPath, "kernel", "mm", "ksm", "general_profit")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}

	v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
