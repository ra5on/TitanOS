#!/bin/sh
# Builds the system image with mkosi and packs the contents of the USB stick:
#
#   out/stick/                 the stick layout, ready to copy onto a FAT32 stick
#   out/titanos-<version>.zip  the same as a zip, plus its SHA-256
#
# Usage: scripts/make-zip.sh [--dev]
#   --dev   development build: root autologin on console and serial port
#
# Expects backend/bin/titand to exist (make build). VERSION sets the version.
set -eu

cd "$(dirname "$0")/.."

profile=
case "${1:-}" in
    --dev) profile=dev ;;
    "") ;;
    *) echo "usage: $0 [--dev]" >&2; exit 2 ;;
esac

version=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 0.0.0)}
name=titanos-$version${profile:+-$profile}
slot=a

if [ ! -x backend/bin/titand ]; then
    echo "backend/bin/titand is missing, run 'make build' first" >&2
    exit 1
fi

# titand is built outside the image and handed to mkosi as an extra tree.
extra=$(mktemp -d)
trap 'rm -rf "$extra"' EXIT
install -D -m 0755 backend/bin/titand "$extra/usr/bin/titand"
# The licenses are shown under Settings > About.
install -D -m 0644 LICENSE.md "$extra/usr/share/titanos/LICENSE.md"
python3 scripts/third-party-licenses.py application "$extra/usr/share/titanos"
# The finalize hook runs in mkosi's host sandbox, with no access to this checkout.
install -m 0644 scripts/third-party-licenses.py "$extra/usr/share/titanos/build-licenses.py"

# Without root, mkosi would map everything to a single user and every file in
# the image would end up owned by root. A user namespace with the full subuid
# range (/etc/subuid) keeps the real owners.
userns=
if [ "${MKOSI_SUDO:-}" = 1 ]; then
    userns="sudo --preserve-env=PATH"
elif [ "$(id -u)" -ne 0 ]; then
    userns="unshare --user --map-auto --map-root-user"
fi

$userns mkosi --directory build \
    --image-version "$version" \
    ${profile:+--profile "$profile"} \
    --extra-tree "$extra" \
    --force build

image=build/mkosi.output
stick=out/stick

rm -rf "$stick"
mkdir -p "$stick/EFI/BOOT" "$stick/loader/entries" "$stick/titanos/$slot" "$stick/config"
cp "$image/BOOTX64.EFI" "$stick/EFI/BOOT/BOOTX64.EFI"
cp boot/loader.conf "$stick/loader/loader.conf"
sed -e "s|@VERSION@|$version|g" -e "s|@SLOT@|$slot|g" boot/entry.conf.in \
    > "$stick/loader/entries/titanos-$slot.conf"
cp "$image/vmlinuz" "$image/initrd.img" "$image/filesystem.squashfs" "$stick/titanos/$slot/"
# The updater reads from this file which version lies in a slot.
echo "$version" > "$stick/titanos/$slot/version"

rm -f "out/$name.zip"
(cd "$stick" && zip -q -r -0 "../$name.zip" .)
(cd out && sha256sum "$name.zip" > "$name.zip.sha256")

echo
echo "Kernel:       $(cat "$image/kernel-version")"
echo "System image: $(du -h "$stick/titanos/$slot/filesystem.squashfs" | cut -f1) (filesystem.squashfs)"
echo "Stick:        $stick"
echo "Zip:          out/$name.zip ($(du -h "out/$name.zip" | cut -f1))"
