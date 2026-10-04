#!/bin/sh
# Install a released Juju Firecracker provider registration binary.
set -eu

OWNER='yanksyoon'
REPOSITORY='juju-provider-firecracker'
RELEASES_URL="https://github.com/${OWNER}/${REPOSITORY}/releases"

fail() { printf '%s\n' "install: $*" >&2; exit 1; }
usage() {
    cat <<'EOF'
Usage: install.sh [VERSION]

Install the Juju Firecracker provider from a GitHub Release. VERSION may be
provided as vMAJOR.MINOR.PATCH; otherwise the latest release is selected.
Set JUJU_FIRECRACKER_VERSION or pass VERSION to pin a release. Set INSTALL_DIR
to choose the destination (default: /usr/local/bin, or ~/.local/bin when the
former is not writable). The downloaded binary and SHA256 manifest are verified
before the existing installation is replaced.
EOF
}

[ "${1:-}" = '-h' ] || [ "${1:-}" = '--help' ] && { usage; exit 0; }
[ "$#" -le 1 ] || fail 'expected at most one version argument'

OS=$(uname -s)
ARCH=$(uname -m)
[ "$OS" = Linux ] || fail "unsupported operating system: $OS (Linux is required)"
command -v awk >/dev/null 2>&1 || fail 'awk is required'
case "$ARCH" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) fail "unsupported architecture: $ARCH (amd64 and arm64 are supported)" ;;
esac

VERSION=${1:-${JUJU_FIRECRACKER_VERSION:-}}
if [ -z "$VERSION" ]; then
    command -v curl >/dev/null 2>&1 || fail 'curl is required'
    latest_url=$(curl -fsSLI --proto '=https' --tlsv1.2 -o /dev/null -w '%{url_effective}' "${RELEASES_URL}/latest") || fail 'could not determine the latest release'
    VERSION=${latest_url##*/}
fi
awk -v version="$VERSION" 'BEGIN { exit(version ~ /^v[0-9]+\.[0-9]+\.[0-9]+$/ ? 0 : 1) }' || \
    fail 'version must have the form vMAJOR.MINOR.PATCH'

command -v curl >/dev/null 2>&1 || fail 'curl is required'
if command -v sha256sum >/dev/null 2>&1; then
    SHA256='sha256sum'
elif command -v shasum >/dev/null 2>&1; then
    SHA256='shasum -a 256'
else
    fail 'sha256sum or shasum is required'
fi

ASSET="juju-firecracker_${VERSION}_linux_${ARCH}"
BASE_URL="${RELEASES_URL}/download/${VERSION}"
TMP_BASE=${TMPDIR:-/tmp}
[ -d "$TMP_BASE" ] && [ -w "$TMP_BASE" ] || fail 'TMPDIR must be an existing writable directory'
TMP_DIR=$(umask 077 && mktemp -d "$TMP_BASE/juju-firecracker-install.XXXXXX") || \
    fail 'cannot create a private temporary directory'
STAGED=''
cleanup() {
    rm -rf "$TMP_DIR"
    [ -z "$STAGED" ] || rm -f "$STAGED" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

curl -fsSL --proto '=https' --tlsv1.2 "${BASE_URL}/${ASSET}" -o "${TMP_DIR}/${ASSET}" || fail "could not download ${ASSET}"
curl -fsSL --proto '=https' --tlsv1.2 "${BASE_URL}/checksums.txt" -o "${TMP_DIR}/checksums.txt" || fail 'could not download checksums.txt'

EXPECTED=$(awk -v asset="$ASSET" '$2 == asset { print $1; found=1 } END { if (!found) exit 1 }' "${TMP_DIR}/checksums.txt") || fail "checksum entry for ${ASSET} is missing"
ACTUAL=$($SHA256 "${TMP_DIR}/${ASSET}" | awk '{print $1}')
[ "$EXPECTED" = "$ACTUAL" ] || fail 'checksum verification failed'

if [ -n "${INSTALL_DIR:-}" ]; then
    DEST_DIR=$INSTALL_DIR
else
    DEST_DIR=/usr/local/bin
    if [ ! -d "$DEST_DIR" ] || [ ! -w "$DEST_DIR" ]; then
        [ -n "${HOME:-}" ] || fail 'HOME is required when /usr/local/bin is not writable'
        DEST_DIR=$HOME/.local/bin
    fi
fi
case "$DEST_DIR" in
    /*) ;;
    *) fail 'INSTALL_DIR must be an absolute path' ;;
esac
if [ ! -d "$DEST_DIR" ]; then
    mkdir -p "$DEST_DIR" 2>/dev/null || true
fi
DEST="$DEST_DIR/juju-firecracker"

# Install to a sibling temporary path, then atomically replace the destination.
if [ -w "$DEST_DIR" ]; then
    STAGED=$(mktemp "${DEST_DIR}/.juju-firecracker.XXXXXX") || fail 'cannot create staging file'
    cp "$TMP_DIR/$ASSET" "$STAGED" || fail "cannot stage binary in $DEST_DIR"
    chmod 0755 "$STAGED"
    mv -f "$STAGED" "$DEST"
else
    command -v sudo >/dev/null 2>&1 || fail "cannot write $DEST_DIR; set INSTALL_DIR to a writable directory"
    sudo -n true 2>/dev/null || fail "cannot write $DEST_DIR without interactive sudo; set INSTALL_DIR"
    STAGED=$(sudo mktemp "$DEST_DIR/.juju-firecracker.XXXXXX") || fail 'cannot create privileged staging file'
    sudo cp "$TMP_DIR/$ASSET" "$STAGED" && sudo chmod 0755 "$STAGED" && sudo mv -f "$STAGED" "$DEST" || fail "cannot install binary in $DEST_DIR"
fi

printf 'Installed juju-firecracker %s at %s\n' "$VERSION" "$DEST"
