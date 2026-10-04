# Juju integration bundle

Stock Juju does not discover `juju-firecracker` as an external provider. A
custom bundle is required: `scripts/build-juju-firecracker.sh` fetches and
verifies Juju commit `f5b474c76ebac3934e2df1e173f85922c48258f5`, vendors the
provider into Juju's existing `internal/provider/all` registration package,
and builds matching `juju` and `jujud` binaries. The bundle is local-only;
this repository does not publish these binaries.

Build in a disposable directory:

    scripts/build-juju-firecracker.sh /tmp/juju-firecracker-bundle

The generated `BUILD-INFO` records the source revision and registration path.
The registration-boundary test runs against the patched Juju source before the
two binaries are compiled. Firecracker has no cloud credentials and exposes a
single `local` region. Real bootstrap and VM lifecycle testing still requires
the isolated host prerequisites documented in `README.md`.

The machine-readable compatibility contract is in
`compatibility/juju-firecracker.json`.