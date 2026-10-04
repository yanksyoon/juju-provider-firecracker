# Firecracker process migration plan: firecracker-go-sdk

Status: implemented in `internal/firecracker/manager.go`; privileged equivalence gates remain open.

## Decision and pinned dependency

Use `github.com/firecracker-microvm/firecracker-go-sdk v1.0.0`.

Evidence checked on 2026-10-04:

- `go list -m -versions github.com/firecracker-microvm/firecracker-go-sdk` returned tags through `v1.0.0`; no later tagged version was available from the configured Go proxy.
- `go mod download -json ...@v1.0.0` resolved tag `v1.0.0` to commit `1f800728632d4e45a384080a51676c5a43665ffd`, module checksum `h1:HTnxnX9pvQkQOHjv+TppzUyi2BNFL/7aegSlqIK/usY=`.
- The SDK's `go.mod` declares Go 1.11, so it is compatible with this repository's `go 1.26.6` declaration. Its CNI dependency is older (`v1.0.1`) than this repository's direct `v1.2.3`; resolve and test the resulting module graph before merge.

Pin the version in the repository's `go.mod` and retain the corresponding `go.sum` entries. Do not use an untagged commit for this migration.

Primary source for the API claims below: the downloaded module at the pinned commit, especially `machine.go`, `machineiface.go`, `jailer.go`, `network.go`, `drives.go`, and `opts.go`. The public source is https://github.com/firecracker-microvm/firecracker-go-sdk/tree/v1.0.0 and the package reference is https://pkg.go.dev/github.com/firecracker-microvm/firecracker-go-sdk@v1.0.0.

## Current boundary and responsibilities

Current production code is `internal/firecracker/manager.go` and `internal/provider/provider.go`:

- `FirecrackerManager.StartVM(id, socketPath, configPath)` creates `<cgroupBase>/<id>`, calls `exec.Command(command, "--api-sock", socketPath, "--config-file", configPath)`, sets `SysProcAttr.Pdeathsig = SIGTERM`, starts the process, writes its PID to `cgroup.procs`, and tracks it.
- `StopVM` sends SIGTERM, waits up to the configured five seconds, sends SIGKILL on timeout, removes the cgroup, and is idempotent.
- The provider currently calls CNI, registers a payload in the separate HTTP metadata server, writes a small JSON file containing kernel, rootfs, interface, and IP fields, then passes config/socket paths to the manager. Cleanup rolls CNI back on failed start and stops the VM before CNI teardown.
- The provider-facing `VMManager` interface is intentionally small: `StartVM(string,string,string) error`, `StopVM(string) error`, and `ListVMs() []string` (`internal/provider/provider.go:36-41`). Existing fakes and Juju orchestration should remain behind this interface.
- `go.mod` pins Go 1.26.6 and Juju as `github.com/juju/juju v0.0.0-20260930100434-f5b474c76eba`; the current Juju-facing implementation is otherwise independent of the Firecracker process implementation.

The current JSON file is not a Firecracker API config schema. It is a repository-local placeholder. It must not be translated field-for-field into an SDK object without validation: SDK `Config` requires a kernel path, root drive, machine vCPU/memory, socket path, and SDK network model.

## Target compatibility boundary

Keep `FirecrackerManager`, `VMManager`, `FirecrackerProvider`, CNI manager, and metadata server behavior stable. Change only the manager implementation and the provider-to-manager construction/configuration seam:

```text
provider StartInstance
  -> network.SetupNetwork
  -> metadata.RegisterPayload (retain during this migration)
  -> firecracker manager StartVM(VMRequest)
       -> sdk.NewMachine(ctx, firecracker.Config, ...)
       -> machine.Start(ctx)
       -> machine.Wait(ctx) in an owned goroutine
  -> publish provider instance
```

The implementation should introduce an internal request rather than preserve three path strings:

```go
type VMRequest struct {
    ID            string
    SocketPath    string
    KernelPath    string
    RootFSPath    string
    TapName       string
    MACAddress    string // required if static SDK network config is used
    IP            net.IPNet
    VCPU          int64
    MemoryMiB     int64
    KernelArgs    string
}
```

The public/provider compatibility adapter can temporarily accept the existing `StartVM(id, socketPath, configPath)` signature, but it must parse/validate the repository config at the boundary and construct a typed `VMRequest`. The preferred follow-up is to change the internal `VMManager` interface to `StartVM(context.Context, VMRequest) error`; update fakes in the same change so no production caller retains config-file semantics.

No production code outside the SDK adapter may call `exec.Command`, construct `--api-sock`, or construct `--config-file`. The SDK itself internally owns process construction through `NewMachine` and its command builder; that is the intended exception and is why the boundary is needed.

## SDK mapping, verified APIs, and gaps

| Responsibility | Existing behavior | SDK v1.0.0 mapping | Plan / constraint |
| --- | --- | --- | --- |
| Socket creation | Provider creates a socket path and CLI receives `--api-sock`. | `firecracker.Config.SocketPath`; `NewMachine(ctx, cfg, opts...)` creates the client and SDK command. `Config` validation rejects an existing socket. | Keep per-VM socket paths, remove CLI flags and JSON socket construction from production manager. Remove stale socket during SDK cleanup and verify absence. |
| Kernel | Config JSON `kernel_image_path`. | `Config.KernelImagePath`; SDK validation stats the path. | Pass the configured absolute kernel path; fail before process start if missing. |
| Rootfs | Config JSON `rootfs`. | `Config.Drives []client/models.Drive`; `NewDrivesBuilder(rootPath)` exists in `drives.go`. | Build one root drive with SDK `DrivesBuilder`; preserve read-only policy explicitly. Do not pass rootfs as an unknown JSON field. |
| Machine sizing | Not currently represented in the config JSON. | `Config.MachineCfg models.MachineConfiguration`, with `VcpuCount` and `MemSizeMib` validated as nonzero. | Add explicit provider config/defaults or reject missing values. Do not invent defaults in the adapter. |
| Kernel args | Not currently represented. | `Config.KernelArgs string`; SDK adds static-IP boot parameters when its static network configuration is used. | Preserve any existing kernel args and define the source of `ip=`. Reject conflicting duplicate `ip=` values as SDK validation does. |
| Network interface | CNI returns host interface name and IP; provider writes both to JSON. | `Config.NetworkInterfaces []firecracker.NetworkInterface`, with `StaticNetworkConfiguration{HostDevName, MacAddress, IPConfiguration}`; or SDK `CNIConfiguration` and its `tc-redirect-tap`-style result parser. | First migration should keep the repository CNI manager and use a verified static interface mapping. Do not invoke SDK CNI as a second allocator. A MAC address is required by SDK static validation, so extend the CNI result seam to return/derive one, or stop at this prerequisite. The current CNI contract does not return a MAC. |
| Metadata/MMDS | Separate HTTP metadata server registers shell payload; current Firecracker JSON does not configure MMDS. | `Config.MmdsAddress`, `Config.MmdsVersion`; `MachineIface.SetMetadata(ctx, interface{}) error`; `Machine.SetMetadata` calls SDK `PutMmds`; `SetMmdsConfig` configures allowed interfaces. | Do not silently replace the HTTP metadata service. Decide whether guest bootstrap will move to MMDS in a separate change. If it does, call `SetMetadata` only after `Start` has initialized the API socket, and explicitly configure allowed interfaces. |
| Process ownership | `exec.Cmd` owned by manager; parent death SIGTERM. | `Machine` owns an `*exec.Cmd`; `WithProcessRunner` is a test/jailer override. SDK `Start` starts and waits asynchronously, and `Wait(ctx)` reports process exit. | Use SDK `Machine` as the owner. `WithProcessRunner` is not a production workaround because it requires constructing an `exec.Cmd`; use normal `NewMachine` or SDK `JailerCfg`. Verify parent-death behavior on the pinned source/host; no public SDK `Pdeathsig` contract was found. Do not claim equivalence until the smoke test demonstrates it. |
| Cgroup placement | Manager writes PID to arbitrary `<cgroupBase>/<id>/cgroup.procs`. | `JailerConfig` exposes `CgroupVersion`, but the verified public config has no arbitrary cgroup base/path field. Jailer is configured through `JailerCfg`, and SDK's jailer command builder owns the jailer invocation. | This is an API gap. A direct arbitrary cgroup-base guarantee cannot be preserved with only the verified SDK public API. Preferred compatibility decision: migrate to SDK jailer-managed cgroups and document/configure the jailer base, then verify actual cgroup placement. If the exact existing path is mandatory, the migration cannot meet the no-`exec.Command` requirement without an SDK change or upstream feature. |
| Parent-death | `SysProcAttr.Pdeathsig=SIGTERM`. | No verified public SDK option or documented guarantee equivalent to the current setting. | Treat as a release blocker for production equivalence. Add a process-parent death smoke test; if it fails, either contribute/use a pinned SDK extension or retain the old manager behind an explicit rollback build/configuration. |
| Start readiness | Manager returns immediately after process start and PID placement; no API readiness probe. | `Machine.Start(ctx)` runs handlers, starts VMM, waits for the API socket, and returns an error if socket initialization fails. | Use `Start` as the readiness boundary. Store the machine only after `Start` succeeds. Start a `Wait` goroutine and surface unexpected exit to manager state. |
| Stop escalation | SIGTERM, five-second wait, SIGKILL, cgroup removal. | `Machine.Shutdown(ctx)` sends guest Ctrl-Alt-Del; `StopVMM()` stops the VMM; `Wait(ctx)` waits for exit; SDK cleanup removes its socket/network cleanup state. | Preserve bounded stop semantics in the adapter: attempt `Shutdown` with `ShutdownTimeout`, then `StopVMM`, then bounded `Wait`; make repeated stop safe at manager level. Do not assume `Shutdown` is equivalent to SIGTERM. Verify all calls and errors against the machine state. |
| Error handling | Failed start kills process and removes cgroup; stop returns cleanup errors. | SDK `Start` performs handler cleanup on failure; `Wait` returns fatal process/cleanup error; `NewMachine` and config validation return pre-start errors. | Wrap errors with VM ID and phase. On failed start, call bounded SDK stop/cleanup and remove manager state. Preserve first operational error while attempting network/file cleanup. |
| Cleanup | Remove cgroup, CNI attachment, config/socket artifacts. | SDK owns socket and internal cleanup funcs; repository owns CNI allocation, metadata registration, config directory, and manager map. | Define one idempotent cleanup path: stop machine, wait/collect result, delete metadata, CNI DEL, remove socket/config only if provider owns them, remove jailer/cgroup state, then forget machine. Verify no named resource remains. |

### Explicit unverified items

These must be resolved by source inspection against the exact pinned SDK and a privileged test before implementation is called complete:

1. Whether the SDK's normal process path provides the current parent-death signal semantics. No public `Pdeathsig` API was found in the inspected v1.0.0 source.
2. Whether SDK jailer-created cgroups can be placed under the configured existing `CgroupBase` with the required path/name and permissions. `JailerConfig.CgroupVersion` is verified; an arbitrary base-path field is not.
3. How to obtain a MAC address from the existing CNI plugin result. The repository's `SetupNetwork` returns only IP and interface name, while SDK static network validation requires a MAC.
4. Exact Firecracker/Juju guest bootstrap semantics for moving the current HTTP metadata payload to MMDS. The current provider has no SDK MMDS call and no guest probe proving the HTTP payload is consumed.

Do not implement any of these by guessing symbol names or relying on an SDK version other than v1.0.0.

## Dependency and implementation sequence

1. Add and pin the SDK; run `go mod tidy`, `go test ./...`, and inspect the final module graph for CNI/API conflicts.
2. Add typed `VMRequest`, machine factory, and a `MachineIface`-backed adapter. Keep `VMManager` fakeable; do not expose SDK types to `internal/provider`.
3. Replace provider's config-file construction with typed request construction. Add explicit vCPU/memory/MAC/kernel-args configuration and validation. Preserve CNI and HTTP metadata as separate existing components.
4. Implement manager state transitions (`starting`, `running`, `stopping`, `exited`) around `NewMachine`, `Start`, `Wait`, `Shutdown`, and `StopVMM`. Ensure concurrent start/stop and duplicate IDs retain current guarantees.
5. Implement cgroup strategy. Prefer SDK Jailer and prove placement. If exact cgroup placement or parent-death behavior is unavailable, stop implementation at the documented gap rather than weakening isolation silently.
6. Add unit/fake coverage, then run static/race checks. Only after those pass run the gated Firecracker/KVM smoke suite.
7. Remove the old config JSON writer, `execProcess`, `Process` production seam, and direct CLI flags only after the adapter and rollback path are verified. Update `PROJECT.md`, `README.md`, and acceptance audit wording that currently describes direct CLI management.

## Tests and acceptance checks

Unit and ordinary CI (no KVM, Firecracker, CNI, or privileges):

- SDK-backed factory tests with a fake `MachineIface`: exact `Config` mapping for kernel, root drive, vCPU/memory, socket, network, and kernel args.
- Manager tests for duplicate/concurrent starts, start failure cleanup, readiness failure, unexpected `Wait` exit, graceful stop, timeout escalation, idempotent stop, and first-error aggregation.
- Provider tests proving CNI setup, metadata registration, VM start, rollback order, and stop/network cleanup order remain unchanged.
- Static check that production repository code contains no `exec.Command`, `--api-sock`, or `--config-file` construction. SDK module source is excluded from this check.
- `gofmt`, `go vet ./...`, `go test ./...`, `go test -race ./...`, `go build ./cmd/juju-firecracker`, and `git diff --check` pass.

Prerequisite-gated privileged smoke tests (must skip before mutation when a prerequisite is absent):

- Firecracker binary and version match the supported API; `/dev/kvm` is accessible; disposable uncompressed ELF kernel and rootfs exist; writable cgroup v2/jailer base exists; CNI plugins/config are disposable; required permissions are available.
- Start one VM through the SDK adapter and verify SDK `Start` returns only after the API socket is usable, the guest reaches the expected network/metadata probe, and the machine is tracked.
- Verify actual process ancestry/ownership, jailer/cgroup path and membership, socket ownership, tap/CNI allocation, and no duplicate network allocation.
- Kill or terminate the provider parent and verify the VM exits according to the chosen parent-death contract; record failure as a release blocker, not a pass.
- Exercise stop escalation by making graceful shutdown exceed the timeout, then verify VMM exit and cgroup/socket/CNI/config cleanup.
- Run concurrent stop/retry and provider destroy; verify no Firecracker processes, sockets, tap devices, CNI IPAM allocations, cgroups, or metadata registrations remain.

Acceptance is not met by ordinary unit tests alone. The privileged suite may be an opt-in self-hosted job, but its skip reason and prerequisites must be explicit and its cleanup audit must pass.

## Risks and mitigations

| Risk | Impact | Mitigation / release gate |
| --- | --- | --- |
| SDK's cgroup model cannot preserve the configured arbitrary base | Isolation regression or leaked resources | Use Jailer only after path/membership smoke proof; otherwise block migration or keep rollback manager. |
| Parent-death semantics differ | Orphaned VMs after provider crash | Dedicated parent-death smoke test; no equivalence claim without evidence. |
| SDK network CNI path conflicts with repository CNI | Duplicate TAP/IPAM state or teardown races | Use one network owner. Prefer static SDK mapping from a single repository CNI allocation; resolve MAC prerequisite first. |
| SDK's `Shutdown` is not SIGTERM-equivalent | Stop hangs or guest corruption | Bounded Shutdown then StopVMM escalation and explicit state tests. |
| SDK validation rejects existing assets or socket | Start failures after partial CNI allocation | Validate all paths and machine config before CNI registration where possible; rollback every partial step. |
| SDK dependency graph changes CNI/Juju behavior | Build or runtime regression | Pin v1.0.0, inspect `go mod graph`, run all ordinary/race tests and CNI tests. |
| MMDS migration changes guest bootstrap contract | Juju agent cannot bootstrap | Keep HTTP metadata for this migration; make MMDS a separately tested follow-up. |

## Rollback and non-goals

Rollback is a clean revert of the SDK adapter and dependency, restoring the existing `FirecrackerManager` behind the unchanged provider interface. Keep the old implementation isolated until privileged evidence proves cgroup and parent-death equivalence; do not delete it in the first dependency/adapter commit.

Non-goals for this migration:

- No change to Juju `environs.Environ` or `InstanceBroker` behavior.
- No change to CNI allocation policy, bridge/IPAM ownership, or the HTTP metadata protocol.
- No MMDS redesign, guest image rebuild, or kernel command-line redesign beyond fields required by SDK validation.
- No snapshot, balloon, vsock, rate-limit, metrics, or logging feature adoption unless required by an acceptance failure.
- No arbitrary external process runner, custom `exec.Cmd`, or hand-built Firecracker CLI flags in production code.
- No claim that SDK Jailer preserves the old cgroup path or parent-death signal until the explicit smoke checks pass.

## Definition of done

The migration is ready only when the typed adapter preserves provider lifecycle tests, ordinary/race/build checks pass, production code no longer constructs Firecracker CLI invocation, and the prerequisite-gated smoke evidence verifies readiness, ownership, cgroup placement, parent-death behavior, stop escalation, and complete cleanup. If any of the four explicit unverified items remains unresolved, record it as a release-blocking gap and do not remove the rollback manager.
