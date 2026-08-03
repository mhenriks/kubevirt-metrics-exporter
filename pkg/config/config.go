package config

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Shared
	ListenAddress string
	LogLevel      string
	NodeName      string
	Namespaces    string
	Boundaries    []float64
	BoundariesNs  []int64

	// CRI (shared by QMP and QGA)
	CRISocket string

	// QMP subsystem
	EnableQMP       bool
	QMPPollInterval time.Duration
	QMPConcurrency  int
	QMPTimeout      time.Duration
	QMPLabelFilter  string

	// QGA subsystem
	EnableQGA       bool
	QGAPollInterval time.Duration
	QGATimeout      int32
	QGAExecWait     time.Duration
	QGARetries      int
	QGAConcurrency  int
	QGALabelFilter  string

	// KVM subsystem
	EnableKVM       bool
	KVMPollInterval time.Duration
	KVMDebugFSPath  string

	// eBPF subsystem
	EnableEBPF           bool
	EnableEBPFBlock      bool
	EnableEBPFNFS        bool
	EnableEBPFNFSKprobe  bool
	EBPFScanInterval     int
	EBPFBlockMapSize     int
	EBPFNFSMapSize       int
	EBPFNFSKprobeMapSize int
	EBPFProcPath         string
}

func Parse() *Config {
	c := &Config{}
	var boundariesStr string

	// Shared flags
	flag.StringVar(&c.ListenAddress, "listen-address", envOrDefault("LISTEN_ADDRESS", ":8080"), "Address to listen on for metrics")
	flag.StringVar(&c.LogLevel, "log-level", envOrDefault("LOG_LEVEL", "info"), "Log level (debug, info, warn, error)")
	flag.StringVar(&boundariesStr, "boundaries", envOrDefault("BOUNDARIES", "10000000,100000000,1000000000"), "Histogram bucket boundaries in nanoseconds (comma-separated)")
	flag.StringVar(&c.Namespaces, "namespaces", envOrDefault("NAMESPACES", ""), "Comma-separated list of namespaces to monitor (empty = all)")

	// CRI socket (shared)
	flag.StringVar(&c.CRISocket, "cri-socket", envOrDefault("CRI_SOCKET", "/run/crio/crio.sock"), "CRI socket path for container discovery")

	// QMP flags
	flag.BoolVar(&c.EnableQMP, "enable-qmp", envBoolOrDefault("ENABLE_QMP", true), "Enable QMP-based VM storage latency collection")
	flag.DurationVar(&c.QMPPollInterval, "qmp-poll-interval", envDurationOrDefault("QMP_POLL_INTERVAL", 1*time.Minute), "Poll interval for scraping VMs")
	flag.IntVar(&c.QMPConcurrency, "qmp-concurrency", envIntOrDefault("QMP_CONCURRENCY", 8), "Max concurrent QMP operations")
	flag.DurationVar(&c.QMPTimeout, "qmp-timeout", envDurationOrDefault("QMP_TIMEOUT", 5*time.Second), "Timeout for individual QMP operations")
	flag.StringVar(&c.QMPLabelFilter, "qmp-label-filter", envOrDefault("QMP_LABEL_FILTER", ""), "Additional label selector for virt-launcher pods")

	// QGA flags
	flag.BoolVar(&c.EnableQGA, "enable-qga", envBoolOrDefault("ENABLE_QGA", true), "Enable QGA-based guest I/O latency collection")
	flag.DurationVar(&c.QGAPollInterval, "qga-poll-interval", envDurationOrDefault("QGA_POLL_INTERVAL", 1*time.Minute), "Poll interval for QGA guest metrics")
	var qgaTimeout int
	flag.IntVar(&qgaTimeout, "qga-timeout", envIntOrDefault("QGA_TIMEOUT", 10), "Timeout in seconds for individual QGA agent commands")
	flag.DurationVar(&c.QGAExecWait, "qga-exec-wait", envDurationOrDefault("QGA_EXEC_WAIT", 1*time.Second), "Wait time between guest-exec and guest-exec-status")
	flag.IntVar(&c.QGARetries, "qga-retries", envIntOrDefault("QGA_RETRIES", 10), "Max consecutive failures before stopping QGA polling for a VM")
	flag.IntVar(&c.QGAConcurrency, "qga-concurrency", envIntOrDefault("QGA_CONCURRENCY", 8), "Max concurrent QGA operations")
	flag.StringVar(&c.QGALabelFilter, "qga-label-filter", envOrDefault("QGA_LABEL_FILTER", ""), "Additional label selector for QGA virt-launcher pods")

	// KVM flags
	flag.BoolVar(&c.EnableKVM, "enable-kvm", envBoolOrDefault("ENABLE_KVM", true), "Enable KVM debugfs stats collection")
	flag.DurationVar(&c.KVMPollInterval, "kvm-poll-interval", envDurationOrDefault("KVM_POLL_INTERVAL", 30*time.Second), "Poll interval for KVM debugfs stats")
	flag.StringVar(&c.KVMDebugFSPath, "kvm-debugfs-path", envOrDefault("KVM_DEBUGFS_PATH", "/sys/kernel/debug/kvm"), "Path to KVM debugfs directory")

	// eBPF flags
	flag.BoolVar(&c.EnableEBPF, "enable-ebpf", envBoolOrDefault("ENABLE_EBPF", true), "Enable eBPF-based I/O latency collection")
	flag.BoolVar(&c.EnableEBPFBlock, "enable-ebpf-block", envBoolOrDefault("ENABLE_EBPF_BLOCK", true), "Enable block I/O tracing (eBPF)")
	flag.BoolVar(&c.EnableEBPFNFS, "enable-ebpf-nfs", envBoolOrDefault("ENABLE_EBPF_NFS", true), "Enable NFS I/O tracing (eBPF)")
	flag.BoolVar(&c.EnableEBPFNFSKprobe, "enable-ebpf-nfs-kprobe", envBoolOrDefault("ENABLE_EBPF_NFS_KPROBE", false), "Enable NFS VFS latency tracing (eBPF kprobe)")
	flag.IntVar(&c.EBPFScanInterval, "ebpf-scan-interval", envIntOrDefault("EBPF_SCAN_INTERVAL", 30), "Device scan interval in seconds (eBPF)")
	flag.IntVar(&c.EBPFBlockMapSize, "ebpf-block-map-size", envIntOrDefault("EBPF_BLOCK_MAP_SIZE", 10240), "Max entries for block start timestamp map")
	flag.IntVar(&c.EBPFNFSMapSize, "ebpf-nfs-map-size", envIntOrDefault("EBPF_NFS_MAP_SIZE", 10240), "Max entries for NFS start timestamp map")
	flag.IntVar(&c.EBPFNFSKprobeMapSize, "ebpf-nfs-kprobe-map-size", envIntOrDefault("EBPF_NFS_KPROBE_MAP_SIZE", 10240), "Max entries for NFS kprobe start timestamp map")
	flag.StringVar(&c.EBPFProcPath, "ebpf-proc-path", envOrDefault("EBPF_PROC_PATH", "/proc"), "Path to host proc filesystem")

	flag.Parse()

	c.NodeName = os.Getenv("NODE_NAME")
	c.QGATimeout = int32(qgaTimeout)
	c.BoundariesNs = parseBoundariesNs(boundariesStr)
	c.Boundaries = parseBoundariesSeconds(boundariesStr)

	return c
}

func (c *Config) Validate() error {
	if c.NodeName == "" {
		return fmt.Errorf("NODE_NAME environment variable is required")
	}
	if len(c.BoundariesNs) == 0 {
		return fmt.Errorf("boundaries must contain at least one value")
	}
	if c.EnableEBPF && c.EBPFScanInterval <= 0 {
		return fmt.Errorf("ebpf-scan-interval must be positive, got %d", c.EBPFScanInterval)
	}
	return nil
}

func ParseNamespaces(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func parseBoundariesNs(s string) []int64 {
	parts := strings.Split(s, ",")
	result := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			continue
		}
		result = append(result, v)
	}
	return result
}

func parseBoundariesSeconds(s string) []float64 {
	parts := strings.Split(s, ",")
	buckets := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		ns, err := strconv.ParseFloat(p, 64)
		if err != nil {
			continue
		}
		buckets = append(buckets, ns/1e9)
	}
	sort.Float64s(buckets)
	return buckets
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDurationOrDefault(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		i, err := strconv.Atoi(v)
		if err == nil {
			return i
		}
	}
	return def
}

func envBoolOrDefault(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1"
	}
	return def
}
