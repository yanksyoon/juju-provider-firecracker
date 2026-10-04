#!/bin/sh
set -eu

# Build matching juju and jujud binaries with the Firecracker provider vendored
# into Juju's existing in-process registration boundary.

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
JUJU_VERSION=${JUJU_FIRECRACKER_JUJU_VERSION:-v0.0.0-20260930100434-f5b474c76eba}
JUJU_HASH=f5b474c76eba
OUTPUT=${1:-"$REPO_ROOT/dist/juju-firecracker-bundle"}
TARGET_GOOS=${GOOS:-$(go env GOOS)}
TARGET_GOARCH=${GOARCH:-$(go env GOARCH)}

case "$OUTPUT" in
  /|/tmp|/home|/home/ubuntu) printf '%s\n' "refusing unsafe output directory: $OUTPUT" >&2; exit 2 ;;
esac

command -v go >/dev/null 2>&1 || { printf '%s\n' 'go is required' >&2; exit 2; }


WORK=$(mktemp -d "${TMPDIR:-/tmp}/juju-firecracker.XXXXXX")
trap 'rm -rf "$WORK"' EXIT HUP INT TERM
DOWNLOAD_JSON=$WORK/download.json
go mod download -json "github.com/juju/juju@$JUJU_VERSION" >"$DOWNLOAD_JSON"

# Do not trust a version string alone: the module proxy must report the exact
# immutable VCS revision selected by the compatibility record.
grep -F '"Hash": "f5b474c76ebac3934e2df1e173f85922c48258f5"' "$DOWNLOAD_JSON" >/dev/null || {
  printf '%s\n' "Juju source hash does not match $JUJU_HASH" >&2; exit 1;
}
SOURCE_CACHE=$(sed -n 's/^[[:space:]]*"Dir": "\(.*\)",$/\1/p' "$DOWNLOAD_JSON")
[ -n "$SOURCE_CACHE" ] && [ -f "$SOURCE_CACHE/go.mod" ] || {
  printf '%s\n' 'go mod download returned no source directory' >&2; exit 1;
}
SOURCE="$WORK/juju"
mkdir -p "$SOURCE"
cp -R --no-preserve=mode,ownership,timestamps "$SOURCE_CACHE"/. "$SOURCE/"

# Keep the provider inside Juju's module so Go internal visibility and the
# controller/CLI registration boundary remain valid. Tests are copied into
# the same package and therefore exercise the actual registration path.
VENDOR_ROOT="$SOURCE/internal/firecracker-provider"
mkdir -p "$VENDOR_ROOT"
cp -R "$REPO_ROOT/internal"/. "$VENDOR_ROOT/"
find "$VENDOR_ROOT" -type f -name '*.go' -exec sed -i \
  's#github.com/canonical/juju-provider-firecracker/internal/#github.com/juju/juju/internal/firecracker-provider/#g' {} +
sed '/^\/\/go:build juju_firecracker_bundle$/d' \
  "$REPO_ROOT/test/juju-registration/firecracker_test.go" > \
  "$SOURCE/internal/provider/all/firecracker_test.go"
cat >"$SOURCE/internal/provider/all/firecracker.go" <<'EOF'
//go:build !minimal || provider_firecracker

package all

import _ "github.com/juju/juju/internal/firecracker-provider/provider"
EOF
gofmt -w "$VENDOR_ROOT" "$SOURCE/internal/provider/all/firecracker.go" "$SOURCE/internal/provider/all/firecracker_test.go"

# These are provider-only dependencies and are intentionally added to the
# generated Juju module rather than this repository's module graph.
(cd "$SOURCE" && go mod edit \
  -require=github.com/containernetworking/cni@v1.2.3 \
  -require=github.com/firecracker-microvm/firecracker-go-sdk@v1.0.0)
(cd "$SOURCE" && go mod tidy)

mkdir -p "$OUTPUT/bin"
if [ "$TARGET_GOOS" = "$(go env GOOS)" ] && [ "$TARGET_GOARCH" = "$(go env GOARCH)" ] && [ "${BUILD_ONLY:-0}" != 1 ]; then
  (cd "$SOURCE" && go test ./internal/provider/all)
else
  printf '%s\n' "skipping target-only tests for $TARGET_GOOS/$TARGET_GOARCH"
fi
(cd "$SOURCE" && GOOS="$TARGET_GOOS" GOARCH="$TARGET_GOARCH" go build -trimpath -o "$OUTPUT/bin/juju" ./cmd/juju)
(cd "$SOURCE" && GOOS="$TARGET_GOOS" GOARCH="$TARGET_GOARCH" go build -trimpath -o "$OUTPUT/bin/jujud" ./cmd/jujud)

cat >"$OUTPUT/BUILD-INFO" <<EOF
juju_module=github.com/juju/juju
juju_version=$JUJU_VERSION
juju_commit=$JUJU_HASH
provider=firecracker
registration=github.com/juju/juju/internal/provider/all
binaries=bin/juju,bin/jujud
goos=$TARGET_GOOS
goarch=$TARGET_GOARCH
EOF
printf '%s\n' "built $OUTPUT/bin/juju and $OUTPUT/bin/jujud"