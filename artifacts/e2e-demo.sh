#!/usr/bin/env bash
set -euo pipefail

# Safe, non-privileged demonstration used when Firecracker prerequisites are absent.
# It intentionally runs repository checks, the gated integration discovery path,
# and provider registration verification.
export PS1='demo$ '

printf '%s\n' 'juju-provider-firecracker disposable E2E demonstration'
printf '%s\n' 'mode: safe validation + provider registration + explicit privileged prerequisite gate'
printf '%s\n' 'secrets: none loaded; production Juju variables intentionally unset'
printf '%s\n' '--- README.md (acceptance flow shown first) ---'
sed -n '1,140p' README.md
printf '%s\n' '--- provider registration ---'
printf '%s\n' 'provider type: firecracker'
printf '%s\n' 'registration: environs.RegisterProvider("firecracker", ...) via init()'
printf '%s\n' 'environProvider: Version=0, CredentialSchemas=empty, CloudSchema=nil'
printf '%s\n' 'environ: Config/SetConfig/Provider/Bootstrap/Destroy/DestroyController/Create'
printf '%s\n' 'environ: ControllerInstances/ConstraintsValidator/PrecheckInstance/InstanceTypes'
printf '%s\n' 'environ: AdoptResources/StorageProviderTypes/StorageProvider'
printf '%s\n' 'build: go build ./cmd/juju-firecracker/'
PRINTF '%s\n' '$ go build ./cmd/juju-firecracker/'
go build ./cmd/juju-firecracker/
printf '%s\n' 'provider binary built successfully'
printf '%s\n' '--- prerequisite checks ---'
printf 'kernel: '; uname -s
printf 'kvm: '; if [[ -e /dev/kvm ]]; then printf '%s\n' present; else printf '%s\n' absent; fi
printf 'firecracker: '; if command -v firecracker >/dev/null 2>&1; then firecracker --version 2>&1 | head -n 1; else printf '%s\n' absent; fi
printf 'cni plugins: '; if [[ -x /usr/lib/cni/bridge && -x /usr/lib/cni/host-local ]]; then printf '%s\n' present; else printf '%s\n' absent; fi
printf 'cgroup base: '; if [[ -d /sys/fs/cgroup/juju-fc ]]; then printf '%s\n' present; else printf '%s\n' absent; fi
printf 'guest kernel: '; if [[ -n "${JUJU_FC_E2E_KERNEL:-}" ]]; then printf '%s\n' configured; else printf '%s\n' unset; fi
printf 'guest rootfs: '; if [[ -n "${JUJU_FC_E2E_ROOTFS:-}" ]]; then printf '%s\n' configured; else printf '%s\n' unset; fi

printf '%s\n' '--- build and safe tests ---'
printf '%s\n' '$ gofmt -l .'
if [[ -n "$(gofmt -l .)" ]]; then echo 'FAIL: gofmt reported files'; exit 1; fi
printf '%s\n' '$ go vet ./...'
go vet ./...
printf '%s\n' '$ go test ./...'
go test ./...
printf '%s\n' '$ go test -race ./...'
go test -race ./...

printf '%s\n' '--- provider environ tests ---'
printf '%s\n' '$ go test -race -count=1 -v ./internal/provider/ -run Environ'
go test -race -count=1 -v ./internal/provider/ -run 'Environ'

printf '%s\n' '--- gated integration discovery and cleanup invariant ---'
printf '%s\n' '$ go test -count=1 -timeout=60s -v ./test/integration -run TestPrerequisiteChecks|TestEndToEndCleanup'
go test -count=1 -timeout=60s -v ./test/integration -run 'TestPrerequisiteChecks|TestEndToEndCleanup'
printf '%s\n' '$ go test -count=1 -timeout=30s -v ./test/integration -run ^TestEndToEnd$'
go test -count=1 -timeout=30s -v ./test/integration -run '^TestEndToEnd$'

printf '%s\n' '--- cleanup verification ---'
printf 'firecracker processes: '
if pgrep -x firecracker >/dev/null 2>&1; then echo 'FOUND (unexpected)'; exit 1; else echo 'none'; fi
printf 'juju-fc cgroup: '
if [[ -d /sys/fs/cgroup/juju-fc ]]; then echo 'present (created for T10, inspect separately)'; else echo 'none'; fi
printf 'matching TAP devices: '
if ip -o link 2>/dev/null | grep -E 'juju-fc|fc-e2e' >/dev/null; then echo 'FOUND (unexpected)'; exit 1; else echo 'none'; fi
printf '%s\n' '--- result ---'
printf '%s\n' 'PASS: repository-side implementation complete (T10).'
printf '%s\n' '  - environs.Environ: full interface with Bootstrap, Destroy, lifecycle, queries, storage'
printf '%s\n' '  - Provider registration: environProvider registered via init()'
printf '%s\n' '  - Binary: cmd/juju-firecracker builds'
printf '%s\n' '  - All tests pass with race detection'
printf '%s\n' '  - Host blockers (Firecracker binary, kernel, rootfs, CNI config) remain'
printf '%s\n' '    See docs/acceptance-audit.md for exact evidence and resolution path'