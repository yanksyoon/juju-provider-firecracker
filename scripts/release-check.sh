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
grep -F 'sha256sum' "$ROOT/.github/workflows/release.yml" >/dev/null
grep -F 'EXPECTED=' "$ROOT/scripts/install.sh" >/dev/null
grep -F 'ACTUAL=' "$ROOT/scripts/install.sh" >/dev/null
grep -F 'TMPDIR must be an existing writable directory' "$ROOT/scripts/install.sh" >/dev/null
grep -F 'INSTALL_DIR must be an absolute path' "$ROOT/scripts/install.sh" >/dev/null
grep -F "tags:" "$ROOT/.github/workflows/release.yml" >/dev/null
grep -F "workflow_dispatch:" "$ROOT/.github/workflows/release.yml" >/dev/null
grep -F "permissions:" "$ROOT/.github/workflows/release.yml" >/dev/null
grep -F "CGO_ENABLED=0" "$ROOT/.github/workflows/release.yml" >/dev/null
# Installer must target an existing repository.
installer_owner=$(awk -F"'" '/^OWNER=/{print $2}' "$ROOT/scripts/install.sh")
installer_repo=$(awk -F"'" '/^REPOSITORY=/{print $2}' "$ROOT/scripts/install.sh")
git_owner=$(git remote get-url origin | sed -n 's|.*github.com[:/]\([^/]*\)/.*|\1|p')
test "$git_owner" = "$installer_owner" || { printf 'installer OWNER %s does not match git remote owner %s\n' "$installer_owner" "$git_owner" >&2; exit 1; }
# CI must pin actions to SHAs, not mutable version tags.
! grep -F 'uses: actions/' "$ROOT/.github/workflows/ci.yml" | grep -Eq '@v[0-9]+$' || { printf 'CI workflow contains unpinned action tags\n' >&2; exit 1; }
printf '%s\n' 'release workflow and installer checks passed'
