# Release and installation

The provider-only release binary is a development/runtime artifact, not a Juju
provider plugin. Releases are created by `.github/workflows/release.yml` only for `vMAJOR.MINOR.PATCH`
tags or an explicit workflow dispatch with that same version. The workflow
builds the provider registration binary with `CGO_ENABLED=0` for Linux `amd64`
and `arm64`, embeds the release tag as build metadata, and publishes named
binary assets and `checksums.txt` to the GitHub Release.

The workflow grants read-only repository access to build jobs and write access
only to the publish job. It validates the version before using it, never
executes release metadata, and pins its third-party actions. Release assets are
immutable by convention: a tag should be used once; publish a new patch version
rather than replacing an asset.

## Installation

Use the repository-owned POSIX installer:

```sh
curl -fsSL https://raw.githubusercontent.com/yanksyoon/juju-provider-firecracker/main/scripts/install.sh | sh
```

Pin a release for reproducibility:

```sh
curl -fsSL https://raw.githubusercontent.com/yanksyoon/juju-provider-firecracker/main/scripts/install.sh \
  | JUJU_FIRECRACKER_VERSION=v1.2.3 INSTALL_DIR="$HOME/.local/bin" sh
```

The installer accepts `vMAJOR.MINOR.PATCH`, rejects other versions, supports
Linux `amd64` and `arm64`, downloads the matching binary and checksum manifest
over HTTPS, and verifies the SHA256 before installation. It defaults to
`/usr/local/bin`, falls back to `$HOME/.local/bin` when that directory is not
writable, and accepts an absolute `INSTALL_DIR`. `TMPDIR`, when set, must be an
existing writable directory. A privileged fallback requires non-interactive
`sudo`; otherwise select a writable directory. It stages and atomically renames
the downloaded binary, so a failed verification or installation never replaces
an existing binary.

After installation, the printed binary can be used for provider-package and
lifecycle development only. It is not loaded by stock Juju and cannot make
`firecracker` discoverable. Do not use it as a workaround for
`juju bootstrap firecracker`; that command requires matching custom `juju` and
`jujud` binaries built from the pinned Juju integration described in
`docs/juju-provider-registration-boundary-plan.md`. To roll back, install an
earlier provider release. To uninstall, remove the printed
`juju-firecracker` file; no service or host configuration is created.

## Development fallback

For local changes, build from source with `go build -o juju-firecracker
./cmd/juju-firecracker`. Run `go test ./...`, `go vet ./...`, and the workflow
checks before proposing a release. Source builds do not replace the release
artifacts and are not required for ordinary installation.
