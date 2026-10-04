# Juju provider registration boundary

Status: implementation boundary for the pinned Juju API; this repository does not publish a custom Juju binary.

## Decision

Stock Juju cannot load `juju-firecracker` as an external provider plugin. The provider must be registered in-process by a Juju fork. The fork must blank-import this repository's public `provider` package from Juju's `internal/provider/all` package, and the same integration must be built into both `juju` and `jujud`.

The pinned API is Juju commit `f5b474c76eba` (`v0.0.0-20260930100434-f5b474c76eba`). The public import boundary is:

```go
//go:build !minimal || provider_firecracker
package all

import _ "github.com/canonical/juju-provider-firecracker/provider"
```

The default fork build includes the provider; minimal builds require `provider_firecracker`. `provider` is a public facade whose import triggers the existing lifecycle provider registration. The standalone `cmd/juju-firecracker` binary remains a development artifact and is not a plugin.

## Provider contract

The provider registers `firecracker` with `environs.RegisterProvider` and implements `environs.CloudRegionDetector`. It reports exactly one local region, `local`, with no endpoint fields and no credentials. This is required because the pinned bootstrap command rejects a provider-name invocation that cannot detect regions. The lifecycle implementation and model configuration keys remain in `internal/provider`.

The safe registration test in `internal/provider/environ_test.go` proves, in a real Juju process, that `environs.Provider("firecracker")` resolves, the provider returns `local`, and no credential schemas are advertised. It does not contact a controller, Firecracker, CNI, KVM, or a production endpoint.

## Reproducible fork build

Use an immutable Juju fork commit based on the pinned commit and record both repository commits in release metadata. During development, use a workspace containing both repositories and verify the module graph has no cycle:

```sh
git clone --branch <juju-fork-ref> https://github.com/<owner>/juju.git juju-firecracker-build
git -C juju-firecracker-build rev-parse HEAD
go work init ./juju-firecracker-build ./juju-provider-firecracker
go work sync
```

From the fork, run the Juju registration/bootstrap tests and build both processes:

```sh
go test ./environs ./cmd/juju/commands ./internal/provider/all
go build -trimpath -o dist/juju ./cmd/juju
go build -trimpath -o dist/jujud ./cmd/jujud
```

A safe process smoke check is `./dist/juju bootstrap firecracker fc-test-controller --no-gui --debug`; it must progress beyond `unknown cloud "firecracker"` before any privileged acceptance run. Release the matching `juju`, `jujud`, and provider compatibility tuple together with checksums. A custom client without matching controller/agent binaries is not supported.

The Juju fork owner/ref, release channel, and controller packaging are intentionally not invented here. Until that fork exists, README and release documentation must not claim that installing this repository's provider binary enables stock Juju bootstrap.
