# Contributing

This repository contains the specification and Go implementation. Read [README.md](README.md) for implementation status and [docs/architecture.md](docs/architecture.md) for runtime design. Release and installation behavior is documented in [docs/release.md](docs/release.md). `PROJECT.md` is the scope and acceptance-criteria document.

## Local prerequisites

Documentation-only changes require Git and a Markdown-capable editor. Go 1.26.6+, the pinned Juju dependency, Linux cgroup v2, CNI plugins, `/dev/kvm`, and Firecracker are needed only for the corresponding runtime or integration checks. Do not install or configure privileged host components just to edit documentation.

## Checks before opening a pull request

Run checks that exist in the checkout:

```bash
git diff --check
```

Run the repository's Go checks from its root:

```bash
go test ./...
go vet ./...
go test -race ./...
test -z "$(gofmt -l .)"
```

The workflow uses the same safe commands. Keep this document and `.github/workflows/ci.yml` synchronized when commands or gates change.

Release changes must also preserve the checks in `.github/workflows/release.yml`:
only semantic-version tags or an explicit dispatch may publish, and the release
must contain both Linux architectures, `version.txt`, and `checksums.txt`. Do
not test an installer by writing to `/usr/local/bin`; use a temporary
`INSTALL_DIR` and a local fixture or static validation instead. Never commit
generated binaries or release credentials.

The end-to-end suite is not a normal unit-test command. It requires Linux, KVM, cgroup v2, Firecracker, a working CNI installation, and disposable network, kernel, rootfs, and cgroup configuration. Run it only on a deliberately configured host with `JUJU_FC_RUN_E2E=1`; the test refuses system CNI paths and configured production Juju targets. The safe Go checks must pass before privileged testing is attempted.

## Code and documentation guidelines

- Use the Go version declared by `go.mod` (currently Go 1.26.6) once the module is introduced; do not assume an older toolchain is supported.
- Keep dependencies pinned and avoid machine-specific absolute paths in source, tests, and docs. Host paths that are part of the privileged contract (such as `/etc/cni/net.d`) must be explicit and configurable where appropriate.
- Return actionable errors and preserve the instance ID in operational context, but never log user-data, credentials, or tokens.
- Make cleanup idempotent and test failure paths, not just the successful VM lifecycle.
- Keep unit tests unprivileged by using fakes for CNI, Firecracker, and cgroups.
- Put architecture and protocol decisions in `docs/architecture.md`; do not create a second competing setup guide.
- Use Markdown headings, fenced commands, and links that work from the repository root.

## Pull requests

1. Keep each pull request focused on one task from `PROJECT.md`.
2. Explain behavior changes, host prerequisites, and the tests you ran.
3. Do not include secrets, guest user-data, generated binaries, local CNI state, Firecracker sockets, or machine-specific configuration.
4. Ensure the safe CI job passes. Privileged integration results must be reported separately and must not be presented as proof that an ordinary hosted runner has KVM or CNI access.
5. Update the relevant source-of-truth document when behavior or prerequisites change.

Maintainers review correctness, cleanup on failure, security of metadata handling, and whether documentation matches executable commands before merging.
