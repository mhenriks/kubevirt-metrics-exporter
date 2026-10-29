# Deployment

[Documentation index](../README.md#documentation)

## From a release

Download the install manifest from the [latest release](https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest):

OpenShift:

```bash
oc apply -f https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest/download/install-openshift.yaml
```

Kubernetes:

```bash
kubectl apply -f https://github.com/openshift-virtualization/kubevirt-metrics-exporter/releases/latest/download/install-kubernetes.yaml
```

## From source

Run these commands from the repository root. See [building and testing](../CONTRIBUTING.md#building-and-testing) for build prerequisites.

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

The OpenShift variant includes SecurityContextConstraints, worker node selector, ServiceMonitor for Prometheus scraping, Perses dashboards/datasource, and Prometheus alerting plus recording rules.

## Required capabilities

| Capability | Reason |
|-----------|--------|
| `hostPID` | Access VM virtqemud sockets via `/proc/<pid>/root/`; read host `/proc/buddyinfo`, `/proc/pagetypeinfo`, and `/proc/vmstat` for node memory metrics |
| `SYS_PTRACE` | Traverse `/proc/<pid>/root/` of other containers |
| `DAC_OVERRIDE` | Connect to virtqemud socket owned by qemu UID; read host proc nodes that restrict unprivileged access |
| `BPF` | Load and attach eBPF programs |
| `PERFMON` | Attach to kernel tracepoints and kprobes |
| `SYS_RESOURCE` | Increase eBPF map memory limits |

See [configuration](configuration.md) for collector settings and [dashboards](dashboards.md) for the OpenShift dashboard catalog.
