#!/usr/bin/env bash
# Fedora-only test adapter: private hostname, Debian OVMF paths and SSH config.
# No host mounts, hostname, SSH configuration or network settings are changed.
set -euo pipefail

if [[ ${1:-} != --inside ]]; then
    if [[ $# == 0 ]]; then
        echo "usage: $0 command [args...]" >&2
        exit 2
    fi
    if [[ $(id -u) == 0 ]]; then
        echo 'Run this helper as your normal user, without sudo.' >&2
        exit 2
    fi
    exec unshare --user --map-root-user --mount --uts \
        "$(realpath "$0")" --inside "$@"
fi
shift
if [[ $# == 0 || $(readlink /proc/self/ns/mnt) == $(readlink /proc/1/ns/mnt) ]]; then
    echo 'A private mount namespace and a command are required.' >&2
    exit 2
fi

mount --make-rprivate /
hostname titanos-dev
scratch=$(mktemp -d "${TMPDIR:-/tmp}/titanos-fedora.XXXXXXXX")
mounted_share=0
mounted_original=0
cleanup() {
    if [[ $mounted_share == 1 ]]; then umount /usr/share; fi
    if [[ $mounted_original == 1 ]]; then umount "$scratch/original"; fi
    rm -rf -- "$scratch"
}
trap cleanup EXIT
mkdir "$scratch/original" "$scratch/view" "$scratch/bin"
mount --bind /usr/share "$scratch/original"
mounted_original=1
for entry in "$scratch/original"/*; do
    name=${entry##*/}
    if [[ $name != OVMF ]]; then ln -s "$entry" "$scratch/view/$name"; fi
done
mkdir "$scratch/view/OVMF"
for kind in CODE VARS; do
    for suffix in '' .ms; do
        target="OVMF_${kind}_4M${suffix}.fd"
        if [[ -f "$scratch/original/OVMF/$target" ]]; then
            cp "$scratch/original/OVMF/$target" "$scratch/view/OVMF/$target"
        else
            source_suffix=
            if [[ $suffix == .ms ]]; then source_suffix=.secboot; fi
            source="$scratch/original/edk2/ovmf/OVMF_${kind}_4M${source_suffix}.qcow2"
            if [[ ! -f $source ]]; then
                echo "Missing OVMF firmware: $source" >&2
                exit 1
            fi
            qemu-img convert -f qcow2 -O raw "$source" "$scratch/view/OVMF/$target"
        fi
    done
done
mount --bind "$scratch/view" /usr/share
mounted_share=1

# Host-root SSH snippets have an unmapped owner in this user namespace.
# Keep explicit per-test keys/known_hosts; skip only the host's client config.
for tool in ssh scp; do
    printf '#!/bin/sh\nexec /usr/bin/%s -F /dev/null "$@"\n' "$tool" > "$scratch/bin/$tool"
    chmod 755 "$scratch/bin/$tool"
done
export PATH="$scratch/bin:$PATH"
"$@"
