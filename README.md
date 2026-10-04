# Juju Firecracker Provider

This repository contains a Go implementation scaffold for a Juju provider backed by Firecracker microVMs. It combines CNI networking, an HTTP metadata service, and cgroup-based process isolation. Scope and acceptance criteria are tracked in [PROJECT.md](PROJECT.md).

## Installation and Usage

This is a Linux-only provider. The repository is the source of truth for
configuration; the detailed design and host contract are in
[docs/architecture.md](docs/architecture.md) and the runtime configuration table
below.

### Install a release (recommended)

Release binaries are published for Linux `amd64` and `arm64`. The installer
downloads only over HTTPS and verifies the SHA256 manifest before replacing the
provider registration binary. It does not require Go, Firecracker, or a clone:

```bash
curl -fsSL https://raw.githubusercontent.com/yanksyoon/juju-provider-firecracker/main/scripts/install.sh | sh
```

For reproducible installation, pin a tag and choose a user-writable directory:

```bash
curl -fsSL https://raw.githubusercontent.com/yanksyoon/juju-provider-firecracker/main/scripts/install.sh \
  | JUJU_FIRECRACKER_VERSION=v1.2.3 INSTALL_DIR="$HOME/.local/bin" sh
```

Without a version, the installer follows the GitHub Releases `latest` redirect.
Use `JUJU_FIRECRACKER_VERSION` or a positional version to avoid moving targets.
The default destination is `/usr/local/bin`; when it is not writable the
installer uses `$HOME/.local/bin`, or an explicit `INSTALL_DIR`. Existing files
are left untouched until download and checksum verification succeed. To roll
back, rerun the installer with the previous tag; to uninstall, remove the
installed `juju-firecracker` path (the installer prints it).

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

### Source build and safe verification (development fallback)

From the repository root:

```bash
go build -o juju-firecracker ./cmd/juju-firecracker
go test ./...
go vet ./...
test -z "$(gofmt -l .)"
git diff --check
```

Source builds are for development and testing; normal users should install a
versioned release instead. The binary registers the `firecracker` provider when
Juju invokes it and must remain available on the host where Juju discovers
providers. No credentials, cloud endpoint, or kubeconfig is required.

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

### Install, bootstrap Juju, and run a workload smoke test

This is the canonical operator walkthrough. Run it only on a disposable Linux
host or LXD VM. It requires Juju, KVM, cgroup v2, Firecracker, CNI plugins, a
disposable guest kernel/rootfs, and a disposable CNI configuration. The release
installer installs the provider registration binary; it does not install or
configure Firecracker, Juju, CNI, the kernel, or the rootfs.

1. Install a pinned provider release. Set `INSTALL_DIR` if `/usr/local/bin` is
   not writable:

```bash
curl -fsSL https://raw.githubusercontent.com/yanksyoon/juju-provider-firecracker/main/scripts/install.sh \
  | JUJU_FIRECRACKER_VERSION=v0.0.1 INSTALL_DIR="$HOME/.local/bin" sh
export PATH="$HOME/.local/bin:$PATH"
test -x "$(command -v juju-firecracker)"
```

2. Prepare the disposable host. Set these to real paths on the host; do not use
   production CNI state or guest images:

```bash
export CNI_PATH=/path/to/cni/bin
export JUJU_FC_CNI_CONFIG_PATH=/path/to/disposable/juju-fc.conflist
export JUJU_FC_CGROUP_BASE=/path/to/disposable/cgroup-v2/juju-fc
export KERNEL_IMAGE=/path/to/disposable/vmlinux
export ROOTFS_IMAGE=/path/to/disposable/rootfs.ext4
```

The CNI configuration must provide a Firecracker-compatible TAP attachment,
non-overlapping address space, host firewall/isolation policy, and cleanup
ownership. The provider must be able to access `/dev/kvm` and the configured
cgroup subtree.

3. Bootstrap a disposable controller and model with the matching custom Juju
   distribution. The provider must be compiled into both `juju` and `jujud`
   from a Juju fork that blank-imports this repository's public `provider`
   package. Installing `juju-firecracker` alone is insufficient: stock Juju
   returns `unknown cloud "firecracker"`, and a stock controller cannot load
   the provider from the client host. The reproducible integration contract is
   documented in `docs/juju-provider-registration-boundary-plan.md`; no custom
   Juju release is published by this repository yet.

```bash
export DISPOSABLE_CONTROLLER=fc-test-controller
# Put the matching fork's bin directory first; it must contain both juju and jujud.
export PATH=/path/to/juju-firecracker-build/dist:$PATH
juju bootstrap firecracker "$DISPOSABLE_CONTROLLER" --no-gui
juju add-model fc-demo
juju model-config kernel-image-path="$KERNEL_IMAGE"
juju model-config rootfs-path="$ROOTFS_IMAGE"
juju model-config cni-config-path="$JUJU_FC_CNI_CONFIG_PATH"
juju model-config cgroup-base="$JUJU_FC_CGROUP_BASE"
```

4. Deploy a small test workload. Juju's `ubuntu` charm is used with the
application name `busybox-smoke`; BusyBox is installed inside the disposable
unit and then executed to verify instance creation, networking, SSH, and guest
command execution:

```bash
juju deploy ubuntu busybox-smoke --channel=stable
juju status --wait 10m
juju ssh busybox-smoke/0 -- \
  'sudo apt-get update && sudo apt-get install -y busybox-static && /bin/busybox echo juju-firecracker-ok'
```

A passing smoke test must show the unit as `active` and print
`juju-firecracker-ok`. This validates a workload path; it does not prove
production isolation, performance, or controller HA.

5. Destroy every disposable resource after the test, even when the workload
fails:

```bash
juju destroy-application busybox-smoke --force
juju destroy-model fc-demo --destroy-storage --force --no-wait
juju destroy-controller "$DISPOSABLE_CONTROLLER" --destroy-all-models --force
```

This walkthrough is an acceptance contract, not a claim that the repository's
checkout has completed a live controller deployment. Record the exact first
failed prerequisite or command instead of substituting mocks. See
[docs/acceptance-audit.md](docs/acceptance-audit.md) for the current evidence and
known implementation boundary.

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
# Development-only install: cp juju-firecracker "$HOME/.local/bin/"
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

The canonical install, disposable bootstrap, BusyBox smoke test, and cleanup
procedure is documented above in [Install, bootstrap Juju, and run a workload
smoke test](#install-bootstrap-juju-and-run-a-workload-smoke-test). Keep that
section as the single source of truth. The procedure is prerequisite-gated and
must not be reported as passing without a real disposable controller run.


## Runtime configuration

All operator-configurable values are consolidated in `internal/provider/config.go`. Precedence: Juju model config > environment variables > defaults. Required fields are `kernel-image-path` and `rootfs-path`; all others have safe defaults.

### Model config keys (Juju `model-config`)

| Key | Default | Description |
|---|---|---|
| `kernel-image-path` | *(required)* | Guest kernel image (vmlinux) |
| `rootfs-path` | *(required)* | Guest root filesystem image |

| `cni-config-path` | `/etc/cni/net.d/juju-fc.conflist` | CNI conflist |
| `cni-bin-dirs` | `/opt/cni/bin:/usr/lib/cni:/usr/libexec/cni` | CNI plugin directories |
| `vcpu` | `1` | Guest virtual CPUs |
| `memory-mib` | `512` | Guest memory |
| `kernel-args` | *(empty)* | Guest kernel command line |
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
- Release binaries, checksum verification, and installer: [docs/release.md](docs/release.md).
- Runtime design and protocols: [docs/architecture.md](docs/architecture.md).
- Disposable Juju performance campaign and measurement matrix: [docs/performance-profiling-campaign.md](docs/performance-profiling-campaign.md).
- Disposable Canonical K8s cross-controller settings and gated smoke procedure: [docs/cross-controller-compatibility.md](docs/cross-controller-compatibility.md).
- Local development, tests, and pull requests: [CONTRIBUTING.md](CONTRIBUTING.md).
- Scope and acceptance criteria: [PROJECT.md](PROJECT.md).
