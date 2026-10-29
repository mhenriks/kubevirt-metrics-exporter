# Exporting historical KME metrics

[Documentation index](../README.md#documentation)

[`scripts/export-kme-metrics.py`](../scripts/export-kme-metrics.py) exports every metric defined by this exporter from a Prometheus-compatible server using its range-query API. This includes `kme_*`, `kubevirt_vmi_*`, `container_memory_*`, and `node_ksmd_general_profit_bytes` series, while excluding generic Go/process metrics supplied by the Prometheus client library. It exports the preceding 24 hours at 30-second resolution by default, matching the bundled OpenShift ServiceMonitor scrape interval, and writes the API's JSON response to a timestamped file.

Run the examples from the repository root. The script requires Python 3. Automatic service discovery and port-forwarding additionally require `oc` or `kubectl`; no `jq`, `curl`, or platform-specific `date` implementation is required.

```bash
./scripts/export-kme-metrics.py
```

The exporter `/metrics` endpoint only provides the current scrape; historical export requires Prometheus, Thanos, or another compatible query server that retains the requested period. By default the script discovers `svc/thanos-querier` (first in `openshift-monitoring`, then cluster-wide), port-forwards it, and uses the current `oc` token when available. Pass `--server` to use an already reachable query server instead. Pass `--start`, `--end`, and `--step` for a fixed window, and use `--bearer-token` (or `PROMETHEUS_BEARER_TOKEN`) and `--ca-file` when the server requires authentication or a custom CA. Timestamps use RFC 3339 and work on macOS and Linux. Run `./scripts/export-kme-metrics.py --help` for all options.

To override discovery, explicitly select a resource for the managed `oc`/`kubectl` port-forward:

```bash
./scripts/export-kme-metrics.py --port-forward svc/thanos-querier --namespace openshift-monitoring
```

It prefers `oc` when available, otherwise uses `kubectl`, waits for the forwarded endpoint, and stops the background process when the export finishes or fails. Use `--local-port`, `--remote-port`, `--port-forward-scheme http`, or `--client kubectl` to override those defaults.

Use `--vmi-namespace` and `--vmi-name` to restrict the range query to a VMI. The filters apply to the Prometheus `namespace` and `name` labels, so node-only metrics and pod-level eBPF metrics without those labels are intentionally excluded from a VMI-filtered export. `--port-forward-namespace` controls where the query service is discovered; the legacy `--namespace` spelling remains an alias for it.

For large windows, pass `--gzip` to compress the single JSON response. The default filename then ends in `.json.gz`. Alternatively, `--output -` writes only the response body to standard output, so it can be streamed into another tool:

```bash
./scripts/export-kme-metrics.py --output - --gzip > kme-metrics.json.gz
```

Pass `--output-format openmetrics` to convert the returned range samples to OpenMetrics text instead. This is useful for producing local TSDB blocks with `promtool`; it is a sampled export at the selected `--step`, not a Prometheus backup.

```bash
./scripts/export-kme-metrics.py --output-format openmetrics --output kme-metrics.om
promtool tsdb create-blocks-from openmetrics kme-metrics.om ./tsdb-blocks
```
