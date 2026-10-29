# Dashboards (OpenShift)

[Documentation index](../README.md#documentation)

OpenShift deploy includes Perses dashboards (`deploy/openshift/dashboard-*.yaml`) and a Thanos Querier datasource:

| Dashboard | File | Purpose |
|-----------|------|---------|
| Overview | [dashboard-overview.yaml](../deploy/openshift/dashboard-overview.yaml) | Cluster glance: memory / CPU / I/O utilization, pressure, imbalance |
| CPU | [dashboard-cpu.yaml](../deploy/openshift/dashboard-cpu.yaml) | Cluster and node CPU capacity, utilization, overcommit |
| I/O | [dashboard-io.yaml](../deploy/openshift/dashboard-io.yaml) | Device utilization, pressure, and host-to-device latency |
| Memory | [dashboard-memory.yaml](../deploy/openshift/dashboard-memory.yaml) | Cluster/node memory utilization, pressure, virtual commitment |
| KubeVirt Storage Latency Details (Dev Preview) | [dashboard-io-latency.yaml](../deploy/openshift/dashboard-io-latency.yaml) | Per-VMI storage path deep-dive (QMP, eBPF, QGA, virtqueues) |
| KubeVirt VM Memory Details (Dev Preview) | [dashboard-memory-detailed.yaml](../deploy/openshift/dashboard-memory-detailed.yaml) | Per-VMI THP/resident ratios, KSM, and node THP readiness |

Overview / CPU / I/O / Memory panels query `openshift:*` [recording rules](alerting.md#recording-rules). The storage and THP dashboards query exporter metrics directly.

For memory panel interpretation, see [THP and node memory readiness](memory-and-thp.md). For storage queries, see [example PromQL queries](example-queries.md).
