#!/usr/bin/env python3
"""Generate promtool fixtures for individual and storage-class latency rules.

Scenarios, all in namespace "ns":

- px-pct: 3 of 4 VMIs have a write tail above 100ms, reads are fast.
  Percentage branch fires (75%). Read does not fire.
- px-few: 2 of 4 VMIs are slow. Diagnostic count remains; admin alert stays quiet.
- px-wide: 11 of 111 VMIs are slow (under 10%). Count branch fires.
- px-quiet: 3 VMIs are slow but have fewer than 100 operations in 5m.
- px-flush: 3 VMIs have a flush tail above 1s.
- px-flush-mild: 3 VMIs have estimated P99 flush latency above 500ms and below 1s.
- px-block: 3 of 4 VMIs are slow at the block layer, plus one slow pod
  that is not a running VMI.
- px-nfs: 3 of 4 VMIs have an NFS tail above 1s.
- px-nfs-mild: 3 VMIs have estimated P99 NFS latency above 250ms and below 1s.
- px-guest: 3 of 4 VMIs have guest average latency above 100ms.
"""

import pathlib

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[2]
RULE_SOURCE = ROOT / "deploy/prometheus-rules/prometheusrule.yaml"
RULES_OUT = pathlib.Path(__file__).resolve().parent / "kme-prometheus-rules.yml"
TEST_OUT = (
    pathlib.Path(__file__).resolve().parent / "kme_storage_latency_alert_tests.yml"
)

STEPS = 60
NAMESPACE = "ns"
VM_OWNER_ALERTS = {
    "VMIStorageWriteLatencyHigh",
    "VMIStorageReadLatencyHigh",
    "VMIStorageFlushLatencyHigh",
    "VMIGuestStorageLatencyHigh",
    "VMIDiskSaturated",
}
WORKLOAD_NAMESPACE_ALERTS = VM_OWNER_ALERTS | {
    "PVCBlockLatencyHigh",
    "PVCNFSLatencyHigh",
}


def counter(step):
    return "0+%dx%d" % (step, STEPS)


def gauge(value):
    return "%s+0x%d" % (value, STEPS)


def main():
    document = yaml.safe_load(RULE_SOURCE.read_text())
    groups = document["spec"]["groups"]
    individual_names = {
        "VMIStorageWriteLatencyHigh",
        "VMIStorageReadLatencyHigh",
        "VMIStorageFlushLatencyHigh",
        "VMIGuestStorageLatencyHigh",
        "PVCBlockLatencyHigh",
        "PVCNFSLatencyHigh",
    }
    alert_names = {
        rule["alert"] for group in groups for rule in group["rules"] if "alert" in rule
    }
    required = {
        "StorageClassVMILatencyHigh",
        "StorageClassVMIFlushLatencyHigh",
        "StorageClassBlockLatencyHigh",
        "StorageClassNFSLatencyHigh",
        "StorageClassGuestLatencyHigh",
        "NodeVMStorageLatencyWidespread",
        "VMIDiskSaturated",
    }
    if (required | individual_names) - alert_names:
        raise SystemExit(
            "default rules must contain summaries and individual latency alerts"
        )
    for group in groups:
        for rule in group["rules"]:
            if "alert" not in rule:
                continue
            labels = rule["labels"]
            expected = {
                "kubernetes_operator_part_of": "kubevirt",
                "kubernetes_operator_component": (
                    "kubevirt" if rule["alert"] in VM_OWNER_ALERTS else "kubevirt-metrics-exporter"
                ),
                "operator_health_impact": "none",
            }
            if any(labels.get(key) != value for key, value in expected.items()):
                raise SystemExit("incorrect operator labels on %s" % rule["alert"])
            if (
                rule["alert"] not in WORKLOAD_NAMESPACE_ALERTS
                and labels.get("namespace") != "kubevirt-metrics-exporter"
            ):
                raise SystemExit("missing component namespace on %s" % rule["alert"])
    RULES_OUT.write_text(yaml.safe_dump({"groups": groups}, sort_keys=False))

    series = {}

    def add(name, values):
        existing = series.get(name)
        if existing is not None and existing != values:
            raise SystemExit("duplicate series %s" % name)
        series[name] = values

    def pvc(storage_class, claim):
        add(
            'kube_persistentvolumeclaim_info{namespace="%s",persistentvolumeclaim="%s",storageclass="%s"}'
            % (NAMESPACE, claim, storage_class),
            gauge(1),
        )

    def qmp(name, claim, operation, count_step, bucket_step, le):
        labels = (
            'namespace="%s",name="%s",disk="root",persistentvolumeclaim="%s",operation="%s"'
            % (NAMESPACE, name, claim, operation)
        )
        add(
            "kubevirt_vmi_storage_io_latency_seconds_count{%s}" % labels,
            counter(count_step),
        )
        add(
            'kubevirt_vmi_storage_io_latency_seconds_bucket{%s,le="%s"}' % (labels, le),
            counter(bucket_step),
        )
        add(
            'kubevirt_vmi_storage_io_latency_seconds_bucket{%s,le="+Inf"}' % labels,
            counter(count_step),
        )

    def pod_hist(metric, pod, claim, operation, count_step, bucket_step, le):
        labels = 'namespace="%s",pod="%s",persistentvolumeclaim="%s",operation="%s"' % (
            NAMESPACE,
            pod,
            claim,
            operation,
        )
        add("%s_count{%s}" % (metric, labels), counter(count_step))
        add('%s_bucket{%s,le="%s"}' % (metric, labels, le), counter(bucket_step))
        add('%s_bucket{%s,le="+Inf"}' % (metric, labels), counter(count_step))

    def vmi(name, pod):
        add(
            'kubevirt_vmi_info{namespace="%s",name="%s",vmi_pod="%s",phase="running"}'
            % (NAMESPACE, name, pod),
            gauge(1),
        )

    def guest(name, claim, operation, value):
        add(
            'kubevirt_vmi_storage_guest_latency_avg_seconds{namespace="%s",name="%s",disk="root",persistentvolumeclaim="%s",operation="%s"}'
            % (NAMESPACE, name, claim, operation),
            gauge(value),
        )

    def vms(storage_class, prefix, slow, fast, operation="write", le="0.1", kind="qmp"):
        for index in range(slow + fast):
            name = "%s-%d" % (prefix, index)
            claim = "pvc-%s" % name
            pod = "virt-launcher-%s" % name
            is_slow = index < slow
            count_step = 100
            bucket_step = 90 if is_slow else 100
            pvc(storage_class, claim)
            if kind == "qmp":
                qmp(name, claim, operation, count_step, bucket_step, le)
            elif kind == "block":
                pod_hist(
                    "kme_block_io_latency_seconds",
                    pod,
                    claim,
                    operation,
                    count_step,
                    bucket_step,
                    le,
                )
                vmi(name, pod)
            elif kind == "nfs":
                pod_hist(
                    "kme_nfs_io_latency_seconds",
                    pod,
                    claim,
                    operation,
                    count_step,
                    bucket_step,
                    le,
                )
                vmi(name, pod)
            else:
                raise SystemExit("unknown kind %s" % kind)
            if le == "1":
                if kind == "qmp":
                    qmp(name, claim, operation, count_step, bucket_step, "0.1")
                elif kind == "nfs":
                    pod_hist(
                        "kme_nfs_io_latency_seconds",
                        pod,
                        claim,
                        operation,
                        count_step,
                        bucket_step,
                        "0.1",
                    )

    vms("px-pct", "pct", slow=3, fast=1, operation="write", le="0.1")
    vms("px-pct", "pct", slow=0, fast=4, operation="read", le="0.1")
    vms("px-few", "few", slow=2, fast=2, operation="write", le="0.1")
    vms("px-wide", "wide", slow=11, fast=100, operation="write", le="0.1")
    for index in range(3):
        name = "quiet-%d" % index
        claim = "pvc-%s" % name
        pvc("px-quiet", claim)
        qmp(name, claim, "write", count_step=10, bucket_step=0, le="0.1")

    vms("px-flush", "flush", slow=3, fast=0, operation="flush", le="1")
    vms("px-flush-mild", "flush-mild", slow=3, fast=0, operation="flush", le="1")
    for index in range(3):
        # Tail sits between 100ms and 1s, preserving interpolated P99 sensitivity.
        name = "flush-mild-%d" % index
        qmp(name, "pvc-%s" % name, "flush", count_step=100, bucket_step=90, le="0.1")

    vms("px-block", "block", slow=3, fast=1, operation="write", le="0.1", kind="block")
    pvc("px-block", "pvc-block-orphan")
    pod_hist(
        "kme_block_io_latency_seconds",
        "orphan-pod",
        "pvc-block-orphan",
        "write",
        100,
        90,
        "0.1",
    )

    vms("px-nfs", "nfs", slow=3, fast=1, operation="write", le="1", kind="nfs")
    vms(
        "px-nfs-mild", "nfs-mild", slow=3, fast=0, operation="write", le="1", kind="nfs"
    )
    for index in range(3):
        name = "nfs-mild-%d" % index
        pod_hist(
            "kme_nfs_io_latency_seconds",
            "virt-launcher-%s" % name,
            "pvc-%s" % name,
            "write",
            100,
            90,
            "0.1",
        )

    for index, value in enumerate((0.2, 0.2, 0.2, 0.01)):
        name = "guest-%d" % index
        claim = "pvc-%s" % name
        pvc("px-guest", claim)
        guest(name, claim, "write", value)

    for name in series:
        if (
            'name="flush-mild-' in name or 'pod="virt-launcher-nfs-mild-' in name
        ) and 'le="1"' in name:
            series[name] = counter(100)

    lines = [
        "# Generated by test/promtool/generate_kme_latency_tests.py.",
        "# Do not edit. Regenerate with hack/test-alert-rules.sh.",
        "rule_files:",
        "  - kme-prometheus-rules.yml",
        "",
        "evaluation_interval: 1m",
        "",
        "tests:",
        "  - interval: 1m",
        "    input_series:",
    ]
    for name, values in series.items():
        lines.append("      - series: '%s'" % name)
        lines.append("        values: '%s'" % values)

    lines.extend(
        [
            "",
            "    promql_expr_test:",
            "      - expr: storageclass:kme_vmi_io_latency_affected:count",
            "        eval_time: 20m",
            "        exp_samples:",
            *samples(
                "storageclass:kme_vmi_io_latency_affected:count",
                [
                    ("px-pct", "write", 3),
                    ("px-few", "write", 2),
                    ("px-wide", "write", 11),
                ],
            ),
            "      - expr: storageclass:kme_vmi_io_latency_active:count",
            "        eval_time: 20m",
            "        exp_samples:",
            *samples(
                "storageclass:kme_vmi_io_latency_active:count",
                [
                    ("px-pct", "read", 4),
                    ("px-pct", "write", 4),
                    ("px-few", "write", 4),
                    ("px-wide", "write", 111),
                ],
            ),
            "      - expr: storageclass:kme_vmi_flush_latency_affected:count",
            "        eval_time: 20m",
            "        exp_samples:",
            *samples(
                "storageclass:kme_vmi_flush_latency_affected:count",
                [("px-flush", "flush", 3), ("px-flush-mild", "flush", 3)],
            ),
            "      - expr: storageclass:kme_block_io_latency_affected:count",
            "        eval_time: 20m",
            "        exp_samples:",
            *samples(
                "storageclass:kme_block_io_latency_affected:count",
                [("px-block", "write", 3)],
            ),
            "      - expr: storageclass:kme_block_io_latency_active:count",
            "        eval_time: 20m",
            "        exp_samples:",
            *samples(
                "storageclass:kme_block_io_latency_active:count",
                [("px-block", "write", 4)],
            ),
            "      - expr: storageclass:kme_nfs_io_latency_affected:count",
            "        eval_time: 20m",
            "        exp_samples:",
            *samples(
                "storageclass:kme_nfs_io_latency_affected:count",
                [("px-nfs", "write", 3), ("px-nfs-mild", "write", 3)],
            ),
            "      - expr: storageclass:kme_guest_latency_affected:count",
            "        eval_time: 20m",
            "        exp_samples:",
            *samples(
                "storageclass:kme_guest_latency_affected:count",
                [("px-guest", "write", 3)],
            ),
            "      - expr: storageclass:kme_guest_latency_active:count",
            "        eval_time: 20m",
            "        exp_samples:",
            *samples(
                "storageclass:kme_guest_latency_active:count",
                [("px-guest", "write", 4)],
            ),
            "",
            "    alert_rule_test:",
            "      - eval_time: 8m",
            "        alertname: StorageClassVMILatencyHigh",
            "        exp_alerts: []",
            "      - eval_time: 40m",
            "        alertname: StorageClassVMILatencyHigh",
            "        exp_alerts:",
            *alert(
                "High write latency on storage class px-pct",
                qmp_description(3, "px-pct", "write", "100ms"),
                "px-pct",
                "write",
            ),
            *alert(
                "High write latency on storage class px-wide",
                qmp_description(11, "px-wide", "write", "100ms"),
                "px-wide",
                "write",
            ),
            "      - eval_time: 40m",
            "        alertname: StorageClassVMIFlushLatencyHigh",
            "        exp_alerts:",
            *alert(
                "High flush latency on storage class px-flush",
                flush_description(3, "px-flush"),
                "px-flush",
                "flush",
            ),
            *alert(
                "High flush latency on storage class px-flush-mild",
                flush_description(3, "px-flush-mild"),
                "px-flush-mild",
                "flush",
            ),
            "      - eval_time: 40m",
            "        alertname: StorageClassBlockLatencyHigh",
            "        exp_alerts:",
            *alert(
                "High block write latency on storage class px-block",
                block_description(3, "px-block", "write"),
                "px-block",
                "write",
            ),
            "      - eval_time: 40m",
            "        alertname: StorageClassNFSLatencyHigh",
            "        exp_alerts:",
            *alert(
                "High NFS write latency on storage class px-nfs",
                nfs_description(3, "px-nfs", "write"),
                "px-nfs",
                "write",
            ),
            *alert(
                "High NFS write latency on storage class px-nfs-mild",
                nfs_description(3, "px-nfs-mild", "write"),
                "px-nfs-mild",
                "write",
            ),
            "      - eval_time: 40m",
            "        alertname: StorageClassGuestLatencyHigh",
            "        exp_alerts:",
            *alert(
                "High guest write latency on storage class px-guest",
                guest_description(3, "px-guest", "write"),
                "px-guest",
                "write",
            ),
            "",
        ]
    )
    tests = yaml.safe_load("\n".join(lines))
    tests["tests"].extend(review_scenarios())
    for test in tests["tests"]:
        for alert_test in test.get("alert_rule_test", []):
            for expected_alert in alert_test.get("exp_alerts", []):
                labels = expected_alert["exp_labels"]
                labels.update(
                    kubernetes_operator_part_of="kubevirt",
                    kubernetes_operator_component=(
                        "kubevirt"
                        if alert_test["alertname"] in VM_OWNER_ALERTS
                        else "kubevirt-metrics-exporter"
                    ),
                    operator_health_impact="none",
                )
    TEST_OUT.write_text(yaml.safe_dump(tests, sort_keys=False))
    print("wrote %s" % RULES_OUT)
    print("wrote %s (%d scenarios)" % (TEST_OUT, len(tests["tests"])))


def samples(metric, rows):
    rendered = []
    for storage_class, operation, value in rows:
        rendered.append(
            '          - labels: \'%s{operation="%s",storageclass="%s"}\''
            % (metric, operation, storage_class)
        )
        rendered.append("            value: %d" % value)
    return rendered


def alert(summary, description, storage_class, operation):
    return [
        "          - exp_labels:",
        "              severity: warning",
        "              namespace: kubevirt-metrics-exporter",
        "              operation: %s" % operation,
        "              storageclass: %s" % storage_class,
        "            exp_annotations:",
        "              summary: %s" % yaml_quote(summary),
        "              description: %s" % yaml_quote(description),
    ]


def yaml_quote(text):
    return yaml.safe_dump(text, default_style='"').strip()


def qmp_description(count, storage_class, operation, threshold):
    return (
        "%d VMIs on storage class %s have more than 1%% of %s operations slower than %s. "
        "Fires when at least 10 VMIs are affected, or at least 10%% of active VMIs are affected and at least 3 are affected."
        % (count, storage_class, operation, threshold)
    )


def flush_description(count, storage_class):
    return (
        "%d VMIs on storage class %s have estimated P99 flush latency above 500ms. "
        "Durability-sensitive workloads on this storage will see slow commits. "
        "Fires when at least 10 VMIs are affected, or at least 10%% of active VMIs are affected and at least 3 are affected."
        % (count, storage_class)
    )


def block_description(count, storage_class, operation):
    return (
        "%d VMIs on storage class %s have more than 1%% of block %s operations slower than 100ms. "
        "Fires when at least 10 VMIs are affected, or at least 10%% of active VMIs are affected and at least 3 are affected."
        % (count, storage_class, operation)
    )


def nfs_description(count, storage_class, operation):
    return (
        "%d VMIs on storage class %s have estimated P99 NFS %s latency above 250ms. "
        "Fires when at least 10 VMIs are affected, or at least 10%% of active VMIs are affected and at least 3 are affected."
        % (count, storage_class, operation)
    )


def guest_description(count, storage_class, operation):
    return (
        "%d VMIs on storage class %s have average guest %s latency above 100ms. "
        "Fires when at least 10 VMIs are affected, or at least 10%% of VMIs reporting guest latency on that storage class are affected and at least 3 are affected."
        % (count, storage_class, operation)
    )


def review_scenarios():
    """Regression cases from human review: coverage, attribution and boundaries."""
    cases = []
    signals = {
        "qmp": ("StorageClassVMILatencyHigh", "write", qmp_description),
        "flush": ("StorageClassVMIFlushLatencyHigh", "flush", flush_description),
        "block": ("StorageClassBlockLatencyHigh", "write", block_description),
        "nfs": ("StorageClassNFSLatencyHigh", "write", nfs_description),
        "guest": ("StorageClassGuestLatencyHigh", "write", guest_description),
    }

    def workload(
        kind,
        name,
        slow=True,
        metadata=True,
        le="1",
        count_step=100,
        tail=10,
        node="node-a",
        disk="root",
    ):
        operation = "flush" if kind == "flush" else "write"
        pod = "virt-launcher-" + name
        claim = "pvc-" + name if metadata else ""
        labels = dict(
            namespace="ns",
            node=node,
            pod=pod,
            persistentvolumeclaim=claim,
            operation=operation,
        )
        inputs = []

        def add(metric, labels, values):
            selector = ",".join('%s="%s"' % item for item in labels.items())
            inputs.append(dict(series=metric + "{" + selector + "}", values=values))

        if metadata:
            add(
                "kube_persistentvolumeclaim_info",
                dict(namespace="ns", persistentvolumeclaim=claim, storageclass="sc"),
                gauge(1),
            )
        if kind in ("qmp", "flush", "guest"):
            labels.update(name=name, disk=disk)
        else:
            add(
                "kubevirt_vmi_info",
                dict(namespace="ns", name=name, vmi_pod=pod, phase="running"),
                gauge(1),
            )
        if kind == "guest":
            add(
                "kubevirt_vmi_storage_guest_latency_avg_seconds",
                labels,
                gauge(2 if slow else 0.01),
            )
        else:
            metric = {
                "qmp": "kubevirt_vmi_storage_io_latency_seconds",
                "flush": "kubevirt_vmi_storage_io_latency_seconds",
                "block": "kme_block_io_latency_seconds",
                "nfs": "kme_nfs_io_latency_seconds",
            }[kind]
            add(metric + "_count", labels, counter(count_step))
            for boundary in ("0.01", "0.1", le, "+Inf"):
                # A tail above one second for slow workloads; full cumulative histogram.
                step = count_step - tail if slow and boundary != "+Inf" else count_step
                add(metric + "_bucket", dict(labels, le=boundary), counter(step))
        return inputs

    def individual_labels(kind, claim=None):
        labels = dict(
            severity="warning",
            namespace="ns",
            node="node-a",
            pod="virt-launcher-vm",
            operation=signals[kind][1],
        )
        if kind in ("qmp", "flush", "guest"):
            labels["name"] = "vm"
        if kind in ("qmp", "flush", "guest"):
            labels["disk"] = "root"
        if claim is not None:
            labels["persistentvolumeclaim"] = claim
        return labels

    def node_alert():
        return dict(
            exp_labels=dict(
                namespace="kubevirt-metrics-exporter",
                node="node-a",
                severity="critical",
            ),
            exp_annotations=dict(
                summary="Widespread VM storage latency on node node-a",
                description="More than 50% of active VMs on node node-a have P99 storage I/O latency exceeding 100ms, suggesting a node- or storage-infrastructure problem rather than a single workload issue.",
            ),
        )

    def summary(kind, count):
        _, operation, describe = signals[kind]
        if kind == "qmp":
            description = describe(count, "sc", operation, "100ms")
            title = "High write latency on storage class sc"
        elif kind == "flush":
            description = describe(count, "sc")
            title = "High flush latency on storage class sc"
        else:
            description = describe(count, "sc", operation)
            title = (
                "High %swrite latency on storage class sc"
                % {"block": "block ", "nfs": "NFS ", "guest": "guest "}[kind]
            )
        return dict(
            exp_labels=dict(
                severity="warning",
                namespace="kubevirt-metrics-exporter",
                storageclass="sc",
                operation=operation,
            ),
            exp_annotations=dict(summary=title, description=description),
        )

    for kind, (alertname, operation, _) in signals.items():
        for slow, total in (
            (1, 1),
            (1, 2),
            (1, 9),
            (1, 10),
            (1, 11),
            (2, 20),
            (3, 30),
            (3, 31),
            (9, 100),
            (10, 101),
        ):
            inputs = []
            for index in range(total):
                inputs.extend(workload(kind, "vm-%d" % index, slow=index < slow))
            fires = slow >= 10 or (slow >= 3 and slow / total >= 0.1)
            cases.append(
                dict(
                    name="%s: %d of %d affected" % (kind, slow, total),
                    interval="1m",
                    input_series=inputs,
                    alert_rule_test=[
                        dict(eval_time="8m", alertname=alertname, exp_alerts=[]),
                        dict(
                            eval_time="40m",
                            alertname=alertname,
                            exp_alerts=[summary(kind, slow)] if fires else [],
                        ),
                    ],
                )
            )

    for kind in ("flush", "nfs"):
        for le in ("1", "1.0"):
            cases.append(
                dict(
                    name=kind + " bucket " + le,
                    interval="1m",
                    input_series=[
                        item
                        for i in range(3)
                        for item in workload(kind, "vm-%d" % i, le=le)
                    ],
                    alert_rule_test=[
                        dict(
                            eval_time="40m",
                            alertname=signals[kind][0],
                            exp_alerts=[summary(kind, 3)],
                        )
                    ],
                )
            )

    # Workload detection must survive both absent and empty PVC metadata.
    individual = {
        "qmp": (
            "VMIStorageWriteLatencyHigh",
            "High P99 write latency for VM ns/vm disk root",
            "P99 hypervisor-side write latency for disk root on VM ns/vm has exceeded 100ms for 10 minutes (current value: 1s). Note: reported value is approximate due to histogram bucket granularity.",
        ),
        "flush": (
            "VMIStorageFlushLatencyHigh",
            "High P99 flush latency for VM ns/vm disk root",
            "P99 flush latency for disk root on VM ns/vm has exceeded 500ms for 15 minutes. Durability-sensitive workloads (databases, journaling) using this disk will see slow commits/fsyncs.",
        ),
        "guest": (
            "VMIGuestStorageLatencyHigh",
            "Guest-side storage latency high on ns/vm",
            "Guest-observed average write latency for disk root on VM ns/vm has exceeded 100ms for 15 minutes.",
        ),
        "block": (
            "PVCBlockLatencyHigh",
            "High block write latency for PVC ns/ (pod virt-launcher-vm)",
            "P99 block write latency for PVC ns/ in pod virt-launcher-vm has exceeded 100ms for 10 minutes.",
        ),
        "nfs": (
            "PVCNFSLatencyHigh",
            "High NFS write latency for PVC ns/ (pod virt-launcher-vm)",
            "P99 NFS write latency for PVC ns/ in pod virt-launcher-vm has exceeded 250ms for 10 minutes.",
        ),
    }
    for kind, (alertname, title, description) in individual.items():
        for missing_label in (False, True):
            inputs = workload(kind, "vm", metadata=False)
            if missing_label:
                for item in inputs:
                    item["series"] = item["series"].replace(
                        ',persistentvolumeclaim=""', ""
                    )
            labels = individual_labels(kind)
            cases.append(
                dict(
                    name=kind + " without PVC " + str(missing_label),
                    interval="1m",
                    input_series=inputs,
                    alert_rule_test=[
                        dict(
                            eval_time="40m",
                            alertname=alertname,
                            exp_alerts=[
                                dict(
                                    exp_labels=labels,
                                    exp_annotations=dict(
                                        summary=title, description=description
                                    ),
                                )
                            ],
                        ),
                        dict(
                            eval_time="40m", alertname=signals[kind][0], exp_alerts=[]
                        ),
                    ],
                )
            )

    # Backup pods are not VMIs: they retain pod/PVC alerts but never enter VMI counts.
    for kind in ("block", "nfs"):
        inputs = [
            item
            for item in workload(kind, "vm", metadata=False)
            if not item["series"].startswith("kubevirt_vmi_info")
        ]
        alertname, title, description = individual[kind]
        labels = individual_labels(kind)
        cases.append(
            dict(
                name=kind + " backup pod",
                interval="1m",
                input_series=inputs,
                alert_rule_test=[
                    dict(
                        eval_time="40m",
                        alertname=alertname,
                        exp_alerts=[
                            dict(
                                exp_labels=labels,
                                exp_annotations=dict(
                                    summary=title, description=description
                                ),
                            )
                        ],
                    )
                ],
                promql_expr_test=[
                    dict(
                        expr="storageclass:kme_%s_io_latency_affected:count" % kind,
                        eval_time="20m",
                        exp_samples=[],
                    )
                ],
            )
        )

    # Each source retains diagnostic identity independently. The original node
    # alert uses QMP only and has no minimum affected population.
    for kind, signal in (
        ("qmp", "vmi_io"),
        ("block", "block_io"),
        ("nfs", "nfs_io"),
        ("guest", "guest"),
    ):
        inputs = workload(kind, "vm", metadata=False)
        labels = dict(
            namespace="ns",
            name="vm",
            node="node-a",
            pod="virt-launcher-vm",
            operation="write",
        )
        if kind in ("qmp", "guest"):
            labels["disk"] = "root"
        metric = "vmi:kme_%s_latency_affected:bool" % signal
        selector = ",".join('%s="%s"' % item for item in labels.items())
        cases.append(
            dict(
                name=kind + " isolated diagnostic",
                interval="1m",
                input_series=inputs,
                promql_expr_test=[
                    dict(
                        expr=metric,
                        eval_time="20m",
                        exp_samples=[
                            dict(labels=metric + "{" + selector + "}", value=1)
                        ],
                    )
                ],
                alert_rule_test=[
                    dict(
                        eval_time="40m",
                        alertname="NodeVMStorageLatencyWidespread",
                        exp_alerts=[node_alert()] if kind == "qmp" else [],
                    ),
                    dict(eval_time="40m", alertname=signals[kind][0], exp_alerts=[]),
                ],
            )
        )

    for kind in ("block", "nfs", "guest"):
        inputs = [
            item
            for i in range(3)
            for item in workload(kind, "vm-%d" % i, metadata=False)
        ]
        cases.append(
            dict(
                name=kind + " population is not QMP node trouble",
                interval="1m",
                input_series=inputs,
                alert_rule_test=[
                    dict(
                        eval_time="40m",
                        alertname="NodeVMStorageLatencyWidespread",
                        exp_alerts=[],
                    )
                ],
            )
        )

    for kind in ("block", "nfs"):
        inputs = [
            item
            for item in workload(kind, "backup", metadata=False)
            if not item["series"].startswith("kubevirt_vmi_info")
        ]
        pod_metric = "pod:kme_%s_io_latency_affected:bool" % kind
        cases.append(
            dict(
                name=kind + " backup pod attribution",
                interval="1m",
                input_series=inputs,
                promql_expr_test=[
                    dict(
                        expr=pod_metric,
                        eval_time="20m",
                        exp_samples=[
                            dict(
                                labels=pod_metric
                                + '{namespace="ns",node="node-a",pod="virt-launcher-backup",operation="write"}',
                                value=1,
                            )
                        ],
                    ),
                    dict(
                        expr="vmi:kme_%s_io_latency_affected:bool" % kind,
                        eval_time="20m",
                        exp_samples=[],
                    ),
                ],
                alert_rule_test=[
                    dict(eval_time="40m", alertname=signals[kind][0], exp_alerts=[])
                ],
            )
        )

    for slow, total in ((1, 1), (2, 2), (3, 6), (3, 5)):
        inputs = [
            item
            for i in range(total)
            for item in workload("qmp", "vm-%d" % i, metadata=False, slow=i < slow)
        ]
        inputs += workload("qmp", "other", metadata=False, slow=False, node="node-b")
        expected = [node_alert()] if slow / total > 0.5 else []
        cases.append(
            dict(
                name="QMP node population %d/%d" % (slow, total),
                interval="1m",
                input_series=inputs,
                alert_rule_test=[
                    dict(
                        eval_time="40m",
                        alertname="NodeVMStorageLatencyWidespread",
                        exp_alerts=expected,
                    )
                ],
            )
        )

    # Preserve sub-second sensitivity of the individual P99 flush/NFS alerts.
    for kind in ("flush", "nfs"):
        inputs = workload(kind, "vm", metadata=False)
        for item in inputs:
            if 'le="1"' in item["series"]:
                item["values"] = counter(100)
        alertname, title, description = individual[kind]
        labels = individual_labels(kind)
        cases.append(
            dict(
                name=kind + " sub-second tail",
                interval="1m",
                input_series=inputs,
                alert_rule_test=[
                    dict(
                        eval_time="40m",
                        alertname=alertname,
                        exp_alerts=[
                            dict(
                                exp_labels=labels,
                                exp_annotations=dict(
                                    summary=title, description=description
                                ),
                            )
                        ],
                    ),
                    dict(eval_time="40m", alertname=signals[kind][0], exp_alerts=[]),
                ],
            )
        )
    for kind in ("qmp", "flush", "block", "nfs"):
        for step, tail, fires in (
            (0, 0, False),
            (10, 10, kind == "flush"),
            (20, 2, True),
            (100, 1, False),
        ):
            cases.append(
                dict(
                    name="%s activity/tail boundary %d/%d" % (kind, step, tail),
                    interval="1m",
                    input_series=[
                        item
                        for i in range(3)
                        for item in workload(
                            kind, "vm-%d" % i, count_step=step, tail=tail
                        )
                    ],
                    alert_rule_test=[
                        dict(
                            eval_time="40m",
                            alertname=signals[kind][0],
                            exp_alerts=[summary(kind, 3)] if fires else [],
                        )
                    ],
                )
            )

    inputs = workload("qmp", "vm", disk="root") + workload("qmp", "vm", disk="data")
    inputs = list({item["series"]: item for item in inputs}.values())
    cases.append(
        dict(
            name="two disks count as one VMI",
            interval="1m",
            input_series=inputs,
            promql_expr_test=[
                dict(
                    expr="storageclass:kme_vmi_io_latency_affected:count",
                    eval_time="20m",
                    exp_samples=[
                        dict(
                            labels='storageclass:kme_vmi_io_latency_affected:count{storageclass="sc",operation="write"}',
                            value=1,
                        )
                    ],
                ),
            ],
        )
    )
    # A backup pod retains its PVC identity even when PVC metadata is absent.
    for kind in ("block", "nfs"):
        for pvc_info in (False, True):
            inputs = [
                item
                for item in workload(kind, "vm")
                if not item["series"].startswith("kubevirt_vmi_info")
                and (
                    pvc_info
                    or not item["series"].startswith("kube_persistentvolumeclaim_info")
                )
            ]
            alertname, title, description = individual[kind]
            labels = individual_labels(kind, claim="pvc-vm")
            cases.append(
                dict(
                    name=kind + " backup PVC metadata " + str(pvc_info),
                    interval="1m",
                    input_series=inputs,
                    alert_rule_test=[
                        dict(
                            eval_time="40m",
                            alertname=alertname,
                            exp_alerts=[
                                dict(
                                    exp_labels=labels,
                                    exp_annotations=dict(
                                        summary=title.replace("ns/ ", "ns/pvc-vm "),
                                        description=description.replace(
                                            "ns/ ", "ns/pvc-vm "
                                        ),
                                    ),
                                )
                            ],
                        ),
                        dict(
                            eval_time="40m", alertname=signals[kind][0], exp_alerts=[]
                        ),
                    ],
                )
            )

    inputs = workload("qmp", "vm", metadata=False)
    for item in inputs:
        item["series"] = item["series"].replace('operation="write"', 'operation="read"')
    _, title, description = individual["qmp"]
    cases.append(
        dict(
            name="isolated QMP read",
            interval="1m",
            input_series=inputs,
            alert_rule_test=[
                dict(
                    eval_time="40m",
                    alertname="VMIStorageReadLatencyHigh",
                    exp_alerts=[
                        dict(
                            exp_labels=dict(
                                severity="warning",
                                namespace="ns",
                                name="vm",
                                node="node-a",
                                pod="virt-launcher-vm",
                                disk="root",
                                operation="read",
                            ),
                            exp_annotations=dict(
                                summary=title.replace("write", "read"),
                                description=description.replace("write", "read"),
                            ),
                        )
                    ],
                )
            ],
        )
    )
    # A usable PVC label must not make individual detection depend on kube-state-metrics.
    for kind, (alertname, title, description) in individual.items():
        inputs = workload(kind, "vm", metadata=False)
        for item in inputs:
            item["series"] = item["series"].replace(
                'persistentvolumeclaim=""', 'persistentvolumeclaim="pvc-vm"'
            )
        cases.append(
            dict(
                name=kind + " PVC label without metadata",
                interval="1m",
                input_series=inputs,
                alert_rule_test=[
                    dict(
                        eval_time="40m",
                        alertname=alertname,
                        exp_alerts=[
                            dict(
                                exp_labels=individual_labels(kind, claim="pvc-vm"),
                                exp_annotations=dict(
                                    summary=title.replace("ns/ ", "ns/pvc-vm "),
                                    description=description.replace(
                                        "ns/ ", "ns/pvc-vm "
                                    ),
                                ),
                            )
                        ],
                    ),
                    dict(eval_time="40m", alertname=signals[kind][0], exp_alerts=[]),
                ],
            )
        )

    # Summaries keep the same sub-second thresholds, for either bucket spelling.
    for kind in ("flush", "nfs"):
        for le in ("1", "1.0"):
            inputs = [
                item for i in range(3) for item in workload(kind, "vm-%d" % i, le=le)
            ]
            for item in inputs:
                if 'le="%s"' % le in item["series"]:
                    item["values"] = counter(100)
            cases.append(
                dict(
                    name=kind + " sub-second population bucket " + le,
                    interval="1m",
                    input_series=inputs,
                    alert_rule_test=[
                        dict(
                            eval_time="40m",
                            alertname=signals[kind][0],
                            exp_alerts=[summary(kind, 3)],
                        )
                    ],
                )
            )

    # The original node condition has no 100-operation activity floor.
    cases.append(
        dict(
            name="QMP node low activity preserves existing condition",
            interval="1m",
            input_series=workload("qmp", "vm", metadata=False, count_step=10, tail=2),
            alert_rule_test=[
                dict(
                    eval_time="8m",
                    alertname="NodeVMStorageLatencyWidespread",
                    exp_alerts=[],
                ),
                dict(
                    eval_time="40m",
                    alertname="NodeVMStorageLatencyWidespread",
                    exp_alerts=[node_alert()],
                ),
                dict(eval_time="40m", alertname=signals["qmp"][0], exp_alerts=[]),
            ],
        )
    )
    # Node P99 still combines disks and operations; a single slow disk or write
    # signal must not turn the node condition into a worst-disk/worst-operation test.
    for dimension in ("disk", "operation"):
        inputs = workload("qmp", "vm", metadata=False)
        fast = workload(
            "qmp",
            "vm",
            metadata=False,
            slow=False,
            count_step=1000,
            disk="data" if dimension == "disk" else "root",
        )
        if dimension == "operation":
            for item in fast:
                item["series"] = item["series"].replace(
                    'operation="write"', 'operation="read"'
                )
        inputs.extend(fast)
        inputs = list({item["series"]: item for item in inputs}.values())
        cases.append(
            dict(
                name="QMP node preserves combined " + dimension,
                interval="1m",
                input_series=inputs,
                alert_rule_test=[
                    dict(
                        eval_time="40m",
                        alertname="NodeVMStorageLatencyWidespread",
                        exp_alerts=[],
                    )
                ],
                promql_expr_test=[
                    dict(
                        expr="vmi:kme_node_io_latency_active:bool",
                        eval_time="20m",
                        exp_samples=[
                            dict(
                                labels='vmi:kme_node_io_latency_active:bool{namespace="ns",name="vm",node="node-a"}',
                                value=1,
                            )
                        ],
                    )
                ],
            )
        )

    # One flush every five seconds is active over the 10-minute P99 window.
    cases.append(
        dict(
            name="low-rate flush population",
            interval="1m",
            input_series=[
                item
                for index in range(3)
                for item in workload("flush", "vm-%d" % index, count_step=12, tail=2)
            ],
            alert_rule_test=[
                dict(
                    eval_time="40m",
                    alertname="StorageClassVMIFlushLatencyHigh",
                    exp_alerts=[summary("flush", 3)],
                )
            ],
        )
    )

    # VMI metadata can briefly disappear while latency series remain present.
    # Neither individual alert identity nor the storage-class pending timer
    # should reset during a short gap or when the metadata returns.
    for kind in ("block", "nfs"):
        inputs = [
            item
            for index in range(3)
            for item in workload(kind, "vm-%d" % index)
        ]
        for item in inputs:
            if item["series"].startswith("kubevirt_vmi_info"):
                item["values"] = " ".join(
                    ["1"] * 22 + ["stale"] + ["_"] * 3 + ["1"] * 34
                )
        alertname, title, description = individual[kind]
        workload_alerts = []
        for index in range(3):
            suffix = "-%d" % index
            labels = individual_labels(kind, claim="pvc-vm" + suffix)
            labels["pod"] += suffix
            workload_alerts.append(
                dict(
                    exp_labels=labels,
                    exp_annotations=dict(
                        summary=title.replace("ns/ ", "ns/pvc-vm%s " % suffix).replace(
                            "virt-launcher-vm", "virt-launcher-vm" + suffix
                        ),
                        description=description.replace(
                            "ns/ ", "ns/pvc-vm%s " % suffix
                        ).replace("virt-launcher-vm", "virt-launcher-vm" + suffix),
                    ),
                )
            )
        cases.append(
            dict(
                name=kind + " VMI metadata gap and recovery",
                interval="1m",
                input_series=inputs,
                alert_rule_test=[
                    entry
                    for minute in (24, 28)
                    for entry in (
                        dict(
                            eval_time="%dm" % minute,
                            alertname=alertname,
                            exp_alerts=workload_alerts,
                        ),
                        dict(
                            eval_time="%dm" % minute,
                            alertname=signals[kind][0],
                            exp_alerts=[summary(kind, 3)],
                        ),
                    )
                ],
            )
        )

    # A stopped VMI must not keep its old pod-to-VMI mapping indefinitely.
    inputs = workload("block", "vm")
    for item in inputs:
        if item["series"].startswith("kubevirt_vmi_info"):
            item["values"] = " ".join(["1"] * 11 + ["stale"] + ["_"] * 48)
    _, title, description = individual["block"]
    cases.append(
        dict(
            name="VMI metadata mapping expires after five minutes",
            interval="1m",
            input_series=inputs,
            promql_expr_test=[
                dict(expr="pod:kme_vmi:info", eval_time="18m", exp_samples=[]),
                dict(
                    expr="vmi:kme_block_io_latency_affected:bool",
                    eval_time="18m",
                    exp_samples=[],
                ),
            ],
            alert_rule_test=[
                dict(
                    eval_time="40m",
                    alertname="PVCBlockLatencyHigh",
                    exp_alerts=[
                        dict(
                            exp_labels=individual_labels("block", claim="pvc-vm"),
                            exp_annotations=dict(
                                summary=title.replace("ns/ ", "ns/pvc-vm "),
                                description=description.replace("ns/ ", "ns/pvc-vm "),
                            ),
                        )
                    ],
                )
            ],
        )
    )
    return cases


if __name__ == "__main__":
    main()
