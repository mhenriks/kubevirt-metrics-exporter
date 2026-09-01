# KubeVirt Metrics Exporter

A Prometheus exporter that monitors storage I/O latency for OpenShift Virtualization workloads. It runs as a DaemonSet and combines three collection methods in a single container:

- **QMP subsystem** — connects to each VM's QEMU Monitor Protocol to collect per-disk read/write/flush latency histograms directly from the hypervisor
- **QGA subsystem** — uses the QEMU Guest Agent to collect guest-side I/O latency and IOPS from Windows VMs via Windows Performance Counters (PDH raw counters)
- **eBPF subsystem** — attaches kernel tracepoints and kprobes to capture block and NFS I/O latency across the node, correlated to Kubernetes pods and PersistentVolumeClaims
- **KVM subsystem** — reads KVM hypervisor event counters (exits, hypercalls, TLB flushes, halt exits) from the kernel debugfs at `/sys/kernel/debug/kvm/`
- **Cgroup subsystem** — reads per-VMI cgroup v2 memory stats (anonymous, THP) from QEMU process cgroups, and exposes per-node kernel thread CPU usage (`khugepaged`, `ksmd`) and KSM memory profit

All subsystems are independently enabled/disabled and degrade gracefully if one fails to start.

## Metrics

The exporter instruments several points along the I/O path from guest application to storage backend:

```
Guest application
  │
  ▼
Guest OS block layer ◄──── QGA: guest-side latency & IOPS (Windows only)
  │
  ▼
Storage virtqueue ◄─────── QMP: queue_inuse / queue_size (saturation, virtio/scsi)
  │
  ▼
QEMU block backend ◄────── QMP: I/O latency histogram (hypervisor-side)
  │
KVM (hypervisor) ◄───────── KVM: exits, hypercalls, tlb_flush, halt_exits
  │
  ├──► Host block layer ◄─ eBPF block: block_rq_issue → block_rq_complete
  │
  └──► Host NFS client ◄── eBPF NFS: nfs_initiate_* → nfs_*_done
         │
         ▼
       Storage backend
```

When diagnosing latency, compare metrics across layers: if QMP latency is high but eBPF block latency is low, the bottleneck is in the virtio/QEMU layer. If both are high, the problem is in the storage backend. If guest-side (QGA) latency is high but QMP latency is low, queuing is building up inside the guest.

VMI-level metrics use the `kubevirt_vmi_storage_*` prefix; exporter-scoped operational and eBPF metrics use the `kme_*` prefix.

### QMP metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubevirt_vmi_storage_io_latency_seconds` | histogram | namespace, name, node, disk, persistentvolumeclaim, operation | Per-disk I/O latency for KubeVirt VMs |
| `kubevirt_vmi_storage_queue_inuse` | gauge | namespace, name, node, disk, persistentvolumeclaim, queue, bus | In-flight descriptors in a storage virtqueue (`bus="virtio"` for virtio-blk disks, `bus="scsi"` for virtio-scsi controllers; `disk` and `persistentvolumeclaim` are empty for SCSI) |
| `kubevirt_vmi_storage_queue_size` | gauge | namespace, name, node, disk, persistentvolumeclaim, queue, bus | Capacity (max descriptors) of a storage virtqueue (see queue_inuse for label semantics) |
| `kme_qmp_scrape_errors_total` | counter | | Errors during QMP poll cycles |
| `kme_qmp_last_poll_timestamp_seconds` | gauge | | Unix timestamp of last QMP poll |

### QGA metrics (guest-side, Windows)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubevirt_vmi_storage_guest_latency_avg_seconds` | gauge | namespace, name, node, disk, persistentvolumeclaim, operation, drive | Average guest-side I/O latency per disk (read/write), derived via Little's Law |
| `kubevirt_vmi_storage_guest_iops` | gauge | namespace, name, node, disk, persistentvolumeclaim, operation, drive | Guest-side IOPS per disk (read/write) |
| `kme_qga_scrape_errors_total` | counter | | Errors during QGA poll cycles |
| `kme_qga_last_poll_timestamp_seconds` | gauge | | Unix timestamp of last QGA poll |

The `disk` label contains the KubeVirt volume name (e.g. `rootdisk`), populated by correlating guest PCI addresses (from the `guest-get-disks` QGA command) with libvirt domain XML disk aliases (`ua-<volumeName>`). The `drive` label contains the raw Windows PhysicalDisk name (e.g. `"1 E:"`). The `persistentvolumeclaim` label is derived by mapping volume names to PVC claim names via the VMI `status.volumeStatus`. If disk mapping is unavailable (e.g. old guest agent), `disk` and `persistentvolumeclaim` are empty.

Disk mapping limitations:
- Disk mappings are established when the VMI is first discovered. Volumes hotplugged after that point are not tracked (their `disk` and `persistentvolumeclaim` labels will be empty).
- SATA disks can only be mapped when a `serial` is explicitly set on the disk in the VM spec. This is a QEMU guest agent limitation. Without `<serial>` the label `persistentvolumeclaim` will be empty.

The QGA subsystem collects raw Windows Performance Counters (`Win32_PerfRawData_PerfDisk_PhysicalDisk`) via `wmic` executed through the QEMU Guest Agent. Metrics are computed by diffing two successive counter snapshots using Little's Law to derive latency from uint64 queue-length counters (avoiding uint32 overflow in the direct latency counters). VMs without a guest agent (e.g., Linux) or with `guest-exec` blacklisted are automatically detected and excluded after a configurable number of retries.

### eBPF metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kme_block_io_latency_seconds` | histogram | node, namespace, persistentvolumeclaim, pod, operation | Block I/O latency attributed to pod volumes |
| `kme_system_block_io_latency_seconds` | histogram | node, device, operation | Block I/O latency for system/unresolved devices |
| `kme_nfs_io_latency_seconds` | histogram | node, namespace, persistentvolumeclaim, pod, operation | NFS I/O latency (tracepoint-based) |
| `kme_nfs_vfs_latency_seconds` | histogram | node, namespace, persistentvolumeclaim, pod, operation | NFS VFS call latency (kprobe-based) |
| `kme_subsystem_active` | gauge | subsystem | Whether an eBPF subsystem loaded successfully (1) or not (0) |

### KVM metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubevirt_vmi_kvm_exits_total` | counter | namespace, name, node, pod | Total KVM VM exits (guest→hypervisor transitions) |
| `kubevirt_vmi_kvm_hypercalls_total` | counter | namespace, name, node, pod | Total KVM hypercalls issued by the guest |
| `kubevirt_vmi_kvm_tlb_flushes_total` | counter | namespace, name, node, pod | Total KVM TLB flush events |
| `kubevirt_vmi_kvm_halt_exits_total` | counter | namespace, name, node, pod | Total KVM exits triggered by guest halt instructions |
| `kme_kvm_scrape_errors_total` | counter | | Errors during KVM debugfs poll cycles |
| `kme_kvm_last_poll_timestamp_seconds` | gauge | | Unix timestamp of last KVM poll |

Counters are read from `/sys/kernel/debug/kvm/<pid>-<fd>/` and aggregated across all KVM file-descriptor entries for a single QEMU process. A high exit rate relative to vCPU time indicates the guest is spending significant cycles in hypervisor context. A high `halt_exits` rate is normal for idle VMs (vCPUs sleeping) but abnormal for CPU-intensive ones.

### Cgroup metrics

Per-VMI memory metrics (aligned with [CRI-O PR #10143](https://github.com/cri-o/cri-o/pull/10143)):

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `container_memory_active_anon_bytes` | gauge | namespace, name, node, pod | Active anonymous memory in bytes |
| `container_memory_inactive_anon_bytes` | gauge | namespace, name, node, pod | Inactive anonymous memory in bytes |
| `container_memory_anon_thp_bytes` | gauge | namespace, name, node, pod | Anonymous memory backed by transparent hugepages in bytes |
| `container_memory_shmem_thp_bytes` | gauge | namespace, name, node, pod | Shared memory backed by transparent hugepages in bytes (kernel 6.8+) |
| `container_memory_file_thp_bytes` | gauge | namespace, name, node, pod | File-backed memory backed by transparent hugepages in bytes |

Per-node kernel thread and KSM metrics:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kme_cgroup_khugepaged_cpu_seconds_total` | counter | node | Cumulative CPU time consumed by the khugepaged kernel thread |
| `kme_cgroup_ksmd_cpu_seconds_total` | counter | node | Cumulative CPU time consumed by the ksmd kernel thread |
| `node_ksmd_general_profit_bytes` | gauge | | Net memory saved by KSM after subtracting tracking overhead (aligned with [node_exporter PR #3778](https://github.com/prometheus/node_exporter/pull/3778)) |
| `kme_node_thp_split_pmd_total` | counter | node | THP page table downgrades (`thp_split_pmd` from `/proc/vmstat`) |
| `kme_node_thp_collapse_alloc_total` | counter | node | Successful THP collapses by khugepaged (`thp_collapse_alloc` from `/proc/vmstat`) |
| `kme_node_movable_bytes_order_ge_9` | gauge | node, numa | Movable-capable free buddy memory at page order ≥9 in bytes (buddy minus pagetype Unmovable and Isolate) |
| `kme_node_movable_bytes_all_orders` | gauge | node, numa | Movable-capable free buddy memory across all orders in bytes (buddy minus Unmovable and Isolate) |
| `kme_node_buddy_bytes_order_ge_9` | gauge | node, numa | Total free buddy memory at page order ≥9 in bytes (exact, Normal zone, `/proc/buddyinfo`) |
| `kme_node_buddy_bytes_all_orders` | gauge | node, numa | Total free buddy memory across all orders in bytes (exact, Normal zone, `/proc/buddyinfo`) |
| `kme_node_unmovable_bytes_order_ge_9` | gauge | node, numa | Unmovable free buddy memory at page order ≥9 in bytes (Normal zone, `/proc/pagetypinfo`) |
| `kme_node_unmovable_bytes_all_orders` | gauge | node, numa | Unmovable free buddy memory across all orders in bytes (Normal zone, `/proc/pagetypinfo`) |
| `kme_cgroup_scrape_errors_total` | counter | | Errors during cgroup poll cycles |
| `kme_cgroup_last_poll_timestamp_seconds` | gauge | | Unix timestamp of last cgroup poll |

Memory metrics are read from the cgroup v2 `memory.stat` file of each QEMU process. The `container_memory_*` naming aligns with the CRI-O proposal to allow future migration when CRI-O exposes these natively. The `kme_*` prefix is used for exporter-specific kernel thread metrics. The `node_ksmd_*` naming aligns with the node_exporter proposal.

### Example PromQL

P99 write latency per VMI:
```promql
histogram_quantile(0.99,
  sum by (name, le) (
    rate(kubevirt_vmi_storage_io_latency_seconds_bucket{operation="write"}[5m])
  )
)
```

Virtqueue saturation per disk (ratio of in-flight descriptors to capacity):
```promql
kubevirt_vmi_storage_queue_inuse / (kubevirt_vmi_storage_queue_size > 0)
```

P99 host-side block I/O latency attributed to a pod's PVC:
```promql
histogram_quantile(0.99,
  sum by (namespace, persistentvolumeclaim, pod, operation, le) (
    rate(kme_block_io_latency_seconds_bucket{operation=~"read|write"}[5m])
  )
)
```

See [`docs/example-queries.md`](docs/example-queries.md) for a full per-metric query reference.

The `bus` label distinguishes `virtio` (per-disk virtio-blk devices) from `scsi` (shared virtio-scsi controller). For virtio-scsi, `disk` and `persistentvolumeclaim` are empty because the virtqueues belong to the shared controller rather than any individual disk.

## Configuration

Shared flags apply to all subsystems. QMP-specific flags are prefixed with `--qmp-`, QGA-specific with `--qga-`, eBPF-specific with `--ebpf-`. All flags can be overridden via environment variables.

### Shared

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--listen-address` | `LISTEN_ADDRESS` | `:8080` | Metrics server listen address |
| `--log-level` | `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `--boundaries` | `BOUNDARIES` | `10000000,100000000,1000000000` | Histogram bucket boundaries in nanoseconds |
| | `NODE_NAME` | (required) | Node name, typically from downward API |
| `--namespaces` | `NAMESPACES` | (all) | Comma-separated namespace filter (applies to both QMP and eBPF) |
| `--cri-socket` | `CRI_SOCKET` | `/run/crio/crio.sock` | CRI socket path for container discovery (shared by QMP and QGA) |

### QMP

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-qmp` | `ENABLE_QMP` | `true` | Enable QMP collection |
| `--qmp-poll-interval` | `QMP_POLL_INTERVAL` | `1m` | VM scrape interval |
| `--qmp-concurrency` | `QMP_CONCURRENCY` | `8` | Max parallel QMP operations |
| `--qmp-timeout` | `QMP_TIMEOUT` | `5s` | Per-operation QMP timeout |
| `--qmp-label-filter` | `QMP_LABEL_FILTER` | | Additional pod label selector |

### QGA

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-qga` | `ENABLE_QGA` | `true` | Enable QGA guest-side I/O collection |
| `--qga-poll-interval` | `QGA_POLL_INTERVAL` | `1m` | Guest metrics poll interval |
| `--qga-timeout` | `QGA_TIMEOUT` | `10` | Per-command QGA timeout (seconds) |
| `--qga-exec-wait` | `QGA_EXEC_WAIT` | `1s` | Wait between guest-exec and guest-exec-status |
| `--qga-retries` | `QGA_RETRIES` | `10` | Max consecutive failures before stopping collection for a VM |
| `--qga-concurrency` | `QGA_CONCURRENCY` | `8` | Max parallel QGA operations |
| `--qga-label-filter` | `QGA_LABEL_FILTER` | | Additional pod label selector for QGA |

### eBPF

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-ebpf` | `ENABLE_EBPF` | `true` | Enable eBPF collection |
| `--enable-ebpf-block` | `ENABLE_EBPF_BLOCK` | `true` | Enable block I/O tracing |
| `--enable-ebpf-nfs` | `ENABLE_EBPF_NFS` | `true` | Enable NFS tracing |
| `--enable-ebpf-nfs-kprobe` | `ENABLE_EBPF_NFS_KPROBE` | `false` | Enable NFS VFS kprobe tracing |
| `--ebpf-scan-interval` | `EBPF_SCAN_INTERVAL` | `30` | Device-to-pod resolution interval (seconds) |
| `--ebpf-proc-path` | `EBPF_PROC_PATH` | `/proc` | Host proc filesystem path |

### KVM

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-kvm` | `ENABLE_KVM` | `true` | Enable KVM debugfs stats collection |
| `--kvm-poll-interval` | `KVM_POLL_INTERVAL` | `30s` | Poll interval for KVM counters |

### Cgroup

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-cgroup` | `ENABLE_CGROUP` | `true` | Enable cgroup v2 memory and kernel thread collection |
| `--cgroup-poll-interval` | `CGROUP_POLL_INTERVAL` | `30s` | Poll interval for cgroup stats |

## Alerting

Prometheus alerting rules are included in `deploy/prometheus-rules/` and deployed automatically with `make deploy` / `make deploy-kubernetes`.

The rules cover two areas:

**Workload health** — alerts on high storage I/O latency (hypervisor-side, guest-side, and node-side) and virtqueue saturation:

| Alert | Severity | Condition |
|-------|----------|-----------|
| VMIStorageWriteLatencyHigh | warning | P99 write latency > 100ms for 10m |
| VMIStorageReadLatencyHigh | warning | P99 read latency > 100ms for 10m |
| VMIStorageFlushLatencyHigh | warning | P99 flush latency > 500ms for 15m |
| NodeVMStorageLatencyWidespread | critical | >50% of active VMs on a node with P99 > 100ms for 10m |
| VMIDiskSaturated | critical | Aggregate virtqueue occupancy > 90% for 5m |
| VMIGuestStorageLatencyHigh | warning | Guest-side avg latency > 100ms for 15m |
| PVCBlockLatencyHigh | warning | P99 block latency > 100ms for 10m |
| PVCNFSLatencyHigh | warning | P99 NFS latency > 250ms for 10m |

**Exporter health** — alerts when the exporter itself is unhealthy or producing stale data:

| Alert | Severity | Condition |
|-------|----------|-----------|
| KMEQMPPollStale | warning | QMP poll > 5 min stale |
| KMEQGAPollStale | warning | QGA poll > 5 min stale |
| KMEKVMPollStale | warning | KVM poll > 90s stale |
| KMECgroupPollStale | warning | Cgroup poll > 90s stale |
| KMEQMPScrapeErrors | warning | Sustained QMP errors for 15m |
| KMEQGAScrapeErrors | warning | Sustained QGA errors for 15m |
| KMEKVMScrapeErrors | warning | Sustained KVM errors for 15m |
| KMECgroupScrapeErrors | warning | Sustained cgroup errors for 15m |
| KMEeBPFSubsystemDown | warning | Block eBPF subsystem down for 10m |
| KMEAbsent | critical | No metrics scraped for 10m |

The `KMEAbsent` alert uses the Prometheus `up` metric with `job="kubevirt-metrics-exporter"`. If your PodMonitor uses a different job name, update the alert expression to match.

QMP histogram latency values reported in alert annotations are approximate due to the default histogram bucket granularity (10ms, 100ms, 1s). For higher precision, configure finer-grained boundaries via `--boundaries`.

## Building

Prerequisites: Go 1.25+, clang, llvm, libbpf-devel

```bash
make build        # generates eBPF bindings and builds the binary (requires clang/llvm)
make test         # runs all tests (requires generated eBPF bindings)
make test-unit    # runs unit tests for non-eBPF packages (works on macOS)
make image        # builds container image with podman (includes full toolchain)
```

To build and push a custom image:

```bash
make push IMAGE=quay.io/myuser/kubevirt-metrics-exporter TAG=v0.1.0
```

## Deploying

### From a release

Download the install manifest from the [latest release](https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest):

OpenShift:

```bash
oc apply -f https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest/download/install-openshift.yaml
```

Kubernetes:

```bash
kubectl apply -f https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest/download/install-kubernetes.yaml
```

### From source

OpenShift:

```bash
make deploy
```

Kubernetes:

```bash
make deploy-kubernetes
```

To deploy with a custom image:

```bash
make deploy IMAGE=quay.io/myuser/kubevirt-metrics-exporter TAG=v0.1.0
```

The OpenShift variant includes SecurityContextConstraints, worker node selector, and PodMonitor for Prometheus scraping.

### Required capabilities

| Capability | Reason |
|-----------|--------|
| `hostPID` | Access VM virtqemud sockets via `/proc/<pid>/root/` |
| `SYS_PTRACE` | Traverse `/proc/<pid>/root/` of other containers |
| `DAC_OVERRIDE` | Connect to virtqemud socket owned by qemu UID |
| `BPF` | Load and attach eBPF programs |
| `PERFMON` | Attach to kernel tracepoints and kprobes |
| `SYS_RESOURCE` | Increase eBPF map memory limits |
