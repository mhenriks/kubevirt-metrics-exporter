# Deployment

[Documentation index](../README.md#documentation)

## Prerequisites

- A cluster running KubeVirt/OpenShift Virtualization, with Linux nodes hosting the VMs to observe.
- Permissions to create the namespace, cluster RBAC, and the DaemonSet with the [required host access](#required-capabilities). OpenShift also needs permission to create the bundled SCC and its RBAC.
- Prometheus Operator's `PrometheusRule` CRD: both variants include rules. OpenShift additionally requires the `ServiceMonitor`, `PersesDashboard`, and `PersesDatasource` APIs and controllers that consume them.
- The manifests mount the CRI-O socket at `/run/crio/crio.sock`. For a different runtime/socket, adapt both the hostPath/mount and `CRI_SOCKET`; changing the environment variable alone does not change the mount.
- Collector-specific host support: eBPF tracing needs the corresponding kernel facilities, KVM collection reads debugfs, and memory collection uses cgroup v2 and host `/proc`. Windows guest metrics also require PowerShell and a QEMU Guest Agent allowing `guest-exec`.

## Monitoring integration

| Variant | Included integration | Required cluster setup |
|---------|----------------------|------------------------|
| OpenShift | Metrics Service and ServiceMonitor, SCC, worker node selector, Perses dashboards/datasource, alerting and recording rules | Monitoring that selects the ServiceMonitor and rules, the `tls-client-certificate-auth` scrape class, and Perses integration with Thanos access |
| Kubernetes | DaemonSet and alerting/recording rules | Configure Prometheus to discover and scrape pod port 8080 (`/metrics`) and select the rules; no Service, ServiceMonitor, or PodMonitor is bundled |

The OpenShift namespace carries `openshift.io/cluster-monitoring: "true"`. Its metrics endpoint uses mTLS; see [deployment defaults and TLS](configuration.md#deployment-defaults-and-tls). The [Perses datasource](../deploy/openshift/datasource.yaml) points to OpenShift's Thanos Querier and references `thanos-querier-secret`; ensure your Perses integration supplies the referenced credentials and service CA.

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

Run these commands from the repository root with standalone `kustomize`, `make`, and `oc` or `kubectl` installed. These targets render manifests and deploy an existing image; they do not build the current checkout. See [building and testing](../CONTRIBUTING.md#building-and-testing) for build prerequisites.

OpenShift:

```bash
make deploy
```

Kubernetes:

```bash
make deploy-kubernetes
```

To build the current checkout, push it to a registry the nodes can access, and deploy it on OpenShift:

```bash
make push IMAGE=quay.io/myuser/kubevirt-metrics-exporter TAG=v0.1.0
make deploy IMAGE=quay.io/myuser/kubevirt-metrics-exporter TAG=v0.1.0
```

### Current Kubernetes image limitation

The Kubernetes overlay currently replaces the image with `localhost/kubevirt-metrics-exporter:e2e`. Consequently, `make deploy-kubernetes` fails its expected-image check, and `make deploy-manifest-kubernetes` retains the local image instead of applying `IMAGE`/`TAG`. Before deploying this overlay, correct its image mapping to the intended registry image and inspect the rendered manifest. Inspect release manifests as well: releases generated with this overlay can contain the same local image.

## Verify the deployment

```bash
oc rollout status daemonset/kubevirt-metrics-exporter -n kubevirt-metrics-exporter
oc get pods -n kubevirt-metrics-exporter -l app=kubevirt-metrics-exporter -o wide
```

Use `kubectl` for Kubernetes. Then check your Prometheus targets for a successful scrape and query `up{job="kubevirt-metrics-exporter"}` for the bundled OpenShift setup (use your configured job label for a custom scraper). Pod readiness alone does not verify scraping or collector output.

See the [dashboard catalog](dashboards.md) and [example queries](example-queries.md) to explore the collected data.

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
