#!/usr/bin/env bash
set -euo pipefail

# Safe, non-privileged demonstration used when Firecracker prerequisites are absent.
# It intentionally runs repository checks and the gated integration discovery path.
export PS1='demo$ '

printf '%s\n' 'juju-provider-firecracker disposable E2E demonstration'
printf '%s\n' 'mode: safe validation + explicit privileged prerequisite gate'
printf '%s\n' 'secrets: none loaded; production Juju variables intentionally unset'
printf '%s\n' '--- README.md (acceptance flow shown first) ---'
sed -n '1,140p' README.md
printf '%s\n' '--- prerequisite checks ---'
printf 'kernel: '; uname -s
printf 'kvm: '; if [[ -e /dev/kvm ]]; then printf '%s\n' present; else printf '%s\n' absent; fi
printf 'firecracker: '; if command -v firecracker >/dev/null 2>&1; then firecracker --version 2>&1 | head -n 1; else printf '%s\n' absent; fi
printf 'cni plugins: '; if [[ -x /opt/cni/bin/bridge && -x /opt/cni/bin/host-local ]]; then printf '%s\n' present; else printf '%s\n' absent; fi
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

printf '%s\n' '--- gated integration discovery and cleanup invariant ---'
printf '%s\n' '$ go test -count=1 -timeout=60s -v ./test/integration -run TestPrerequisiteChecks|TestEndToEndCleanup'
go test -count=1 -timeout=60s -v ./test/integration -run 'TestPrerequisiteChecks|TestEndToEndCleanup'
printf '%s\n' '$ go test -count=1 -timeout=30s -v ./test/integration -run ^TestEndToEnd$'
go test -count=1 -timeout=30s -v ./test/integration -run '^TestEndToEnd$'

printf '%s\n' '--- cleanup verification ---'
printf 'firecracker processes: '
if pgrep -x firecracker >/dev/null 2>&1; then echo 'FOUND (unexpected)'; exit 1; else echo 'none'; fi
printf 'juju-fc cgroup: '
if [[ -d /sys/fs/cgroup/juju-fc ]]; then echo 'present (inspect separately)'; else echo 'none'; fi
printf 'matching TAP devices: '
if ip -o link 2>/dev/null | grep -E 'juju-fc|fc-e2e' >/dev/null; then echo 'FOUND (unexpected)'; exit 1; else echo 'none'; fi
printf '%s\n' '--- result ---'
printf '%s\n' 'PASS: safe pipeline executed; privileged lifecycle was not attempted because prerequisites were unavailable.'
