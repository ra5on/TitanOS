#!/usr/bin/env bash
# Sign an already packed release. Only the protected publish job gets the key.
# Locally minisign asks for the password; CI sets MINISIGN_PASSWORD.
set -euo pipefail
release=${1:?usage: sign-release.sh <release directory> <secret key>}
key=${2:?usage: sign-release.sh <release directory> <secret key>}
version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$release/manifest.json")
for path in "$release/manifest.json" "$release/titanos-$version.zip" "$release/SHA256SUMS" "$release/"*.plugin.json; do
    # An image-only rehearsal has no plugins, so the pattern stays unexpanded.
    case $path in *'*'*) continue ;; esac
    name=${path##*/}
    if [ "${MINISIGN_PASSWORD+x}" = x ]; then
        printf '%s\n' "$MINISIGN_PASSWORD" | minisign -S -s "$key" -m "$release/$name" -t "TitanOS $version: $name"
    else
        minisign -S -s "$key" -m "$release/$name" -t "TitanOS $version: $name"
    fi
done
