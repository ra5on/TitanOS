#!/usr/bin/env bash
# Writes the pci.ids pinned in build/pci-ids.lock to the given file, or
# with --update moves the pin to the newest commit of pciutils/pciids.
#
# Usage: scripts/fetch-pci-ids.sh <output-file> | --update
set -euo pipefail

cd "$(dirname "$0")/.."
lock=build/pci-ids.lock
repo=pciutils/pciids

download() { # commit target
    local attempt
    for attempt in 1 2 3 4; do
        if curl -fsSL --max-time 120 -o "$2" "https://raw.githubusercontent.com/$repo/$1/pci.ids"; then
            return 0
        fi
        sleep $((attempt * 2))
    done
    echo "pci.ids of $repo@$1 could not be downloaded" >&2
    return 1
}

if [ "${1:-}" = --update ]; then
    commit=$(curl -fsSL "https://api.github.com/repos/$repo/commits?per_page=1" |
        python3 -c 'import json, sys; print(json.load(sys.stdin)[0]["sha"])')
    tmp=$(mktemp)
    trap 'rm -f "$tmp"' EXIT
    download "$commit" "$tmp"
    sum=$(sha256sum "$tmp" | cut -d' ' -f1)
    sed -i -e "s/^commit=.*/commit=$commit/" -e "s/^sha256=.*/sha256=$sum/" "$lock"
    echo "pci.ids pinned to $commit"
    exit 0
fi
[ $# -eq 1 ] || { echo "usage: $0 <output-file> | --update" >&2; exit 2; }

commit=$(sed -n 's/^commit=//p' "$lock")
sum=$(sed -n 's/^sha256=//p' "$lock")
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
download "$commit" "$tmp"
if ! echo "$sum  $tmp" | sha256sum -c --quiet -; then
    echo "pci.ids of $repo@$commit does not match build/pci-ids.lock" >&2
    exit 1
fi
install -D -m 0644 "$tmp" "$1"
