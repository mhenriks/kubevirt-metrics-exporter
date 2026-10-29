# Metrics

[Documentation index](../README.md#documentation)

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

For memory and THP, see [THP and node memory readiness](memory-and-thp.md) and the cgroup metrics below.

VMI-level storage and KVM metrics use the `kubevirt_vmi_*` prefix. Per-VMI cgroup memory from this exporter uses `container_memory_*` (CRI-O-aligned). Exporter operational, eBPF, and node buddy metrics use `kme_*`.

## QMP metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubevirt_vmi_storage_io_latency_seconds` | histogram | namespace, name, node, pod, disk, persistentvolumeclaim, operation | Per-disk I/O latency for KubeVirt VMs |
| `kubevirt_vmi_storage_queue_inuse` | gauge | namespace, name, node, pod, disk, persistentvolumeclaim, queue, bus | In-flight descriptors in a storage virtqueue (`bus="virtio"` for virtio-blk disks, `bus="scsi"` for virtio-scsi controllers; `disk` and `persistentvolumeclaim` are empty for SCSI) |
| `kubevirt_vmi_storage_queue_size` | gauge | namespace, name, node, pod, disk, persistentvolumeclaim, queue, bus | Capacity (max descriptors) of a storage virtqueue (see queue_inuse for label semantics) |
| `kme_qmp_scrape_errors_total` | counter | | Errors during QMP poll cycles |
| `kme_qmp_last_poll_timestamp_seconds` | gauge | | Unix timestamp of last QMP poll |

The `bus` label distinguishes `virtio` (per-disk virtio-blk devices) from `scsi` (shared virtio-scsi controller). For virtio-scsi, `disk` and `persistentvolumeclaim` are empty because the virtqueues belong to the shared controller rather than any individual disk.

## QGA metrics (guest-side, Windows)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubevirt_vmi_storage_guest_latency_avg_seconds` | gauge | namespace, name, node, pod, disk, persistentvolumeclaim, operation, drive | Average guest-side I/O latency per disk (read/write), derived via Little's Law |
| `kubevirt_vmi_storage_guest_iops` | gauge | namespace, name, node, pod, disk, persistentvolumeclaim, operation, drive | Guest-side IOPS per disk (read/write) |
| `kme_qga_scrape_errors_total` | counter | | Errors during QGA poll cycles |
| `kme_qga_last_poll_timestamp_seconds` | gauge | | Unix timestamp of last QGA poll |

The `disk` label contains the KubeVirt volume name (e.g. `rootdisk`), populated by correlating guest PCI addresses (from the `guest-get-disks` QGA command) with libvirt domain XML disk aliases (`ua-<volumeName>`). The `drive` label contains the raw Windows PhysicalDisk name (e.g. `"1 E:"`). The `persistentvolumeclaim` label is derived by mapping volume names to PVC claim names via the VMI `status.volumeStatus`. If disk mapping is unavailable (e.g. old guest agent), `disk` and `persistentvolumeclaim` are empty.

Disk mapping limitations:
- Disk mappings are established when the VMI is first discovered. Volumes hotplugged after that point are not tracked (their `disk` and `persistentvolumeclaim` labels will be empty).
- SATA disks can only be mapped when a `serial` is explicitly set on the disk in the VM spec. This is a QEMU guest agent limitation. Without `<serial>` the label `persistentvolumeclaim` will be empty.

The QGA subsystem collects raw Windows Performance Counters (`Win32_PerfRawData_PerfDisk_PhysicalDisk`) via PowerShell `Get-CimInstance` executed through the QEMU Guest Agent. Metrics are computed by diffing two successive counter snapshots using Little's Law to derive latency from uint64 queue-length counters (avoiding uint32 overflow in the direct latency counters). Guest-side collection requires Windows with PowerShell and a functioning QEMU Guest Agent that allows `guest-exec`. After the configured failure threshold, collection falls back to probing every 60 poll intervals; blacklisted commands trigger this fallback immediately. A successful probe restores normal polling.

## eBPF metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kme_block_io_latency_seconds` | histogram | node, namespace, persistentvolumeclaim, pod, operation | Block I/O latency attributed to pod volumes |
| `kme_system_block_io_latency_seconds` | histogram | node, device, operation | Block I/O latency for system/unresolved devices |
| `kme_nfs_io_latency_seconds` | histogram | node, namespace, persistentvolumeclaim, pod, operation | NFS I/O latency (tracepoint-based) |
| `kme_nfs_vfs_latency_seconds` | histogram | node, namespace, persistentvolumeclaim, pod, operation | NFS VFS call latency (kprobe-based) |
| `kme_subsystem_active` | gauge | subsystem | Whether an eBPF subsystem loaded successfully (1) or not (0) |

## KVM metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `kubevirt_vmi_kvm_exits_total` | counter | namespace, name, node, pod | Total KVM VM exits (guest→hypervisor transitions) |
| `kubevirt_vmi_kvm_hypercalls_total` | counter | namespace, name, node, pod | Total KVM hypercalls issued by the guest |
| `kubevirt_vmi_kvm_tlb_flushes_total` | counter | namespace, name, node, pod | Total KVM TLB flush events |
| `kubevirt_vmi_kvm_halt_exits_total` | counter | namespace, name, node, pod | Total KVM exits triggered by guest halt instructions |
| `kme_kvm_scrape_errors_total` | counter | | Errors during KVM debugfs poll cycles |
| `kme_kvm_last_poll_timestamp_seconds` | gauge | | Unix timestamp of last KVM poll |

Counters are read from `/sys/kernel/debug/kvm/<pid>-<fd>/` and aggregated across all KVM file-descriptor entries for a single QEMU process. A high exit rate relative to vCPU time indicates the guest is spending significant cycles in hypervisor context. A high `halt_exits` rate is normal for idle VMs (vCPUs sleeping) but abnormal for CPU-intensive ones.

## Cgroup metrics

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
| `node_ksmd_general_profit_bytes` | gauge | node | Net memory saved by KSM after subtracting tracking overhead (aligned with [node_exporter PR #3778](https://github.com/prometheus/node_exporter/pull/3778)) |
| `kme_node_thp_split_pmd_total` | counter | node | THP page table downgrades (`thp_split_pmd` from `/proc/vmstat`) |
| `kme_node_thp_collapse_alloc_total` | counter | node | Successful THP collapses by khugepaged (`thp_collapse_alloc` from `/proc/vmstat`) |
| `kme_node_movable_bytes_order_ge_9` | gauge | node, numa | Movable-capable free buddy memory at page order ≥9 in bytes (Movable zone if present, else Normal; buddy minus Unmovable and Isolate) |
| `kme_node_movable_bytes_all_orders` | gauge | node, numa | Movable-capable free buddy memory across all orders in bytes (Movable zone if present, else Normal; buddy minus Unmovable and Isolate) |
| `kme_node_buddy_bytes_order_ge_9` | gauge | node, numa | Total free buddy memory at page order ≥9 in bytes (Movable zone if present, else Normal; exact from `/proc/buddyinfo`) |
| `kme_node_buddy_bytes_all_orders` | gauge | node, numa | Total free buddy memory across all orders in bytes (Movable zone if present, else Normal; exact from `/proc/buddyinfo`) |
| `kme_node_unmovable_bytes_order_ge_9` | gauge | node, numa | Unmovable free buddy memory at page order ≥9 in bytes (Normal + Movable zones, `/proc/pagetypeinfo`) |
| `kme_node_unmovable_bytes_all_orders` | gauge | node, numa | Unmovable free buddy memory across all orders in bytes (Normal + Movable zones, `/proc/pagetypeinfo`) |
| `kme_node_zone_present_bytes` | gauge | node, numa, zone | Zone size in bytes from `/proc/zoneinfo` `present` pages |
| `kme_node_zone_free_bytes` | gauge | node, numa, zone | Zone free pages in bytes from `/proc/zoneinfo` `pages free` |
| `kme_node_slab_reclaimable_bytes` | gauge | node, numa | Reclaimable slab in bytes from zoneinfo per-node `nr_slab_reclaimable` (not zone-scoped) |
| `kme_cgroup_scrape_errors_total` | counter | | Errors during cgroup poll cycles |
| `kme_cgroup_last_poll_timestamp_seconds` | gauge | | Unix timestamp of last cgroup poll |

Per-VMI gauges come from cgroup v2 `memory.stat` on each QEMU process. Node buddy, pagetype, zoneinfo, and vmstat THP counters come from host `/proc` (per NUMA node). Buddy/movable metrics use the Movable zone when present (else Normal). The `container_memory_*` naming aligns with the [CRI-O cgroup memory proposal](https://github.com/cri-o/cri-o/pull/10143); `node_ksmd_general_profit_bytes` aligns with [node_exporter PR #3778](https://github.com/prometheus/node_exporter/pull/3778).

VM resident and domain memory (`kubevirt_vmi_memory_resident_bytes`, `kubevirt_vmi_memory_domain_bytes`) are scraped from virt-handler / KubeVirt, not from this exporter — see the [dashboards](dashboards.md) and [THP and node memory readiness](memory-and-thp.md) sections for how those metrics are used alongside KME cgroup and buddy gauges.

## Example PromQL

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

See [example PromQL queries](example-queries.md) for a full per-metric query reference (storage, KVM, and exporter health). Memory and THP dashboard interpretation is in [THP and node memory readiness](memory-and-thp.md).
