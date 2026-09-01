package cgroup

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const buddyPageSize = 4096

// numaBuddyFree holds exact free buddy block counts for the Normal zone per NUMA node.
type numaBuddyFree struct {
	NUMA              string
	OrderGe9Bytes     uint64
	AllOrdersBytes    uint64
}

// readBuddyNormal parses /proc/buddyinfo free block counts for the Normal zone per NUMA node.
func readBuddyNormal(procPath string) ([]numaBuddyFree, error) {
	f, err := os.Open(filepath.Join(procPath, "buddyinfo"))
	if err != nil {
		return nil, fmt.Errorf("opening buddyinfo: %w", err)
	}
	defer f.Close()

	var results []numaBuddyFree

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 5 || parts[0] != "Node" || parts[2] != "zone" {
			continue
		}
		zone := strings.TrimSuffix(parts[3], ",")
		if zone != "Normal" {
			continue
		}

		numa := strings.TrimSuffix(parts[1], ",")
		var orderGe9, allOrders uint64
		for order, countStr := range parts[4:] {
			blocks, err := strconv.ParseUint(countStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parsing buddyinfo NUMA %s order %d: %w", numa, order, err)
			}
			bytes := blocks * (uint64(buddyPageSize) << order)
			allOrders += bytes
			if order >= 9 {
				orderGe9 += bytes
			}
		}

		if allOrders == 0 {
			continue
		}
		results = append(results, numaBuddyFree{
			NUMA:           numa,
			OrderGe9Bytes:  orderGe9,
			AllOrdersBytes: allOrders,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading buddyinfo: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("buddyinfo: Normal zone entries not found")
	}

	return results, nil
}
