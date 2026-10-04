# Release and installation

The supported user path is the release binary, not a source checkout. Releases
are created by `.github/workflows/release.yml` only for `vMAJOR.MINOR.PATCH`
tags or an explicit workflow dispatch with that same version. The workflow
builds the provider registration binary with `CGO_ENABLED=0` for Linux `amd64`
and `arm64`, embeds the release tag as build metadata, and publishes named
binary assets, `version.txt`, and `checksums.txt` to the GitHub Release.

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
writable, and accepts `INSTALL_DIR`. A privileged fallback requires non-
interactive `sudo`; otherwise select a writable directory. It stages and
atomically renames user-writable installs, so a failed verification never
replaces an existing binary.

After installation, ensure the printed path is on the host's Juju provider
search path. To roll back, install an earlier release. To uninstall, remove the
printed `juju-firecracker` file; no service or host configuration is created by
the installer.

## Development fallback

For local changes, build from source with `go build -o juju-firecracker
./cmd/juju-firecracker`. Run `go test ./...`, `go vet ./...`, and the workflow
checks before proposing a release. Source builds do not replace the release
artifacts and are not required for ordinary installation.
