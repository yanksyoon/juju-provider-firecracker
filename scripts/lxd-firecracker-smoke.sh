#!/bin/sh
# Preflight and opt-in smoke test for a disposable LXD guest.
# The provider and Firecracker execute in the guest; LXD is only the boundary.
set -eu

usage() {
    printf '%s\n' "usage: $0 --preflight | --smoke"
    printf '%s\n' "  --preflight  check host and guest prerequisites without changing state"
    printf '%s\n' "  --smoke      create, exercise, and delete one disposable LXD guest"
}

failures=""
require_command() {
    if ! command -v "$1" >/dev/null 2>&1; then
        failures="${failures}\n- missing command: $1"
    fi
}
require_path() {
    if [ ! -e "$1" ]; then
        failures="${failures}\n- missing $2: $1"
    fi
}

preflight() {
    failures=""
    [ "$(uname -s)" = Linux ] || failures="${failures}\n- Linux host is required"
    require_command lxc
    require_command juju
    require_command firecracker
    require_path /dev/kvm KVM device

    cni_config=${JUJU_FC_LXD_CNI_CONFIG:-/etc/cni/net.d/juju-fc.conflist}
    cni_path=${CNI_PATH:-/opt/cni/bin}
    require_path "$cni_config" CNI configuration
    old_ifs=$IFS
    IFS=:
    for plugin in bridge host-local; do
        found=0
        for dir in $cni_path; do
            if [ -x "$dir/$plugin" ]; then found=1; break; fi
        done
        [ "$found" -eq 1 ] || failures="${failures}\n- missing executable CNI plugin $plugin in $cni_path"
    done
    IFS=$old_ifs

    if command -v firecracker >/dev/null 2>&1; then
        firecracker --version >/dev/null 2>&1 || failures="${failures}\n- Firecracker is not executable: $(command -v firecracker)"
    fi
    if command -v lxc >/dev/null 2>&1; then
        lxc info >/dev/null 2>&1 || failures="${failures}\n- LXD daemon is unavailable or not accessible"
    fi
    if command -v juju >/dev/null 2>&1; then
        juju version >/dev/null 2>&1 || failures="${failures}\n- Juju CLI is not executable"
    fi

    if [ -n "$failures" ]; then
        printf '%b\n' "LXD/Firecracker preflight failed:$failures" >&2
        printf '%s\n' "No LXD, Juju, CNI, or Firecracker resources were modified." >&2
        return 1
    fi
    printf '%s\n' "LXD/Firecracker preflight passed (host prerequisites only)."
}

smoke() {
    [ "${JUJU_FC_LXD_RUN:-}" = 1 ] || {
        printf '%s\n' "refusing smoke test: set JUJU_FC_LXD_RUN=1 explicitly" >&2
        return 2
    }
    preflight
    provider=${JUJU_FC_PROVIDER_BINARY:-}
    case "$provider" in
        /*) [ -x "$provider" ] || { printf '%s\n' "provider binary is not executable: $provider" >&2; return 1; } ;;
        *) printf '%s\n' "JUJU_FC_PROVIDER_BINARY must be an executable absolute path" >&2; return 1 ;;
    esac

    name=${JUJU_FC_LXD_NAME:-juju-fc-smoke-$(date +%s)-$$}
    case "$name" in *[!A-Za-z0-9-]*) printf '%s\n' "invalid disposable LXD name: $name" >&2; return 1 ;; esac
    image=${JUJU_FC_LXD_IMAGE:-images:ubuntu/24.04}
    created=0
    cleanup() {
        if [ "$created" -eq 1 ]; then
            if ! lxc delete --force "$name" >/dev/null 2>&1; then
                printf '%s\n' "failed to delete disposable LXD guest $name" >&2
                exit 1
            fi
        fi
    }
    trap cleanup EXIT HUP INT TERM

    lxc launch "$image" "$name"
    created=1
    lxc config set "$name" security.nesting true
    lxc config set "$name" security.privileged true
    lxc config device add "$name" kvm unix-char path=/dev/kvm
    lxc exec "$name" -- mkdir -p /usr/local/bin
    lxc file push "$provider" "$name/usr/local/bin/juju-firecracker"
    lxc exec "$name" -- chmod 0755 /usr/local/bin/juju-firecracker

    # Registration is the supported Juju boundary currently exercised here;
    # no controller, model, or production cloud is contacted by this smoke.
    lxc exec "$name" -- env PATH=/usr/local/bin:/usr/bin:/bin juju version >/dev/null
    providers=$(lxc exec "$name" -- env PATH=/usr/local/bin:/usr/bin:/bin juju providers 2>&1) || {
        printf '%s\n' "$providers" >&2
        printf '%s\n' "Juju provider discovery failed inside disposable guest $name" >&2
        return 1
    }
    printf '%s\n' "$providers" | grep -E '(^|[[:space:]])firecracker([[:space:]]|$)' >/dev/null || {
        printf '%s\n' "firecracker provider was not registered inside disposable guest $name" >&2
        return 1
    }

    # The lifecycle boundary is deliberately limited to LXD create/exec/delete.
    # A real Firecracker VM requires guest kernel/rootfs and a guest CNI setup;
    # those are separate, explicit inputs and are never guessed here.
    printf '%s\n' "LXD smoke passed: provider registration and guest lifecycle verified for $name"
}

[ "$#" -eq 1 ] || { usage >&2; exit 2; }
case "$1" in
    --preflight) preflight ;;
    --smoke) smoke ;;
    *) usage >&2; exit 2 ;;
esac
