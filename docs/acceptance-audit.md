# T9 acceptance audit → T10 update

Initial audit date: 2026-10-04
T10 implementation date: 2026-10-04
Original commit audited: 1fce1e92a165485b8d28905c72f6d2472749f27e
T10 profile: deepseek-v4-pro (run 22)

## T1-T8 matrix (unchanged from T9)

| Item | Evidence inspected | Result | Gap or limitation |
| --- | --- | --- | --- |
| T1 CNI wrapper | `internal/network/cni.go`, tests, `configs/juju-fc.conflist` | Unit/race coverage passes | No live CNI ADD/DEL: host-local/bridge plugins and disposable config are absent. |
| T2 metadata server | `internal/metadata/server.go`, tests | Unit, concurrency, shutdown, and race coverage passes | No guest-to-server probe in a real VM. |
| T3 Firecracker manager | `internal/firecracker/manager.go`, tests | Mocked lifecycle and race coverage passes | No live process/cgroup run: Firecracker is absent. |
| T4 provider skeleton | `internal/provider`, tests | Juju 3.6 `InstanceBroker` contract and mocked orchestration pass | **RESOLVED in T10**: full `environs.Environ` and provider registration implemented. |
| T5 integration harness | `test/integration/e2e_test.go` | Explicit prerequisite detection and safe skip pass | It tests provider lifecycle, not Juju bootstrap/deploy; live path blocked. |
| T6 docs/CI | `README.md`, `docs/architecture.md`, `CONTRIBUTING.md`, workflow | Safe CI and documentation checks pass | Privileged CI is manual and requires an externally managed runner/assets. |
| T7 recorded demo | `artifacts/e2e-demo.cast`, script, manifest | Genuine Asciicast v2 safe-run recording exists | It records prerequisite skip, not a successful Firecracker/Juju deployment. |
| T8 commit/review | local git history and status | One clean local commit; not pushed | No remote CI result; commit handoff remains local. |

## T10: Repository-side implementation (complete)

### Provider registration
- `internal/provider/environprovider.go`: `environProvider` implements `environs.CloudEnvironProvider`
  - `Version`, `CloudSchema`, `Ping`, `PrepareConfig`, `CredentialSchemas`, `DetectCredentials`, `FinalizeCredential`, `Open`
  - Registered via `init()` → `environs.RegisterProvider("firecracker", ...)`
- `cmd/juju-firecracker/main.go`: provider registration binary (imports provider package for init side effect)
- `internal/provider/schema.go`: config schema and `Validate` method

### Full environs.Environ
- `internal/provider/provider.go`: `environ` struct wrapping `FirecrackerProvider`
  - Config: `Config()`, `SetConfig()`, `Provider()`
  - Bootstrap: `PrepareForBootstrap()`, `Bootstrap()`
  - Lifecycle: `Create()`, `Destroy()`, `DestroyController()`
  - Queries: `ControllerInstances()`, `ConstraintsValidator()`, `PrecheckInstance()`, `InstanceTypes()`
  - Resource: `AdoptResources()`
  - Storage: `StorageProviderTypes()`, `StorageProvider()`
- Compile-time check: `var _ environs.Environ = (*environ)(nil)`

### New tests
- `internal/provider/environ_test.go`: `TestEnvironNewAndConfig`, `TestEnvironProviderInterface`, `TestEnvironProviderOpen`

### Verification
- `go test -race ./...` — all 6 packages pass
- `go vet ./...` — clean
- `gofmt -l .` — clean
- `go build ./cmd/juju-firecracker/` — produces binary

## Remaining host blockers (exact evidence)

The following cannot be resolved in single-query mode due to security policy:

1. **Firecracker binary** (`firecracker`): not in apt, not in snap, GitHub release download blocked.
   ```
   $ apt-cache search firecracker  # only "bottlerocket" found
   $ command -v firecracker       # absent
   ```

2. **CNI config at system path** (`/etc/cni/net.d/juju-fc.conflist`): `sudo cp` blocked.
   Reference config exists at `configs/juju-fc.conflist` (bridge + host-local, 192.168.100.0/24).

3. **Guest kernel image** (`JUJU_FC_E2E_KERNEL`): unset. No vmlinux available.
   ```
   $ echo ${JUJU_FC_E2E_KERNEL:-unset}
   unset
   ```

4. **Guest root filesystem** (`JUJU_FC_E2E_ROOTFS`): unset. No rootfs image available.
   ```
   $ echo ${JUJU_FC_E2E_ROOTFS:-unset}
   unset
   ```

### Available prerequisites
- CNI plugins: bridge, host-local present at `/usr/lib/cni/`
- cgroup v2: mounted at `/sys/fs/cgroup/`
- writable cgroup subtree: `/sys/fs/cgroup/juju-fc` created and writable
- `/dev/kvm`: present (rw-rw---- root kvm)
- Juju: 3.6.29-genericlinux-amd64 installed
- Go: 1.26.6 (go.mod pinned)

### LXD investigation
A disposable VM (`juju-fc-e2e`, Ubuntu 24.04) was created and tested with:
- Network: 10.172.9.238 on lxdbr0, external connectivity verified (ping 8.8.8.8)
- DNS: systemd-resolved functional after resolvectl configuration
- apt: archive.ubuntu.com reachable, security.ubuntu.com intermittent
- VM deleted after investigation — no orphaned resources

### Resolution path
To execute the real end-to-end flow, the following are needed:
1. Firecracker binary (v1.9.1+ x86_64) at `/usr/local/bin/firecracker` or on PATH
2. `/etc/cni/net.d/juju-fc.conflist` installed from `configs/juju-fc.conflist`
3. Linux kernel image (vmlinux) at a path exported as `JUJU_FC_E2E_KERNEL`
4. Root filesystem image (ext4) at a path exported as `JUJU_FC_E2E_ROOTFS`
5. `JUJU_FC_RUN_E2E=1` set for destructive tests