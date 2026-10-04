# Juju deployment performance-profiling campaign

Status: planning only. This document defines a disposable, reproducible campaign; it does not authorize a run on production infrastructure and contains no measured results.

## Safety and entry gate

Run only on an isolated Linux host dedicated to this campaign. The host must have no production Juju controller, model, LXD instance, bridge, CNI allocation, credential, or guest asset in scope. Use a campaign-specific temporary directory and a campaign-specific LXD project/model/controller namespace. Never collect environment dumps, authorization headers, cookies, private URLs, kubeconfigs, cloud credentials, or raw Juju credential files. Redact tokens, query signatures, cookies, and request bodies before storing evidence.

The run is held until an operator explicitly confirms all of the following:

- the host and network are disposable and have capacity limits;
- the Firecracker binary, kernel, rootfs, CNI config/plugins, LXD, Juju, and representative charm versions are pinned and recorded by version/checksum;
- non-production credentials are available through the approved secret source and will not be written to logs or artifacts;
- packet capture and request logging are permitted for the disposable endpoints;
- the six research cards and their paired adversarial validation cards are reviewed and released together;
- cleanup ownership and an emergency teardown command are assigned.

No optimization, cache/mirror experiment, or production comparison begins from this plan alone.

## Baseline scenario

Use one fixed scenario for every phase:

- LXD project: `juju-fc-perf-t13`; VM: `fc-perf-vm-01`.
- Juju controller: `fc-perf-controller-01`; model: `fc-perf-model-01`; application: `fc-perf-app-01`.
- Provider run directory: `/var/tmp/juju-fc-perf-t13/` (or an equivalent disposable path), never a shared system path.
- One fixed VM profile, one fixed CNI configuration, one pinned kernel/rootfs pair, and one small representative charm/workload. Record exact architecture, vCPU, memory, disk, filesystem, host kernel, LXD/Juju/Firecracker versions, and asset digests.
- Run one lifecycle at a time unless the matrix explicitly says otherwise. Do not let leftover instances or caches satisfy a later run silently.

The baseline lifecycle is: host/cache preparation -> LXD VM create -> guest SSH/cloud-init readiness -> asset preparation -> CNI/network readiness -> provider/controller bootstrap -> model readiness -> workload deploy -> active/healthy and reachability -> application removal -> model/controller/VM/provider teardown -> leak checks.

## Timing and evidence contract

Use a monotonic clock for local phase boundaries and retain UTC wall time only for correlation. Each phase emits a JSONL record with: `run_id`, `phase`, `start_mono_ns`, `end_mono_ns`, `duration_ns`, result, versions/digests, cache state, and redaction status. Record command exit status and bounded stdout/stderr separately; do not use shell `time` output as the sole measurement.

Capture these evidence streams with synchronized run IDs:

1. phase JSONL and raw command exit records;
2. provider, Juju controller/agent, LXD, Firecracker, CNI, cloud-init, and workload logs;
3. request-class records containing timestamp, direction, protocol, host category, method/status, response bytes, retry/redirect/range flags, DNS/TCP/TLS/connect/request/TTFB/total timings, and artifact digest where applicable—but never secrets or full bodies;
4. packet evidence limited to disposable interfaces/endpoints, preferably summarized flow/packet counters plus a short bounded capture when approved;
5. resource samples for CPU, RSS, disk I/O/free space, network bytes, process count, TAP/bridge/CNI state, and Firecracker/Juju/LXD object inventories;
6. a cleanup manifest proving every named resource is absent after teardown.

Request classification is exclusive and stable: each request receives exactly one class and an initiator/phase. If classification is uncertain, mark it `unknown` and stop interpretation rather than assigning it by URL guesswork. DNS, TCP connect, TLS handshake, redirects, retries, and transfer time remain separate fields; parallel requests are retained individually and also reported with an explicit concurrency group.

## Measurement matrix and dependencies

| ID | Phase / request classes | Depends on | Cold run | Warm run | Required evidence / exit condition |
|---|---|---|---|---|---|
| M0 | Host inventory, isolation, empty-state and baseline leak check | operator gate | fresh campaign directory and disposable namespace | same host, no reset beyond documented cache policy | pinned versions/digests, clean inventory, capacity limits; stop if any production resource is visible |
| M1 / T18 | Kernel/rootfs/cloud-image acquisition; checksum; decompression/conversion; local staging. Classes: URL metadata, manifest/checksum, blob/range, registry/OCI, redirect, retry | M0 | clear only campaign-owned asset/cache paths | preserve campaign cache and repeat same digest | per-request timing/bytes/range/retry plus CPU/disk; integrity passes; stop on unbounded asset, checksum mismatch, or unexpected host |
| M2 / T14 | LXD VM creation and guest preparation. Classes: LXD image metadata, image layers, storage, cloud-init/agent and guest DNS/SSH | M1 | campaign cache and VM absent | recycle/reset only as specified, with cache state recorded | phase timings, LXD events, guest readiness, I/O; stop on non-campaign LXD mutation or readiness timeout |
| M3 / T20 | CNI and first network path. Classes: plugin discovery/config, bridge/IPAM, TAP, metadata, DNS, route, guest-host, guest-network | M2 | remove campaign network state before run | repeat with documented IPAM/cache state | namespace/interface/IPAM snapshots, flow counters, request timings; stop on leaked allocation, broad host-network change, or packet outside allowlist |
| M4 / T16 | Provider/controller bootstrap and model setup. Classes: controller/store discovery, auth handshake metadata, agent download, controller RPC, model creation/readiness | M3 | new disposable controller/model and cleared campaign cache | destroy/recreate model/controller under warm cache policy | Juju/provider logs, request records with auth values redacted, readiness boundary; stop on production endpoint or credential exposure |
| M5 / T22 | Charmhub metadata/artifact retrieval. Classes: store/API discovery, metadata, revision resolution, OCI/blob/charm/resource transfer, redirect, retry, auth handshake | M4 | fresh model and cleared relevant cache | repeated install with model/cache policy recorded | artifact sizes/digests, per-request timings, status transitions; stop on mutable response caching, mirror/publish action, or digest mismatch |
| M6 / T24 | Workload deploy/convergence and teardown. Classes: agent/image/package/config retrieval, unit startup, hooks, probes, workload reachability, removal, model destruction, provider teardown | M5 | fresh model/unit and campaign asset/cache state | repeat install/deploy under warm policy | status transition timestamps, logs, reachability, teardown and leak manifest; stop on unhealthy unit timeout or incomplete cleanup |
| M7 | Cross-phase reconciliation and repeatability gate | M1-M6 | at least 3 valid cold lifecycles | at least 3 valid warm lifecycles | paired raw data, invalid-run reasons, medians and spread; no conclusion from a single wall-clock sample |

Each T-number row has a research card and a directly paired adversarial card in the Kanban graph. The adversarial card must review the completed research evidence before that row is accepted. A row is not a license to run its dependent rows until its evidence and cleanup checks pass.

## Cold and warm protocol

A cold run means only campaign-owned caches are cleared according to a written allowlist; do not flush host-wide caches or alter unrelated LXD/CNI state. A warm run reuses exactly the documented campaign-owned cache state and repeats the same pinned inputs. Between runs, preserve the cache manifest, asset digests, connection-reuse policy, DNS resolver state, and VM/model reset procedure. Randomize cold/warm order when practical, record interruptions, and discard a run if any hidden warm state, retry, or concurrent workload is detected.

For M7, perform at least three valid cold and three valid warm lifecycles. Report each raw sample, median, and spread; use a larger sample set if variability prevents a stable comparison. Never call a cache effect causal without matching request evidence, cache-state proof, and an adversarial review.

## Stop conditions and cleanup

Stop immediately and preserve evidence without interpretation if: a production endpoint/resource is discovered; a secret appears; an unbounded download, unexpected redirect, retry storm, or packet destination occurs; integrity/authentication fails; instrumentation changes the path materially; a phase times out; a request cannot be classified; a non-campaign interface/IPAM allocation appears; or teardown cannot be proven complete.

On every stop or normal completion, attempt bounded teardown in dependency order: workload, model, controller, Firecracker processes, VM, TAP/network namespace, CNI allocation, bridge (only if campaign-owned), temporary files, and campaign credentials/session handles. Then verify by fresh inventory: no named Juju objects, LXD instances/projects, Firecracker processes, TAP devices, campaign bridge/IPAM allocations, cgroups, mounts, listening ports, or campaign directory handles remain. Preserve only redacted logs, timing records, digests, and the cleanup manifest; securely remove credential material and packet payloads.

## Reporting and decision rules

The report must include the exact scenario manifest, matrix row, dependency status, cache state, raw evidence locations, invalid samples and reasons, cleanup result, and threats to validity. Separate local preparation, network wait, guest boot, controller convergence, and workload convergence. Request classes must be reported individually before any aggregate. A bottleneck hypothesis is provisional until reproduced in both independent repeated runs and the paired adversarial review. Any proposed optimization is a later task with explicit integrity, authentication, invalidation, legal, and rollback checks; this campaign does not implement one.
