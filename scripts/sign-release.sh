#!/bin/sh
# Sign an already packed release. Only the protected publish job gets the key.
# Locally minisign asks for the password; CI sets MINISIGN_PASSWORD.
set -eu
release=${1:?usage: sign-release.sh <release directory> <secret key>}
key=${2:?usage: sign-release.sh <release directory> <secret key>}
version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "$release/manifest.json")
for name in manifest.json "titanos-$version.zip" SHA256SUMS; do
    if [ "${MINISIGN_PASSWORD+x}" = x ]; then
        printf '%s\n' "$MINISIGN_PASSWORD" | minisign -S -s "$key" -m "$release/$name" -t "TitanOS $version: $name"
    else
        minisign -S -s "$key" -m "$release/$name" -t "TitanOS $version: $name"
    fi
done
