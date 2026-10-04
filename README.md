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

The package code is under `internal/`; the provider is currently a library and does not ship an installable command-line binary. `test/integration` contains both safe prerequisite-discovery tests and an explicitly gated destructive path. On an ordinary workstation, run the package tests; the end-to-end path additionally needs the privileged prerequisites above and disposable guest assets.

## Example integration configuration

`configs/juju-fc.conflist` is a reference configuration, not an instruction to modify a host. A deliberately isolated integration host must provide its own disposable copy, kernel, rootfs, CNI plugins, bridge, and writable cgroup subtree. Set `JUJU_FC_E2E_CNI_CONFIG`, `JUJU_FC_CGROUP_BASE`, `JUJU_FC_E2E_KERNEL`, `JUJU_FC_E2E_ROOTFS`, and `JUJU_FC_RUN_E2E=1` only for that host, then run:

```bash
go test -count=1 -timeout=60s -v ./test/integration -run '^TestEndToEnd$'
```

## Development status

Tasks 1–5 in `PROJECT.md` describe the provider and integration work. Task 6 supplies this documentation and CI policy. The CI workflow runs formatting, vet, unit tests, and race tests on a hosted runner. The privileged integration job is manual, requires a labeled self-hosted runner, and is never run by pushes or pull requests.

## Where to change things

- Setup and repository status: this file.
- Runtime design and protocols: [docs/architecture.md](docs/architecture.md).
- Local development, tests, and pull requests: [CONTRIBUTING.md](CONTRIBUTING.md).
- Scope and acceptance criteria: [PROJECT.md](PROJECT.md).
