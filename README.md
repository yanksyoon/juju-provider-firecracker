# Juju Firecracker Provider

This repository contains a Go implementation scaffold for a Juju provider backed by Firecracker microVMs. It combines CNI networking, an HTTP metadata service, and cgroup-based process isolation. Scope and acceptance criteria are tracked in [PROJECT.md](PROJECT.md).

## Installation and Usage

This is a Linux-only provider scaffold. The repository is the source of truth for
configuration; the detailed design and host contract are in
[docs/architecture.md](docs/architecture.md) and the runtime configuration table
below.

### Prerequisites

- Go 1.26.6 or newer, as required by `go.mod`.
- For a real VM: Linux with accessible `/dev/kvm`, cgroup v2 with a writable
  provider subtree, a Firecracker binary on `PATH`, and CNI `bridge` and
  `host-local` plugins.
- A disposable guest kernel and rootfs image, plus a disposable CNI conflist.
  `configs/juju-fc.conflist` is a reference only; do not install it over a
  host's system configuration.

The Firecracker, KVM, CNI, cgroup, kernel, and rootfs prerequisites are normally
unavailable on ordinary workstations and hosted CI. They are not needed for
editing the repository or running its safe tests.

### Build and safe verification

From the repository root:

```bash
go build -o juju-firecracker ./cmd/juju-firecracker
sudo install -m 0755 juju-firecracker /usr/local/bin/juju-firecracker  # optional; needs privilege
go test ./...
go vet ./...
test -z "$(gofmt -l .)"
git diff --check
```

The binary registers the `firecracker` provider when Juju invokes it and must
remain available on the host where Juju discovers providers. No credentials,
cloud endpoint, or kubeconfig is required by this local provider.

### Disposable real integration test

Only run this on an isolated Linux host with disposable resources. Replace each
placeholder with an existing path on that host; do not use production CNI or
cgroup state:

```bash
export CNI_PATH=/path/to/cni/bin
export JUJU_FC_RUN_E2E=1
export JUJU_FC_E2E_CNI_CONFIG=/path/to/disposable/juju-fc.conflist
export JUJU_FC_CGROUP_BASE=/path/to/writable/cgroup-v2/juju-fc
export JUJU_FC_E2E_KERNEL=/path/to/disposable/vmlinux
export JUJU_FC_E2E_ROOTFS=/path/to/disposable/rootfs.ext4
go test -count=1 -timeout=60s -v ./test/integration -run '^TestEndToEnd$'
```

The test refuses system CNI paths and configured production Juju targets. It
cleans up its VM, CNI allocation, cgroup, sockets, and temporary files. The
ordinary `go test ./...` command does not prove a real Firecracker deployment.
The workflow's privileged job is opt-in and requires a labeled self-hosted
runner; see [CONTRIBUTING.md](CONTRIBUTING.md).

### Juju acceptance contract (not a local smoke test)

After installing the registration binary and preparing the prerequisites above,
an operator may use a disposable Juju controller/model. Set the required model
configuration with machine-specific paths, then exercise the smallest workload
flow:

```bash
export DISPOSABLE_CONTROLLER=fc-test-controller
juju bootstrap firecracker "$DISPOSABLE_CONTROLLER" --no-gui
juju add-model fc-demo
juju model-config kernel-image-path=/path/to/disposable/vmlinux
juju model-config rootfs-path=/path/to/disposable/rootfs.ext4
juju deploy ubuntu --channel=stable fc-ubuntu
juju status --wait 10m

# After verification, clean up every disposable Juju resource.
juju destroy-application fc-ubuntu --force
juju destroy-model fc-demo --destroy-storage --force --no-wait
juju destroy-controller "$DISPOSABLE_CONTROLLER" --destroy-all-models --force
```

This flow is an acceptance contract, not a claim that this checkout has passed a
controller deployment. Use only a disposable controller/model and guest image;
remove any host resources after testing. Do not put credentials or private
endpoints in this document. See [docs/acceptance-audit.md](docs/acceptance-audit.md)
for the current implementation boundary.

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

The detailed design, lifecycle, and privileged-host requirements are in [docs/architecture.md](docs/architecture.md). The bounded LXD guest boundary and its opt-in smoke path are documented in [docs/lxd-compatibility.md](docs/lxd-compatibility.md). Keep design changes there rather than duplicating protocol details in this file.

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
registers the provider and implements the `environs.Environ` surface, but the
bootstrap/deploy flow remains an acceptance contract and must not be reported
as passing without a disposable controller run.

For a side-effect-free prerequisite check and an explicitly gated LXD
provider-registration smoke test, see
[`docs/lxd-compatibility.md`](docs/lxd-compatibility.md). The smoke test does
not bootstrap or contact a Juju controller/model and must not be presented as
Firecracker workload compatibility.

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
| `metadata-listen-addr` | `127.0.0.1:8080` | Metadata HTTP server listen address |
| `cross-controller-settings` | *(optional)* | Strict secret-free JSON settings contract; see [cross-controller compatibility](docs/cross-controller-compatibility.md) |
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
- Disposable Canonical K8s cross-controller settings and gated smoke procedure: [docs/cross-controller-compatibility.md](docs/cross-controller-compatibility.md).
- Local development, tests, and pull requests: [CONTRIBUTING.md](CONTRIBUTING.md).
- Scope and acceptance criteria: [PROJECT.md](PROJECT.md).
