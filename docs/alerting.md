# Alerting and recording rules

[Documentation index](../README.md#documentation)

Prometheus rules are included in `deploy/prometheus-rules/` and deployed automatically with `make deploy` / `make deploy-kubernetes`.

## Alerting

The alerting rules cover two areas:

**Workload health** — alerts on high storage I/O latency (hypervisor-side, guest-side, and node-side) and virtqueue saturation. Individual alerts remain deployed alongside storage-class summaries:

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

| Storage-class summary | Severity | Condition |
|-----------------------|----------|-----------|
| StorageClassVMILatencyHigh | warning | Read/write tail above 100ms; population threshold for 10m |
| StorageClassVMIFlushLatencyHigh | warning | Estimated P99 flush latency above 500ms; population threshold for 10m |
| StorageClassBlockLatencyHigh | warning | Block read/write tail above 100ms; population threshold for 10m |
| StorageClassNFSLatencyHigh | warning | Estimated P99 NFS latency above 250ms; population threshold for 10m |
| StorageClassGuestLatencyHigh | warning | Guest average above 100ms; population threshold for 10m |

The storage-class population threshold is **≥10 affected VMIs, or ≥10% affected with at least 3 affected VMIs**. Individual alerts continue detecting isolated incidents below that threshold. These counts indicate the extent of degradation; they do not establish its cause or assign responsibility to a VM owner.

All alerts set `kubernetes_operator_part_of="kubevirt"`. Dedicated VMI alerts set `kubernetes_operator_component="kubevirt"` and `operator_health_impact="none"` so the Virtualization alert matcher places them with VM workloads. Storage-class, node, exporter-health, and generic pod/PVC alerts set `kubernetes_operator_component="kubevirt-metrics-exporter"` and `operator_health_impact="none"` so they remain cluster-level alerts. The pod/PVC alerts also cover non-VMI workloads, so they cannot be classified as VM-only alerts. These labels classify alerts in the console; they do not configure authorization or notification delivery. `operator_health_impact="none"` is deliberate: storage latency or a metrics-exporter fault does not establish that the virtualization operator is unhealthy. Severity still expresses the urgency of the alert.

Histogram summaries require at least 100 operations per disk/pod-volume and operation in the preceding 5m, except flush, which uses 10m to match its P99 window. The active denominator uses the same activity floor. Read/write and block summaries require more than 1% of operations above 100ms. Flush and NFS summaries preserve the existing estimated P99 thresholds of 500ms and 250ms and their 10m/5m measurement windows. Quantiles use all histogram buckets and accept both `le="1"` and `le="1.0"`. With the default 10ms, 100ms and 1s buckets, sub-second quantiles are interpolated estimates. Custom `--boundaries` must retain 100ms for the read/write and block summaries. Individual alert thresholds and persistence, the QMP-only node condition, and `VMIDiskSaturated` remain unchanged.

Intermediate `vmi:kme_*_latency_{affected,active}:bool` records retain namespace, name, node, pod, operation and available disk/PVC labels without requiring PVC metadata. `pod:kme_{block,nfs}_io_latency_{affected,active}:bool` records and individual block/NFS alerts also retain non-VMI pods, including backup pods. Individual block/NFS alerts keep pod/PVC identity independent of VMI metadata; storage-class summaries tolerate up to five minutes of missing VMI metadata before dropping the mapping. PVC/storage-class metadata is required only for storage-class summaries; unenriched workloads remain covered by individual alerts. Disks are deduplicated into VMIs before counting. Guest summaries use VMIs reporting guest latency as their denominator. The node summary derives from per-VMI QMP records with the original combined-disk/read-write P99 and positive-activity denominator, independently of the storage-class summary floors.

Storage-class and node alerts use the exporter namespace; individual alerts retain their workload namespace. Each collector detects independently; collector agreement is not required. This repository does not deploy Alertmanager routes or inhibition, so a storage-class summary can notify alongside its individual workload alerts. Deployments that need fewer notifications must validate a grouping or inhibition policy against the labels already present on the alerts.

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

## Recording rules

Cluster glance dashboards depend on recording rules in `deploy/prometheus-rules/recording/`:

| File | Series prefix | Notes |
|------|---------------|-------|
| `cpu.yaml` | `openshift:node:cpu:*`, `openshift:cluster:cpu:*`, `openshift:vm:cpu:*` | Uses kube-state-metrics, cAdvisor, KubeVirt vCPU metrics |
| `io.yaml` | `openshift:node:io:*`, `openshift:cluster:io:*` | Utilization from node_exporter; latency from `kme_system_block_io_latency_seconds_*` |
| `memory.yaml` | `openshift:node:memory:*`, `openshift:cluster:memory:*`, `openshift:vm:memory:*` | Uses node_exporter, cAdvisor, kube-state-metrics, KubeVirt memory metrics |

These are separate from the alerting `PrometheusRule` and use the same namespace/labels as the existing KME alerts.
