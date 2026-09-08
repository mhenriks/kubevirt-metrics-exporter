// Copyright 2026 The KubeVirt Metrics Exporter Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type MountEntry struct {
	MountID    int
	ParentID   int
	Major      uint32
	Minor      uint32
	Root       string
	MountPoint string
	FSType     string
	Source     string
}

func ParseMountInfo(path string) ([]MountEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	var entries []MountEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		entry, err := parseMountInfoLine(scanner.Text())
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

func parseMountInfoLine(line string) (MountEntry, error) {
	fields := strings.Fields(line)
	if len(fields) < 7 {
		return MountEntry{}, fmt.Errorf("too few fields")
	}

	mountID, err := strconv.Atoi(fields[0])
	if err != nil {
		return MountEntry{}, err
	}
	parentID, err := strconv.Atoi(fields[1])
	if err != nil {
		return MountEntry{}, err
	}

	devParts := strings.SplitN(fields[2], ":", 2)
	if len(devParts) != 2 {
		return MountEntry{}, fmt.Errorf("invalid device %q", fields[2])
	}
	major, err := strconv.ParseUint(devParts[0], 10, 32)
	if err != nil {
		return MountEntry{}, err
	}
	minor, err := strconv.ParseUint(devParts[1], 10, 32)
	if err != nil {
		return MountEntry{}, err
	}

	sepIdx := -1
	for i := 6; i < len(fields); i++ {
		if fields[i] == "-" {
			sepIdx = i
			break
		}
	}
	if sepIdx == -1 || sepIdx+2 >= len(fields) {
		return MountEntry{}, fmt.Errorf("missing separator or fields after separator")
	}

	return MountEntry{
		MountID:    mountID,
		ParentID:   parentID,
		Major:      uint32(major),
		Minor:      uint32(minor),
		Root:       fields[3],
		MountPoint: fields[4],
		FSType:     fields[sepIdx+1],
		Source:     fields[sepIdx+2],
	}, nil
}
