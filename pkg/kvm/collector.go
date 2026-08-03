package kvm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift-virtualization/kubevirt-metrics-exporter/pkg/vmi"
)

var (
	exitsDesc = prometheus.NewDesc(
		"kubevirt_vmi_kvm_exits_total",
		"Total number of KVM VM exits for a VMI",
		[]string{"namespace", "name", "node", "pod"},
		nil,
	)

	hypercallsDesc = prometheus.NewDesc(
		"kubevirt_vmi_kvm_hypercalls_total",
		"Total number of KVM hypercalls for a VMI",
		[]string{"namespace", "name", "node", "pod"},
		nil,
	)

	tlbFlushDesc = prometheus.NewDesc(
		"kubevirt_vmi_kvm_tlb_flushes_total",
		"Total number of KVM TLB flushes for a VMI",
		[]string{"namespace", "name", "node", "pod"},
		nil,
	)

	haltExitsDesc = prometheus.NewDesc(
		"kubevirt_vmi_kvm_halt_exits_total",
		"Total number of KVM halt exits for a VMI",
		[]string{"namespace", "name", "node", "pod"},
		nil,
	)

	kvmScrapeErrorsDesc = prometheus.NewDesc(
		"kme_kvm_scrape_errors_total",
		"Total number of errors encountered during KVM debugfs scrape cycles",
		nil, nil,
	)

	kvmLastPollDesc = prometheus.NewDesc(
		"kme_kvm_last_poll_timestamp_seconds",
		"Unix timestamp of the last KVM debugfs poll cycle",
		nil, nil,
	)
)

type Config struct {
	NodeName     string
	PollInterval time.Duration
	DebugFSPath  string
}

type vmiStats struct {
	namespace  string
	name       string
	pod        string
	exits      uint64
	hypercalls uint64
	tlbFlush   uint64
	haltExits  uint64
}

type Collector struct {
	cfg      Config
	podStore cache.Store
	log      *slog.Logger

	mu           sync.RWMutex
	results      []vmiStats
	scrapeErrors float64
	lastPollTS   float64
}

func NewCollector(cfg Config, podStore cache.Store, log *slog.Logger) *Collector {
	return &Collector{
		cfg:      cfg,
		podStore: podStore,
		log:      log,
	}
}

func (c *Collector) Run(ctx context.Context) {
	c.poll()
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.poll()
		}
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- exitsDesc
	ch <- hypercallsDesc
	ch <- tlbFlushDesc
	ch <- haltExitsDesc
	ch <- kvmScrapeErrorsDesc
	ch <- kvmLastPollDesc
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ch <- prometheus.MustNewConstMetric(kvmScrapeErrorsDesc, prometheus.CounterValue, c.scrapeErrors)
	ch <- prometheus.MustNewConstMetric(kvmLastPollDesc, prometheus.GaugeValue, c.lastPollTS)

	for _, s := range c.results {
		labels := []string{s.namespace, s.name, c.cfg.NodeName, s.pod}
		ch <- prometheus.MustNewConstMetric(exitsDesc, prometheus.CounterValue, float64(s.exits), labels...)
		ch <- prometheus.MustNewConstMetric(hypercallsDesc, prometheus.CounterValue, float64(s.hypercalls), labels...)
		ch <- prometheus.MustNewConstMetric(tlbFlushDesc, prometheus.CounterValue, float64(s.tlbFlush), labels...)
		ch <- prometheus.MustNewConstMetric(haltExitsDesc, prometheus.CounterValue, float64(s.haltExits), labels...)
	}
}

func (c *Collector) poll() {
	c.log.Info("kvm: starting poll cycle")

	pidMap, err := c.scanDebugFS()
	if err != nil {
		c.log.Error("kvm: scanning debugfs", "error", err)
		c.mu.Lock()
		c.scrapeErrors++
		c.mu.Unlock()
		return
	}

	domainToPod := vmi.BuildDomainToPodMap(c.podStore)

	var (
		results      []vmiStats
		scrapeErrors int
	)

	for pid, stats := range pidMap {
		domainName, ok := vmi.ReadDomainName(pid, "/proc")
		if !ok {
			continue
		}

		ns, vmiName, ok := vmi.ParseDomainName(domainName)
		if !ok {
			c.log.Debug("kvm: skipping unrecognised domain name", "pid", pid, "name", domainName)
			continue
		}

		podName := domainToPod[ns+"_"+vmiName]
		if podName == "" {
			c.log.Debug("kvm: no matching pod for VMI", "namespace", ns, "name", vmiName)
			scrapeErrors++
			continue
		}

		results = append(results, vmiStats{
			namespace:  ns,
			name:       vmiName,
			pod:        podName,
			exits:      stats.exits,
			hypercalls: stats.hypercalls,
			tlbFlush:   stats.tlbFlush,
			haltExits:  stats.haltExits,
		})
	}

	c.mu.Lock()
	c.results = results
	c.scrapeErrors += float64(scrapeErrors)
	c.lastPollTS = float64(time.Now().Unix())
	c.mu.Unlock()

	c.log.Info("kvm: poll cycle complete", "vms", len(results), "errors", scrapeErrors)
}

// perPIDStats accumulates KVM counters across multiple fd entries for one PID.
type perPIDStats struct {
	exits      uint64
	hypercalls uint64
	tlbFlush   uint64
	haltExits  uint64
}

// scanDebugFS reads all <pid>-<fd> subdirectories under DebugFSPath and
// aggregates the four counters per PID.
func (c *Collector) scanDebugFS() (map[int]*perPIDStats, error) {
	entries, err := os.ReadDir(c.cfg.DebugFSPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", c.cfg.DebugFSPath, err)
	}

	pidMap := make(map[int]*perPIDStats)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, ok := parsePIDFromDir(entry.Name())
		if !ok {
			continue
		}

		dir := filepath.Join(c.cfg.DebugFSPath, entry.Name())
		s, existing := pidMap[pid]
		if !existing {
			s = &perPIDStats{}
			pidMap[pid] = s
		}

		s.exits += readUint64File(filepath.Join(dir, "exits"))
		s.hypercalls += readUint64File(filepath.Join(dir, "hypercalls"))
		s.tlbFlush += readUint64File(filepath.Join(dir, "tlb_flush"))
		s.haltExits += readUint64File(filepath.Join(dir, "halt_exits"))
	}

	return pidMap, nil
}

// parsePIDFromDir extracts the PID from a "<pid>-<fd>" directory name.
func parsePIDFromDir(name string) (int, bool) {
	idx := strings.LastIndex(name, "-")
	if idx <= 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(name[:idx])
	if err != nil {
		return 0, false
	}
	return pid, true
}

// readUint64File reads a single uint64 value from a file, returning 0 on error.
func readUint64File(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}
