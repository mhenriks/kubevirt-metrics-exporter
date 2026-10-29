# KubeVirt Metrics Exporter

A Prometheus exporter for OpenShift Virtualization that combines storage I/O latency tracing with VM memory and THP observability. It runs as a DaemonSet with several independent collection subsystems in a single container:

- **QMP subsystem** — connects to each VM's QEMU Monitor Protocol to collect per-disk read/write/flush latency histograms directly from the hypervisor
- **QGA subsystem** — uses the QEMU Guest Agent to collect guest-side I/O latency and IOPS from Windows VMs via Windows Performance Counters (PDH raw counters)
- **eBPF subsystem** — attaches kernel tracepoints and kprobes to capture block and NFS I/O latency across the node, correlated to Kubernetes pods and PersistentVolumeClaims
- **KVM subsystem** — reads KVM hypervisor event counters (exits, hypercalls, TLB flushes, halt exits) from the kernel debugfs at `/sys/kernel/debug/kvm/`
- **Cgroup subsystem** — reads per-VMI cgroup v2 memory stats (anonymous, THP) from QEMU process cgroups; per-node buddy/pagetype THP readiness (`/proc/buddyinfo`, `/proc/pagetypeinfo`); and kernel thread CPU (`khugepaged`, `ksmd`) plus KSM profit

All subsystems are independently enabled/disabled and degrade gracefully if one fails to start.

## Quick start

Install the manifest from the [latest release](https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest) on your cluster.

### OpenShift

```bash
oc apply -f https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest/download/install-openshift.yaml
oc rollout status daemonset/kubevirt-metrics-exporter -n kubevirt-metrics-exporter
```

The OpenShift deployment includes monitoring integration, Perses dashboards, and alerting and recording rules. See the [dashboard catalog](docs/dashboards.md) for available dashboards and their dependencies.

### Kubernetes

```bash
kubectl apply -f https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest/download/install-kubernetes.yaml
kubectl rollout status daemonset/kubevirt-metrics-exporter -n kubevirt-metrics-exporter
```

For source deployments, custom images, and required host capabilities, see the [deployment guide](docs/deployment.md). Collector settings are documented in the [configuration reference](docs/configuration.md).

## Documentation

| I want to… | Guide |
|------------|-------|
| Install or customize a deployment | [Deployment](docs/deployment.md) |
| Configure collectors, polling, or TLS | [Configuration](docs/configuration.md) |
| Look up metrics, labels, and collection limitations | [Metric reference](docs/metrics.md) |
| Write PromQL queries | [Example queries](docs/example-queries.md) |
| Find dashboards and their dependencies | [Dashboards](docs/dashboards.md) |
| Interpret VM memory and node THP readiness | [Memory and THP](docs/memory-and-thp.md) |
| Understand alerts and recording rules | [Alerting](docs/alerting.md) |
| Export historical metrics | [Exporting metrics](docs/exporting-metrics.md) |
| Build, test, or contribute | [Contributing](CONTRIBUTING.md) |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build prerequisites, testing commands, and the contribution workflow.
