# Juju Firecracker Provider

This repository contains a Go implementation scaffold for a Juju provider backed by Firecracker microVMs. It combines CNI networking, an HTTP metadata service, and cgroup-based process isolation. Scope and acceptance criteria are tracked in [PROJECT.md](PROJECT.md).

## Architecture

```text
Juju controller
      |
      v
Firecracker provider
  |       |        |
  v       v        v
 CNI   metadata   cgroup v2
  |       |        |
  +-------+--------+--> Firecracker VM
```

The detailed design, lifecycle, and privileged-host requirements are in [docs/architecture.md](docs/architecture.md). Keep design changes there rather than duplicating protocol details in this file.

## Prerequisites for the planned implementation

These are host prerequisites for running Firecracker, not requirements for reading or editing this repository:

- Linux with `/dev/kvm` and permission to use it.
- cgroup v2 mounted and writable by the provider process.
- A working CNI installation and a configuration at `/etc/cni/net.d/juju-fc.conflist`.
- CNI plugins installed in `/opt/cni/bin` (or the configured CNI binary directory).
- A Firecracker binary on `PATH`.
- Go 1.26.6 or a compatible newer Go toolchain, as declared by `go.mod`.
- The pinned Juju API dependency downloaded by Go; a separate Juju controller is not needed for unit tests.

Do not grant these privileges to ordinary CI jobs. See [CONTRIBUTING.md](CONTRIBUTING.md) for the local test split.

## Build and test

To inspect the specification:

```bash
go test ./...
go vet ./...
gofmt -l .                 # empty output is required
git diff --check
```

The package code is under `internal/`; the provider registration binary is at `cmd/juju-firecracker`. `test/integration` contains both safe prerequisite-discovery tests and an explicitly gated destructive path. On an ordinary workstation, run the package tests; the end-to-end path additionally needs the privileged prerequisites above and disposable guest assets.

Build the provider registration binary:

```bash
go build -o juju-firecracker ./cmd/juju-firecracker
# Registration happens via init(); the binary blocks for Juju to call it.
# Install: cp juju-firecracker /usr/local/bin/
```

## Disposable Firecracker integration

`configs/juju-fc.conflist` is a reference configuration, not an instruction to modify a host. A deliberately isolated integration host must provide its own disposable copy, kernel, rootfs, CNI plugins, bridge, and writable cgroup subtree. Set `JUJU_FC_E2E_CNI_CONFIG`, `JUJU_FC_CGROUP_BASE`, `JUJU_FC_E2E_KERNEL`, `JUJU_FC_E2E_ROOTFS`, and `JUJU_FC_RUN_E2E=1` only for that host, then run:

```bash
go test -count=1 -timeout=60s -v ./test/integration -run '^TestEndToEnd$'
```

The test above exercises the provider's real CNI, metadata, Firecracker, and
cgroup lifecycle. It is not a Juju controller acceptance test. This checkout
does not yet register a provider with Juju or implement the remaining
`environs.Environ` capabilities needed by `juju bootstrap`; therefore the
bootstrap/deploy flow below is documented as the acceptance contract and must
not be reported as passing for this checkout.

## Juju bootstrap/deploy acceptance flow

Run this only in a disposable LXD VM or an equivalently isolated Linux host.
Do not use a production controller, model, credential, CNI state directory,
or guest image. The host must first satisfy the prerequisites above and have
the provider registration binary installed at `/usr/local/bin/juju-firecracker`.

```bash
# From the isolated host, after installing the provider registration package:
juju bootstrap firecracker «redacted:fc-…» --no-gui
juju add-model fc-demo
juju deploy ubuntu --channel=stable fc-ubuntu
juju status --wait 10m
juju ssh fc-ubuntu/0 -- 'cloud-init status --wait && uname -a'

# Verify the workload, then remove every disposable resource.
juju status --format=yaml > /tmp/fc-demo-status.yaml
juju destroy-application fc-ubuntu --force
juju destroy-model fc-demo --destroy-storage --force --no-wait
juju destroy-controller «redacted:fc-…» --destroy-all-models --force
```

Acceptance requires `fc-ubuntu` to reach `active/0`, the SSH/cloud-init
check to succeed through the Firecracker network, and the final commands to
leave no Juju model/controller, VM, TAP device, CNI allocation, cgroup, or
process. Provider registration and the full `environs.Environ` interface are
implemented (T10), but host prerequisites (Firecracker binary, kernel, rootfs,
CNI config at system path) are still required. The current status and exact
blockers are recorded in
[`docs/acceptance-audit.md`](docs/acceptance-audit.md); no command above was
executed or claimed as successful.

## Runtime configuration

All operator-configurable values are consolidated in `internal/provider/config.go`. Precedence: Juju model config > environment variables > defaults. Required fields are `kernel-image-path` and `rootfs-path`; all others have safe defaults.

### Model config keys (Juju `model-config`)

| Key | Default | Description |
|---|---|---|
| `kernel-image-path` | *(required)* | Guest kernel image (vmlinux) |
| `rootfs-path` | *(required)* | Guest root filesystem image |
| `firecracker-binary` | `firecracker` | Firecracker binary (absolute or PATH) |
| `cni-config-path` | `/etc/cni/net.d/juju-fc.conflist` | CNI conflist |
| `cni-bin-dirs` | `/opt/cni/bin:/usr/lib/cni:/usr/libexec/cni` | CNI plugin directories |
| `cgroup-base` | `/sys/fs/cgroup/juju-fc` | cgroup v2 base path |
| `config-dir` | `/var/lib/juju-firecracker/configs` | Per-VM config directory |
| `socket-dir` | `/var/lib/juju-firecracker/sockets` | Per-VM API socket directory |
| `metadata-listen-addr` | `127.0.0.1` | Metadata HTTP server listen address |
| `stop-timeout` | `5s` | Grace period for VM shutdown |
| `shutdown-timeout` | `5s` | Grace period for metadata server shutdown |

### Environment variable overrides

| Variable | Overrides |
|---|---|
| `JUJU_FC_FIRECRACKER_BINARY` | `firecracker-binary` |
| `JUJU_FC_KERNEL_IMAGE_PATH` | `kernel-image-path` |
| `JUJU_FC_ROOTFS_PATH` | `rootfs-path` |
| `JUJU_FC_CNI_CONFIG_PATH` | `cni-config-path` |
| `CNI_PATH` | `cni-bin-dirs` |
| `JUJU_FC_CGROUP_BASE` | `cgroup-base` |
| `JUJU_FC_CONFIG_DIR` | `config-dir` |
| `JUJU_FC_SOCKET_DIR` | `socket-dir` |
| `JUJU_FC_METADATA_LISTEN` | `metadata-listen-addr` |
| `JUJU_FC_STOP_TIMEOUT` | `stop-timeout` |
| `JUJU_FC_SHUTDOWN_TIMEOUT` | `shutdown-timeout` |

### Validation

The configuration fails closed on: missing required fields, non-canonical paths, traversal components (`..`), invalid listen addresses, non-`.conflist` CNI config paths, and non-positive timeouts.

## Development status

Tasks 1–5 in `PROJECT.md` describe the provider and integration work. Task 6 supplies this documentation and CI policy. Task 10 implemented provider registration and the full `environs.Environ` interface. Task 11 (current) inventories and exposes all runtime configuration through a single typed structure with explicit defaults, environment mapping, validation, and precedence. The CI workflow runs formatting, vet, unit tests, and race tests on a hosted runner. The privileged integration job is manual, requires a labeled self-hosted runner, and is never run by pushes or pull requests. The provider is buildable as a registration binary (`cmd/juju-firecracker`); the real end-to-end gate requires host prerequisites that are documented in `docs/acceptance-audit.md`.

## Where to change things

- Setup and repository status: this file.
- Runtime design and protocols: [docs/architecture.md](docs/architecture.md).
- Disposable Juju performance campaign and measurement matrix: [docs/performance-profiling-campaign.md](docs/performance-profiling-campaign.md).
- Local development, tests, and pull requests: [CONTRIBUTING.md](CONTRIBUTING.md).
- Scope and acceptance criteria: [PROJECT.md](PROJECT.md).
