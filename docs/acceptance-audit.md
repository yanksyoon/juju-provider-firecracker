# T9 acceptance audit

Audit date: 2026-10-04
Repository commit audited: 1fce1e92a165485b8d28905c72f6d2472749f27e
Requested reviewer: GPT Terra; substitution: default Hermes profile (GPT Terra is not installed in this session).

## T1-T8 matrix

| Item | Evidence inspected | Result | Gap or limitation |
| --- | --- | --- | --- |
| T1 CNI wrapper | `internal/network/cni.go`, tests, `configs/juju-fc.conflist` | Unit/race coverage passes | No live CNI ADD/DEL: host-local/bridge plugins and disposable config are absent. |
| T2 metadata server | `internal/metadata/server.go`, tests | Unit, concurrency, shutdown, and race coverage passes | No guest-to-server probe in a real VM. |
| T3 Firecracker manager | `internal/firecracker/manager.go`, tests | Mocked lifecycle and race coverage passes | No live process/cgroup run: Firecracker is absent and cgroup subtree is not writable. |
| T4 provider skeleton | `internal/provider`, tests | Juju 3.6 `InstanceBroker` contract and mocked orchestration pass | Not a complete `environs.Environ`; no provider registration, bootstrap, storage/image APIs, or real controller path. |
| T5 integration harness | `test/integration/e2e_test.go` | Explicit prerequisite detection and safe skip pass | It tests provider lifecycle, not Juju bootstrap/deploy; live path blocked. |
| T6 docs/CI | `README.md`, `docs/architecture.md`, `CONTRIBUTING.md`, workflow | Safe CI and documentation checks pass | Privileged CI is manual and requires an externally managed runner/assets. |
| T7 recorded demo | `artifacts/e2e-demo.cast`, script, manifest | Genuine Asciicast v2 safe-run recording exists | It records prerequisite skip, not a successful Firecracker/Juju deployment. |
| T8 commit/review | local git history and status | One clean local commit; not pushed | No remote CI result; commit handoff remains local. |

## Real-world gate and exact blockers

The requested Juju bootstrap, representative workload deployment, reachability
check, and teardown were not run. The repository currently has no registered
Juju provider executable or complete `environs.Environ`, so `juju bootstrap
firecracker ...` cannot be executed against this checkout even though Juju
3.6.29 is installed. The host also lacks the Firecracker binary, disposable
CNI config, bridge/host-local plugins, writable `/sys/fs/cgroup/juju-fc`, and
the `JUJU_FC_E2E_KERNEL` and `JUJU_FC_E2E_ROOTFS` assets.

LXD was investigated as the requested disposable boundary. Existing instances
were left untouched: `hermes-core` (VM), `pfe-self-hosted-runner-bot-token-test`
(VM), and `juju-3fcc70-0` (container). The available managed network is
`lxdbr0` (`10.172.9.1/24`), and the default storage pool is a shared `dir`
pool. No new VM was created because it would not supply the missing
Firecracker/provider-registration prerequisites and would add unrelated state.
No production model or credential was used.

## Verification evidence

Safe checks run: `go test ./...`, `go test -race ./...`, `go vet ./...`,
formatting, integration prerequisite/cleanup tests, and the explicitly gated
`TestEndToEnd` (expected skip). Post-run checks found no Firecracker process,
`juju-fc` cgroup, or matching TAP interface. The genuine recording and its
hash are listed in `artifacts/e2e-demo.manifest`.

The next implementation item is provider registration plus the missing Juju
`Environ` capabilities. Only after that item and disposable guest assets exist
can the bootstrap/deploy commands in `README.md` be promoted from acceptance
contract to an executed acceptance result.