# Configuration

[Documentation index](../README.md#documentation)

The tables below show **binary defaults**, before deployment environment variables or explicit command-line flags are applied. Settings take precedence in this order: explicit flag, environment variable, built-in default. `NODE_NAME` is environment-only and is required; the DaemonSet supplies it from the node name.

Shared flags apply to all subsystems. Collector-specific settings use the `qmp`, `qga`, `ebpf`, `kvm`, or `cgroup` prefix.

## Changing a deployment

For example, disable guest-side collection and change the QMP poll interval on an existing deployment:

```bash
oc set env daemonset/kubevirt-metrics-exporter -n kubevirt-metrics-exporter --containers=exporter ENABLE_QGA=false QMP_POLL_INTERVAL=30s
oc rollout status daemonset/kubevirt-metrics-exporter -n kubevirt-metrics-exporter
```

Use `kubectl` in place of `oc` on Kubernetes. These changes update the pod template and trigger a rollout. For repeatable deployments, keep the same environment settings in your deployment manifests or Kustomize patches; reapplying the bundled manifests can reset settings they define.

Duration settings such as `QMP_POLL_INTERVAL` accept values like `30s` or `1m`. `QGA_TIMEOUT` and `EBPF_SCAN_INTERVAL` take integer seconds. Histogram `BOUNDARIES` are integer nanoseconds.

## Deployment defaults and TLS

The [base DaemonSet](../deploy/base/daemonset.yaml) explicitly enables QMP, QGA, eBPF block/NFS, KVM, and cgroup collection; NFS VFS kprobe tracing remains disabled. Both deployment variants expose plain HTTP health checks on port 8081, although the binary disables the separate health listener by default.

| Deployment | Metrics endpoint | Authentication |
|------------|------------------|----------------|
| Kubernetes/base | HTTP `/metrics` on port 8080 | No metrics authentication configured |
| OpenShift | HTTPS `/metrics` on port 8443 | Client certificate required (mTLS) |

The [OpenShift overlay](../deploy/openshift/kustomization.yaml) mounts a service-serving certificate and sets the TLS version and cipher configuration. The [ServiceMonitor](../deploy/openshift/metrics-servicemonitor.yaml) scrapes every 30 seconds using `scrapeClass: tls-client-certificate-auth`; cluster monitoring must provide that class and its client credentials.

When TLS is enabled, the exporter uses `TLS_CLIENT_CA_FILE` if supplied; otherwise it loads and watches `kube-system/extension-apiserver-authentication` for the client CA. A bearer token alone does not satisfy this metrics endpoint's client-certificate requirement. For TLS outside OpenShift, supply a serving certificate, key, and client CA bundle, and configure the scraper with a trusted client certificate.

The separate `/healthz` listener is for pod probes; a healthy pod does not establish that every collector is producing data.

## Shared

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--listen-address` | `LISTEN_ADDRESS` | `:8080` | Metrics server listen address |
| `--tls-cert-file` | `TLS_CERT_FILE` | _(empty)_ | TLS serving certificate; set with `TLS_KEY_FILE` to enable HTTPS and mTLS |
| `--tls-key-file` | `TLS_KEY_FILE` | _(empty)_ | TLS serving key |
| `--tls-client-ca-file` | `TLS_CLIENT_CA_FILE` | _(empty)_ | PEM client-CA bundle; when unset on OpenShift, uses `kube-system/extension-apiserver-authentication` |
| `--tls-min-version` | `TLS_MIN_VERSION` | `VersionTLS12` | Minimum TLS version; Autopilot supplies the resolved OpenShift TLS profile value |
| `--tls-cipher-suites` | `TLS_CIPHER_SUITES` | _(empty)_ | Comma-separated cipher names emitted by an OpenShift TLS profile (OpenSSL format, e.g. `ECDHE-ECDSA-AES128-GCM-SHA256`); supplied by Autopilot for a custom/profile-specific policy |
| `--tls-groups` | `TLS_GROUPS` | _(empty)_ | Comma-separated group names from an OpenShift TLS profile (OpenSSL format, e.g. `X25519`) |
| `--health-listen-address` | `HEALTH_LISTEN_ADDRESS` | _(empty)_ | Plain HTTP address for health checks; Kubernetes and OpenShift deployments use `:8081` while metrics use `:8080` and `:8443`, respectively |
| `--log-level` | `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `--boundaries` | `BOUNDARIES` | `10000000,100000000,1000000000` | Histogram bucket boundaries in nanoseconds |
| | `NODE_NAME` | (required) | Node name, typically from downward API |
| `--namespaces` | `NAMESPACES` | (all) | Comma-separated namespace filter (QMP, QGA, and eBPF) |
| `--cri-socket` | `CRI_SOCKET` | `/run/crio/crio.sock` | CRI socket path for container discovery (shared by QMP and QGA) |

## QMP

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-qmp` | `ENABLE_QMP` | `true` | Enable QMP collection |
| `--qmp-poll-interval` | `QMP_POLL_INTERVAL` | `1m` | VM scrape interval |
| `--qmp-concurrency` | `QMP_CONCURRENCY` | `8` | Max parallel QMP operations |
| `--qmp-timeout` | `QMP_TIMEOUT` | `5s` | Per-operation QMP timeout |
| `--qmp-label-filter` | `QMP_LABEL_FILTER` | | Additional pod label selector |

## QGA

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-qga` | `ENABLE_QGA` | `true` | Enable QGA guest-side I/O collection |
| `--qga-poll-interval` | `QGA_POLL_INTERVAL` | `1m` | Guest metrics poll interval |
| `--qga-timeout` | `QGA_TIMEOUT` | `10` | Per-command QGA timeout (seconds) |
| `--qga-exec-wait` | `QGA_EXEC_WAIT` | `1s` | Wait between guest-exec and guest-exec-status |
| `--qga-retries` | `QGA_RETRIES` | `10` | Max consecutive failures before falling back to probing every 60 poll intervals |
| `--qga-concurrency` | `QGA_CONCURRENCY` | `8` | Max parallel QGA operations |
| `--qga-label-filter` | `QGA_LABEL_FILTER` | | Additional pod label selector for QGA |

## eBPF

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-ebpf` | `ENABLE_EBPF` | `true` | Enable eBPF collection |
| `--enable-ebpf-block` | `ENABLE_EBPF_BLOCK` | `true` | Enable block I/O tracing |
| `--enable-ebpf-nfs` | `ENABLE_EBPF_NFS` | `true` | Enable NFS tracing |
| `--enable-ebpf-nfs-kprobe` | `ENABLE_EBPF_NFS_KPROBE` | `false` | Enable NFS VFS kprobe tracing |
| `--ebpf-scan-interval` | `EBPF_SCAN_INTERVAL` | `30` | Device-to-pod resolution interval (seconds) |
| `--ebpf-block-map-size` | `EBPF_BLOCK_MAP_SIZE` | `10240` | Max entries in the block start timestamp map |
| `--ebpf-nfs-map-size` | `EBPF_NFS_MAP_SIZE` | `10240` | Max entries in the NFS start timestamp map |
| `--ebpf-nfs-kprobe-map-size` | `EBPF_NFS_KPROBE_MAP_SIZE` | `10240` | Max entries in the NFS kprobe start timestamp map |
| `--ebpf-proc-path` | `EBPF_PROC_PATH` | `/proc` | Host proc filesystem path |

## KVM

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-kvm` | `ENABLE_KVM` | `true` | Enable KVM debugfs stats collection |
| `--kvm-poll-interval` | `KVM_POLL_INTERVAL` | `30s` | Poll interval for KVM counters |

## Cgroup

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--enable-cgroup` | `ENABLE_CGROUP` | `true` | Enable cgroup v2 memory and kernel thread collection |
| `--cgroup-poll-interval` | `CGROUP_POLL_INTERVAL` | `30s` | Poll interval for cgroup stats |
