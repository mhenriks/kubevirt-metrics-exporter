# THP and node memory readiness

[Documentation index](../README.md#documentation)

This guide explains the **KubeVirt VM Memory Details (Dev Preview)** Perses dashboard (`deploy/openshift/dashboard-memory-detailed.yaml`) and the KME node memory metrics it uses.

## Three layers (do not mix them)

| Layer | Source | What it measures | Dashboard panel |
|-------|--------|------------------|-----------------|
| **Structural layout** | `/proc/zoneinfo` | Zone sizes (`present`) and in-zone free pages. When `ZONE_MOVABLE` is populated (`kernelcore=` layout): DMA+DMA32+Normal ≈ pinned kernel pool; Movable ≈ guest/THP pool. Without Movable, DMA+DMA32+Normal is essentially all node RAM — kernelcore used is not meaningful. | Kernelcore pool |
| **THP buddy stock** | `/proc/buddyinfo` + `/proc/pagetypeinfo` | Free buddy blocks in the **Movable zone** (else Normal): total, movable-capable, and pinned freelist. | Movable zone (stack) |
| **Node reclaim context** | `MemAvailable` (node_exporter) | Node-wide estimate including reclaimable cache — **not** per-NUMA. | Movable zone (dashed overlay) |

On split-layout hosts only, kernelcore **used** (present − free over DMA+DMA32+Normal) is mostly kernel/slab/page-table. The Kernelcore panel is gated on Movable `present > 0` so that `used` is not almost all of RAM. It stacks **Unreclaimable used** (`used − slab recl`) + **Reclaimable slab** + **Free** (`visual.stack: all`, one `sum()` per slice). Pin `$numa` to inspect one socket. Reclaimable slab is a real per-NUMA gauge everywhere; it is only a kernelcore slice when Movable holds user pages. On fallback hosts the panel is empty — use the Movable zone panel (Non-THP-movable ≈ unmovable freelist).

## Background: transparent huge pages (THP)

On typical x86-64 hosts (4 KiB base page size), **PMD-sized THP uses 2 MiB pages** (buddy **order 9**; each order *o* spans `4096 × 2^o` bytes per block). The kernel can promote anonymous and shmem mappings to THP and demote them again under pressure. Unless THP is fully disabled, the **`khugepaged`** kernel thread scans memory and attempts to **collapse** eligible small pages into huge pages. Policy (`always`, `madvise`, `never`, per-size controls on recent kernels) is described in the [Transparent Hugepage Support](https://docs.kernel.org/admin-guide/mm/transhuge.html) admin guide.

THP observability splits naturally into two questions:

1. **Guest / VM:** How much memory is **already** huge-backed?
2. **Node:** How much **free physical stock** can still support new 2 MiB allocations or khugepaged collapse on a given NUMA node?

The dashboard and KME metrics address both.

## Kernel sources

| Source | What it reports | Used for |
|--------|-----------------|----------|
| `/proc/buddyinfo` | Free buddy **blocks** per zone and NUMA node, by **order only** (not migrate type). **Exact** block counts. | `kme_node_buddy_bytes_*` |
| `/proc/pagetypeinfo` | Same free-block shape, split by **migrate type** (Movable, Unmovable, Reclaimable, Isolate, …). Counts can be **capped** (e.g. `>100000`) when the kernel avoids long zone-lock holds. | `kme_node_unmovable_bytes_*`; input to movable derivation |
| `/proc/zoneinfo` | Structural zone sizes (`present`) and free pages (`pages free`) per zone and NUMA node; per-node `nr_slab_reclaimable`. | `kme_node_zone_present_bytes`, `kme_node_zone_free_bytes`, `kme_node_slab_reclaimable_bytes` |
| cgroup v2 `memory.stat` | Per-QEMU `anon_thp`, `shmem_thp`, `file_thp`, etc. | `container_memory_*_thp_bytes` |
| `/proc/vmstat` | Node counters `thp_split_pmd`, `thp_collapse_alloc` | `kme_node_thp_*_total` |
| virt-handler metrics | libvirt `dommemstat` RSS and balloon/domain size | `kubevirt_vmi_memory_resident_bytes`, `kubevirt_vmi_memory_domain_bytes` |

**Buddyinfo vs pagetypeinfo:** Buddyinfo answers “how much free memory exists at each block size?” Pagetypeinfo answers the same question but splits counts by **migrate type** — the kernel’s label for whether free pages in a page block can be relocated during compaction, migration, or hugepage grouping:

| Migrate type | Typical meaning |
|--------------|-----------------|
| **Movable** | Can be migrated/compacted; preferred for user allocations and THP grouping. |
| **Unmovable** | Pinned on the freelist (kernel structures, some DMA-related stock, etc.); not available for collapse as movable stock. |
| **Reclaimable** | File-backed pages on the freelist that can be reclaimed (metadata/cache) before reuse. |
| **Isolate** | Isolated blocks (debug/testing); treated like unmovable for THP readiness math in KME. |
| **Reserve** | Reserved for future movable use; not reported by KME. |

The kernel keeps migrate types separated within page blocks to limit fragmentation under mixed workloads.

KME reports buddy/pagetype from the **Movable zone when present** (else Normal) for THP readiness, and sums Unmovable freelist across Normal + Movable zones. Zoneinfo covers all zones (DMA, DMA32, Normal, Movable). Values are labeled by Kubernetes **node** name and **NUMA** node id.

## How KME derives buddy metrics

```
buddy (exact)          ← /proc/buddyinfo Movable zone (else Normal)
excluded               ← /proc/pagetypeinfo same THP zone (Unmovable + Isolate lines)
movable-capable        ← buddy − excluded   (per NUMA, clamped at 0)
unmovable freelist     ← pagetypeinfo Unmovable summed across Normal + Movable zones
kernelcore pool        ← zoneinfo present/free on DMA + DMA32 + Normal zones
kernelcore slab recl   ← zoneinfo per-node nr_slab_reclaimable (occupies the pool on split layout)
```

Exported gauges:

| Metric | Meaning |
|--------|---------|
| `kme_node_buddy_bytes_order_ge_9` / `_all_orders` | Total free buddy memory in the THP zone (all migrate types). |
| `kme_node_movable_bytes_order_ge_9` / `_all_orders` | **Movable-capable** free buddy in the THP zone: buddy minus Unmovable and Isolate freelist pages. |
| `kme_node_unmovable_bytes_order_ge_9` / `_all_orders` | **Unmovable** migrate-type freelist only (Normal + Movable zones). Isolate is subtracted for movable-capable math but not included here. |
| `kme_node_zone_present_bytes` / `_free_bytes` | Structural zone size and free pages from zoneinfo (`present` / `pages free`). |
| `kme_node_slab_reclaimable_bytes` | Per-NUMA reclaimable slab from zoneinfo `nr_slab_reclaimable`. On split layout this is the reclaimable portion of kernelcore **used** (not file cache; that lives in Movable). |

**Important:** `movable-capable` is **not** the pagetypeinfo “Movable” line. Buddy total includes **Reclaimable** (and other) freelist pages that are not Unmovable or Isolate; those remain in buddy and are counted in `movable-capable` because only Unmovable and Isolate are subtracted. In practice, `movable-capable` is often **above** the pagetype Movable line at the same NUMA node; the gap is largely **reclaimable freelist** memory (and any other migrate types except Isolate).

**Why both `order_ge_9` and `all_orders` for unmovable?** They measure different things:

- **`unmovable_bytes_order_ge_9`** — free Unmovable blocks at order ≥ 9 (2 MiB+). This is the portion of **THP-sized** free buddy that cannot be used for collapse. It is required internally to compute `movable_bytes_order_ge_9` and is useful for alerts/debugging (“how much 2 MiB-class free stock is pinned unmovable?”).
- **`unmovable_bytes_all_orders`** — all free Unmovable blocks (orders 0–10). Most unmovable free memory usually sits in **small** orders; this line shows total pinned freelist footprint but does **not** substitute for ge9 when assessing immediate THP readiness.

The **KubeVirt VM Memory Details (Dev Preview)** dashboard stacks THP buddy free as non-THP-movable + movable below 2 MiB + ≥2 MiB movable (`visual.stack: all`). It does not plot `unmovable_bytes_all_orders` or `unmovable_bytes_order_ge_9` directly.

## Dashboard panels

**Limit** (`$topk`, default 5) — caps most panels to the top *N* series by **instant value** (Prometheus `topk`). Not used on the Kernelcore pool or Movable-zone THP stacks (`sum()` of the current `$node`/`$numa` selection). Remaining multi-line panels share an anchor so related series stay aligned: metric sum per node for khugepaged/ksmd/KSM-profit and split/collapse (see table).

**VM Memory** — scoped by `node` (pick one node for per-VM detail). Cgroup THP metrics use `sum without (pod)` so `node` stays on the series for the virt-handler join. Resident and domain metrics are filtered with `kubevirt_vmi_info{phase="running"}` (includes paused VMs; excludes stopped/transitional/stale post-migration series).

| Panel | Limit sort (descending) | PromQL idea | Interpretation |
|-------|-------------------------|-------------|----------------|
| **THP / Resident** | THP / resident % | `topk(N, sum(anon_thp + shmem_thp) / resident × 100)` joined on `(namespace, name, node)`; resident filtered with `kubevirt_vmi_info{phase="running"}` | Share of **RSS** already backed by THP. Rising → more guest RAM in huge pages (successful collapse or huge-friendly allocation). Low → mostly 4 KiB pages. Denominator is libvirt RSS (`kubevirt_vmi_memory_resident_bytes`), numerator from QEMU cgroup `memory.stat` (`sum without (pod)`). |
| **Resident / Configured** | resident / configured % | `topk(N, resident / domain_bytes × 100)` with running-VMI filter on both sides | Share of **ballooned domain size** actually resident. Context for memory footprint, not THP-specific. Low ratio with a large balloon means much “configured” memory is not in RAM. |

**Node — THP readiness (Movable zone)** — stacked (`visual.stack: all`); height = buddy free total in the THP zone (Movable if present, else Normal) for the current `$node`/`$numa` selection. Pin Node/NUMA — one stack, not per-node series.

| Slice (bottom → top) | PromQL | Interpretation |
|----------------------|--------|----------------|
| **Non-THP-movable freelist** | `sum(buddy_all) − sum(movable_all)` | Unmovable + Isolate free buddy; not usable as THP stock |
| **Movable below 2 MiB** | `sum(movable_all) − sum(movable_ge9)` | Movable-capable but not yet 2 MiB blocks (compaction/collapse) |
| **≥2 MiB Movable** | `sum(movable_ge9)` | Ready THP-sized movable free stock |
| **MemAvailable** (dashed overlay) | `sum(MemAvailable)` for `$node` | Node-wide reclaim (`MemAvailable (node-wide reclaim)`) free + caches, **not** filtered by `$numa`. |

**Node — THP readiness (Kernelcore pool)** — **split-layout hosts only** (populated Movable zone). Empty on fallback hosts; do not interpret DMA+DMA32+Normal `used` there as kernel stock. Stacked (`visual.stack: all`); height = `present` for the current `$node`/`$numa` selection.

| Slice (bottom → top) | PromQL | Interpretation |
|----------------------|--------|----------------|
| **Unreclaimable used** | `sum(present) − sum(free) − sum(slab_recl)` over DMA\|DMA32\|Normal, × gate | Used minus reclaimable slab (not equal to unreclaimable slab alone) |
| **Reclaimable slab** | `sum(slab_recl)` × gate | Per-NUMA `nr_slab_reclaimable`; not Movable file cache |
| **Free** | `sum(free)` over DMA\|DMA32\|Normal × gate | Buddy free in core zones |

`zone=~"DMA|DMA32|Normal"` is required on present/free (those series are per-zone; omitting Movable is the pool). Slab has no zone label. Gate: `kernelcore_zoneinfo_gate` (`movable_present` \| `always`). To drop the gate once all nodes use split layout: set `always`, then remove ` $kernelcore_zoneinfo_gate` from the three queries.

**Node — khugepaged & ksmd CPU / KSM profit** — CPU lines and profit share the same top **nodes** (anchor: `khugepaged CPU % + ksmd CPU %` per node). Dual Y-axis: left = CPU %, right = profit bytes (dashed).

| Line | Limit sort | Axis |
|------|------------|------|
| **khugepaged CPU %** | sum anchor (per node) | left |
| **ksmd CPU %** | sum anchor (per node) | left |
| **KSM general profit** | sum anchor (per node) | right (bytes) |

`100 × rate(cpu_seconds_total[interval])` → approximate **% of one CPU core**.

- **khugepaged** — THP collapse / scanning activity. Bursts are normal when memory is being collapsed; sustained high rates under load warrant checking split vs collapse counters.
- **ksmd** — Kernel Samepage Merging (separate from THP). High ksmd CPU means active page merging; it competes for CPU but is not the same mechanism as THP.
- **KSM general profit** — `node_ksmd_general_profit_bytes` (node_exporter-aligned, with `node` label). Net memory saved after rmap_item overhead; negative → overhead exceeds savings.

**Node — THP split & collapse** — both lines share the same top **nodes** (anchor: `split_pmd/min + collapse_alloc/min` per node).

| Line | Limit sort |
|------|------------|
| **split_pmd/min** | sum anchor (per node) |
| **collapse_alloc/min** | sum anchor (per node) |

`60 × rate(vmstat_counter[interval])` → events **per minute**.

- **`thp_split_pmd`** — THPs split back to smaller pages (unmap, protection changes, pressure, fragmentation management).
- **`thp_collapse_alloc`** — Successful collapses by khugepaged.

Counters are **lifetime** totals; the dashboard shows **recent rate**. Near-zero rates mean a quiet window, not disabled THP. Rising splits with flat collapses suggest THP churn or pressure.

## Reading trends together

| Pattern | Likely meaning |
|---------|----------------|
| movable ge9 ↑ | More immediate 2 MiB-capable free stock on that NUMA node. |
| movable ge9 ↓ while VM resident ↑ | Guest RAM growing faster than 2 MiB free stock replenishes; new THP formation may slow. |
| unmovable ge9 ↑ (or large `buddy_ge9 − movable_ge9`) | More THP-sized free memory is **not** movable-capable. |
| collapse_alloc rate ↑ | khugepaged actively forming THPs. |
| split_pmd rate ↑, collapse flat | THPs being torn down faster than formed — check pressure, mapping churn, or policy. |
| VM THP/Resident ↑ | Guest using more huge-backed RAM. |
| VM THP/Resident low, movable ge9 high | THP opportunity not yet taken (policy, workload, or khugepaged not needed yet). |
| Kernelcore stack: green sliver, red filling, thin amber | Unreclaimable used filling the pool (reclaimable slab cannot cover used). Pin `$numa` — DMA32 on node 0 can hide a tight Normal zone in a combined stack. |
| Movable stack: green (ge9) shrinking, amber growing | Free movable is fragmenting below 2 MiB; compaction/khugepaged may still recover. |
| Movable stack: red (non-THP-movable) dominating | Buddy free is mostly Unmovable/Isolate; little THP stock even if height is large. |
| Kernelcore stack: amber growing, green shrinking | Slab filling the pool; reclaim can still help. |

## Caveats and limitations

- **Pagetype saturation:** Fields like `>100000` in pagetypeinfo are a **floor**, not an exact count. Affected orders in unmovable/movable derivations can be **undercounted**; buddyinfo totals at the same order remain exact. Prefer buddy ge9 for exact 2 MiB **total** free; treat pagetype-derived unmovable at saturated orders as approximate.
- **Order 0:** Pagetype per-order sums can diverge from buddyinfo at **order 0** on some kernels; orders **8–10** typically align. Rely on **ge9** lines for THP-sized conclusions.
- **Reclaimable freelist:** `movable-capable` subtracts only Unmovable and Isolate; **Reclaimable** free buddy pages remain in the residual. That makes `movable-capable` larger than the pagetype Movable line and can include pages that are not as readily usable for collapse as strictly Movable stock.
- **Isolate migrate type:** Subtracted in movable-capable math but not included in `kme_node_unmovable_bytes_*`. The Movable-zone stack bottom slice is `buddy − movable` (Unmovable + Isolate in the THP zone).
- **kernelcore hosts:** When `ZONE_MOVABLE` exists, THP buddy metrics read the **Movable** zone. Without it, they read **Normal**. Zoneinfo always reports all zones. Kernelcore pool panel slices (Unreclaimable used / Reclaimable slab / Free) are gated on Movable zone presence; do not interpret DMA+DMA32+Normal used on non-split hosts as kernel stock. Both stacked charts `sum()` the current `$node`/`$numa` selection (`visual.stack: all` is a single stack).
- **Resident vs cgroup anon:** RSS and cgroup `anon` are related but not identical; small gaps in THP/Resident are expected.
- **Scope:** Buddy/pagetype metrics describe **free** buddy pages in the THP zone (Movable if present, else Normal). Zoneinfo metrics describe **structural** zone sizes (`present`) and free pages across DMA/DMA32/Normal/Movable. Neither replaces hugetlbfs pool accounting.
- **Policy:** Metrics show stock and activity, not sysfs THP policy (`/sys/kernel/mm/transparent_hugepage/…`). Low VM THP with `never` or without `MADV_HUGEPAGE` is expected regardless of movable ge9.

## Further reading

- [Transparent Hugepage Support](https://docs.kernel.org/admin-guide/mm/transhuge.html) — THP policies, khugepaged, sysfs and boot parameters.
- [Memory management documentation index](https://docs.kernel.org/admin-guide/mm/index.html) — broader MM admin topics.
- [CRI-O cgroup memory metrics proposal](https://github.com/cri-o/cri-o/pull/10143) — alignment of `container_memory_*` naming.

See the [cgroup metric reference](metrics.md#cgroup-metrics) for types and labels, and the [dashboard catalog](dashboards.md) for the other dashboards.
