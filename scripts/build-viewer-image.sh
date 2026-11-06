#!/usr/bin/env bash
# Build the titanos-viewer container image (TitanOS remote view,
# docs/FERNANSICHT.md) and export it for the release.
#
#   scripts/build-viewer-image.sh <version> <out-dir>
#
# Writes <out-dir>/titanos-viewer-<version>.tar.zst (docker save | zstd)
# and <out-dir>/titanos-viewer-<version>.packages.txt ("name version
# source source-version" of every Debian package in the image, for the license
# inventory). Prints the image ID as the last line. The image is tagged
# titanos-viewer:<version>.
#
# Environment: VIEWER_TAG_DEV=1 additionally tags titanos-viewer:dev.
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "usage: $0 <version> <out-dir>" >&2
	exit 2
fi
VERSION=$1
OUT=$2
case "$VERSION" in
*[!0-9A-Za-z.+-]* | "") echo "invalid version: $VERSION" >&2; exit 2 ;;
esac

ROOT=$(cd "$(dirname "$0")/.." && pwd)
CTX=$(mktemp -d)
trap 'rm -rf "$CTX"' EXIT
mkdir -p "$OUT"

echo "== agent" >&2
(cd "$ROOT/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
	-ldflags "-s -w -buildid=" -o "$CTX/titanos-viewer" ./cmd/titanos-viewer)
cp "$ROOT/viewer/Dockerfile" "$ROOT/viewer/pulse.pa" "$ROOT/viewer/dpkg-excludes" "$CTX/"
touch -d @0 "$CTX"/*

echo "== image" >&2
TAG=titanos-viewer:$VERSION
SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-$(git -C "$ROOT" log -1 --format=%ct 2>/dev/null || echo 0)}
export SOURCE_DATE_EPOCH
docker build --network=default --label org.opencontainers.image.version="$VERSION" \
	--label org.opencontainers.image.title=titanos-viewer -t "$TAG" "$CTX" >&2
if [ "${VIEWER_TAG_DEV:-0}" = 1 ]; then
	docker tag "$TAG" titanos-viewer:dev
fi

echo "== export" >&2
docker run --rm --entrypoint cat "$TAG" /usr/share/titanos-viewer/packages.txt >"$OUT/titanos-viewer-$VERSION.packages.txt"
docker save "$TAG" | zstd -q -19 -T0 -f -o "$OUT/titanos-viewer-$VERSION.tar.zst"
ls -l "$OUT/titanos-viewer-$VERSION.tar.zst" >&2
docker image inspect -f '{{.Size}}' "$TAG" | awk '{printf "image size: %.0f MB\n", $1/1000000}' >&2
docker image inspect -f '{{.Id}}' "$TAG"
