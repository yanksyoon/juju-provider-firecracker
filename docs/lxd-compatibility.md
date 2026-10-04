# LXD compatibility smoke path

This repository supports a bounded, opt-in compatibility check for running the
Firecracker provider registration binary from an isolated LXD guest. It does
not claim that a Juju controller can bootstrap a Firecracker workload: the
provider still needs a disposable guest kernel, rootfs, CNI setup, and a host
with the required privileges for that acceptance test.

## Boundary and prerequisites

The LXD host owns the LXD daemon and creates one disposable guest. The guest
owns the Juju CLI and `/usr/local/bin/juju-firecracker`; Firecracker, `/dev/kvm`,
CNI, cgroups, kernel, and rootfs must be available in the execution environment
where the provider is actually exercised. The smoke script does not copy host
CNI state or guest images, and it never selects a controller or model.

Run the side-effect-free host preflight first:

```bash
scripts/lxd-firecracker-smoke.sh --preflight
```

The preflight requires Linux, an accessible LXD daemon, the Juju CLI, a
Firecracker executable, `/dev/kvm`, the configured CNI conflist, and executable
`bridge` and `host-local` CNI plugins. Override the CNI inputs when they are
not at the normal host paths:

```bash
JUJU_FC_LXD_CNI_CONFIG=/path/to/disposable/juju-fc.conflist \
CNI_PATH=/path/to/cni/bin:/another/cni/bin \
scripts/lxd-firecracker-smoke.sh --preflight
```

## Opt-in smoke test

The smoke test requires an explicit gate and an executable provider binary.
Use a disposable LXD image that already contains the Juju CLI; the default
Ubuntu image is only a placeholder and is not assumed to contain Juju.

```bash
JUJU_FC_LXD_RUN=1 \
JUJU_FC_PROVIDER_BINARY="$PWD/juju-firecracker" \
JUJU_FC_LXD_IMAGE=local:juju-test-image \
scripts/lxd-firecracker-smoke.sh --smoke
```

The script creates one uniquely named `juju-fc-smoke-*` guest, enables nesting
and the KVM device, copies only the provider registration binary, checks Juju
provider discovery inside the guest, and deletes the guest in an EXIT/TERM
trap. Set `JUJU_FC_LXD_NAME` only to a name that is known to be disposable; the
script rejects names containing characters outside `[A-Za-z0-9-]`. It refuses
to use configured controller/model targets and does not touch existing LXD
instances.

This smoke path verifies the provider-registration and LXD guest lifecycle
boundary only. A successful result is not evidence that Firecracker can boot a
Juju machine. A real controller/model acceptance run must separately provide
explicit disposable controller and model names, guest kernel/rootfs, CNI
configuration and plugins, writable cgroup v2, `/dev/kvm`, and a cleanup audit.
