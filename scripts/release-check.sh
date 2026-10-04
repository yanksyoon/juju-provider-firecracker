#!/bin/sh
# Deterministic checks for the release workflow and installer.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)

sh -n "$ROOT/scripts/install.sh"
case "$(uname -s)" in Linux) ;; *) printf '%s\n' 'release checks require Linux' >&2; exit 1 ;; esac

grep -F 'raw.githubusercontent.com/yanksyoon/juju-provider-firecracker/main/scripts/install.sh' "$ROOT/README.md" >/dev/null
test "$(grep -F -c 'raw.githubusercontent.com' "$ROOT/scripts/install.sh")" -eq 0
test "$(grep -F -c 'raw.githubusercontent.com' "$ROOT/README.md")" -ge 1
test "$(grep -F -c 'checksums.txt' "$ROOT/.github/workflows/release.yml")" -ge 2
grep -F "tags:" "$ROOT/.github/workflows/release.yml" >/dev/null
grep -F "workflow_dispatch:" "$ROOT/.github/workflows/release.yml" >/dev/null
grep -F "permissions:" "$ROOT/.github/workflows/release.yml" >/dev/null
grep -F "CGO_ENABLED=0" "$ROOT/.github/workflows/release.yml" >/dev/null
printf '%s\n' 'release workflow and installer checks passed'
