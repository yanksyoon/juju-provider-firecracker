# Architecture

This document is the source of truth for runtime design and protocols. Repository setup and implementation status are in [../README.md](../README.md); contributor and test procedures are in [../CONTRIBUTING.md](../CONTRIBUTING.md).

## Scope and status

The provider adapts the Juju instance-broker lifecycle while managing Firecracker microVMs directly. The design is specified in `PROJECT.md` Tasks 1–5. The checkout provides a standalone provider registration executable at `cmd/juju-firecracker`; supported release binaries and their verified installer are documented in [release.md](release.md). A live-controller deployment still requires the host prerequisites and acceptance flow described in [acceptance-audit.md](acceptance-audit.md).

The toolchain is the Go version declared in `go.mod` (currently Go 1.26.6). The Juju 3.x API dependency is pinned in `go.mod`. No unpinned dependency or local-home path is part of this design.

## Configuration

All runtime configuration is consolidated in a single typed structure (`internal/provider/config.go`). The provider reads configuration with this precedence (highest first):

1. Juju model config (`model-config` keys)
2. Environment variables (`JUJU_FC_*` and `CNI_PATH`)
3. Hardcoded defaults (backwards-compatible with prior releases)

Required fields: `kernel-image-path`, `rootfs-path`. All other fields have safe defaults documented in [README.md](../README.md#runtime-configuration).

The `Config` struct carries paths (CNI config, CNI bin dirs, cgroup base, config/socket dirs, Firecracker binary, kernel, rootfs), network bind address, and timeouts (VM stop grace, metadata server shutdown). Validation rejects missing required fields, non-canonical paths, traversal components, invalid addresses, non-`.conflist` CNI paths, and non-positive timeouts.

## Components

The provider coordinates three resource managers:

1. **CNI network manager** allocates and releases a VM network attachment using the configured CNI conflist.
2. **Metadata server** serves per-instance user data over HTTP while the guest boots.
3. **Firecracker manager** starts and stops the Firecracker process and places it in a dedicated cgroup v2 subtree.

The provider's start path is:

```text
Juju StartInstance
  -> CNI ADD(instance ID)
  -> register user data
  -> create Firecracker config (kernel, rootfs, TAP)
  -> start Firecracker and assign cgroup
  -> return instance identity and address
```

The stop path reverses the allocations and must be safe to repeat:

```text
Juju StopInstances
  -> terminate Firecracker (SIGTERM, then SIGKILL after timeout)
  -> remove cgroup
  -> CNI DEL(instance ID)
```

Cleanup is required on every failed start after the first successful allocation. Instance IDs are the correlation key across CNI, metadata, Firecracker, and cgroup state.

## CNI networking flow

The network manager loads the `juju-fc` conflist and calls CNI `ADD` with the instance ID. It extracts the allocated IP address and TAP/device name from the result and passes the device into the Firecracker network interface configuration. On teardown it calls CNI `DEL`; teardown is idempotent for an already-removed attachment.

The sample configuration described by `PROJECT.md` uses a bridge named `fc-br0` and host-local IPAM on `192.168.100.0/24`. The bridge, subnet, routes, firewall policy, and CNI plugin binaries are host administration concerns. They must not be silently created by the provider or by CI.

A production host must ensure that:

- the bridge and CNI binary directory exist;
- the provider can access the network namespace and CNI state directory;
- the address pool does not overlap with the host or Juju networks; and
- cleanup is performed after crashes as well as normal stops.

## Metadata protocol

The metadata server maintains an in-memory mapping from instance ID to user-data payload, protected by a read/write lock. The endpoints implemented by the current server are:

- `GET /latest/meta-data/instance-id?instance=<instance-id>` — returns the requested instance ID. `X-Instance-ID` and `/latest/meta-data/instance-id/<instance-id>` are also accepted.
- `GET /latest/user-data?instance=<instance-id>` — returns the registered payload for that instance; `X-Instance-ID` is also accepted.

The current server returns `400` when no instance ID is supplied and `404` for unknown user data, avoids logging payloads, and binds to loopback with an explicitly selected port. A query parameter alone is not an authorization boundary; production deployment must bind access to the guest network and add an authentication boundary before exposing sensitive payloads.

## Firecracker and cgroups

Each VM is launched with its own API socket and configuration file. The manager sets a parent-death signal, tracks the process by instance ID, and places the process in `/sys/fs/cgroup/juju-fc/<instance-id>` under cgroup v2. Stopping sends SIGTERM, waits up to five seconds, and then sends SIGKILL if needed before removing the cgroup.

Cgroup placement and KVM access are privileged host operations. They are not simulated by unit tests and are not assumed by GitHub-hosted runners. The integration test must be explicitly enabled on a suitably configured self-hosted Linux runner and must clean up sockets, processes, cgroups, TAP devices, and CNI allocations even on failure.

## Failure and cleanup invariants

- A failed CNI allocation must not start Firecracker.
- A failed metadata registration must release the CNI allocation.
- A failed Firecracker start must remove metadata and release networking.
- `StopInstances` is idempotent and attempts all requested IDs even if one cleanup operation fails.
- User-data, credentials, and guest payloads are never committed to the repository or printed in CI logs.
- Unit tests use fakes for privileged components; only the gated integration suite uses a real CNI configuration, KVM, Firecracker, and cgroup hierarchy.
