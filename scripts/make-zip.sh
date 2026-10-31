#!/usr/bin/env bash
# Builds the system image with mkosi and packs the contents of the USB stick:
#
#   out/stick/                 the stick layout, ready to copy onto a FAT32 stick
#   out/titanos-<version>.zip  the same as a zip, plus its SHA-256
#   out/titanos-<version>.img  with --image: the stick as a disk image that
#                              also starts on boards with a legacy BIOS
#
# Usage: scripts/make-zip.sh [--dev] [--image | --kernel-new]
#   --dev         development build: root autologin on console and serial port
#   --image       also write the stick image (scripts/make-stick-image.py)
#   --kernel-new  the system with the newer kernel from trixie-backports
#                 (kernel choice "Neu", docs/KERNEL.md): built into
#                 build/mkosi.output-new, its slot files go to out/kernel-new/;
#                 no stick. Build it next to the default image of a release.
#
# Expects backend/bin/titand to exist (make build). VERSION sets the version.
set -euo pipefail

cd "$(dirname "$0")/.."

profile=
kernel_new=
stick_image=
for arg in "$@"; do
    case "$arg" in
        --dev) profile=dev ;;
        --kernel-new) kernel_new=1 ;;
        --image) stick_image=1 ;;
        *) echo "usage: $0 [--dev] [--image | --kernel-new]" >&2; exit 2 ;;
    esac
done
profiles=()
[ -n "$profile" ] && profiles+=(--profile "$profile")
output=mkosi.output
if [ -n "$kernel_new" ]; then
    profiles+=(--profile kernel-new)
    output=mkosi.output-new
fi

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
install -D -m 0755 backend/bin/titanosctl "$extra/usr/bin/titanosctl"
# The licenses are shown under Settings > About.
install -D -m 0644 LICENSE.md "$extra/usr/share/titanos/LICENSE.md"
python3 scripts/third-party-licenses.py application "$extra/usr/share/titanos"
# The finalize hook runs in mkosi's host sandbox, with no access to this checkout.
install -m 0644 scripts/third-party-licenses.py "$extra/usr/share/titanos/build-licenses.py"
install -m 0644 scripts/normalize-image-tmpfiles.py "$extra/usr/share/titanos/normalize-image-tmpfiles.py"
install -m 0644 scripts/check-image-hardware.py "$extra/usr/share/titanos/check-image-hardware.py"
# GRUB for boards without 64-bit UEFI is built inside the image (postinst).
install -m 0755 scripts/make-grub.sh "$extra/usr/share/titanos/make-grub.sh"
install -m 0644 boot/grub-early.cfg "$extra/usr/share/titanos/grub-early.cfg"
# Catalog descriptions and hardware rules of the official plugins: the NAS
# recommends a plugin for its hardware without asking GitHub first.
install -d "$extra/usr/share/titanos/plugin-catalog"
install -m 0644 plugins/catalog/*.json "$extra/usr/share/titanos/plugin-catalog/"

# Without root, mkosi would map everything to a single user and every file in
# the image would end up owned by root. A user namespace with the full subuid
# range (/etc/subuid) keeps the real owners.
userns=
if [ "${MKOSI_SUDO:-}" = 1 ]; then
    userns="sudo --preserve-env=PATH"
elif [ "$(id -u)" -ne 0 ]; then
    userns="unshare --user --map-auto --map-root-user"
fi

# Reproducible image: file and squashfs timestamps come from the commit, not
# from the clock (mkosi clamps the tree and hands SOURCE_DATE_EPOCH on to
# mkosi.finalize and mksquashfs). Passed as an option, which sudo keeps.
epoch=${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || true)}

# sudo may resolve commands through its secure_path even with PATH preserved.
mkosi_bin=$(command -v mkosi)
$userns "$mkosi_bin" --directory build \
    --image-version "$version" \
    ${epoch:+--source-date-epoch "$epoch"} \
    "${profiles[@]}" \
    --output-directory "$output" \
    --extra-tree "$extra" \
    --force build

image=build/$output

if [ -n "$kernel_new" ]; then
    kernel=$(cat "$image/kernel-version")
    case "$kernel" in
        *bpo*) ;;
        *) echo "the image with the new kernel holds $kernel, not a backports kernel" >&2; exit 1 ;;
    esac
    rm -rf out/kernel-new
    mkdir -p out/kernel-new
    cp "$image/vmlinuz" "$image/initrd.img" "$image/filesystem.squashfs" "$image/kernel-version" out/kernel-new/
    echo "$version" > out/kernel-new/version
    echo
    echo "Kernel (new): $kernel"
    echo "Slot files:   out/kernel-new/"
    exit 0
fi

stick=out/stick

rm -rf "$stick"
mkdir -p "$stick/EFI/BOOT" "$stick/loader/entries" "$stick/titanos/$slot" "$stick/config"
cp "$image/BOOTX64.EFI" "$stick/EFI/BOOT/BOOTX64.EFI"
cp boot/loader.conf "$stick/loader/loader.conf"
sed -e "s|@VERSION@|$version|g" -e "s|@SLOT@|$slot|g" boot/entry.conf.in \
    > "$stick/loader/entries/titanos-$slot.conf"
# Boards without 64-bit UEFI: GRUB for 32-bit UEFI (BOOTIA32.EFI) and for a
# legacy BIOS (boot.img/core.img, written behind the MBR by stick-bios.py or
# in the stick image). Both read boot/grub/grub.cfg and follow the same A/B
# entries as systemd-boot; updates never replace these files.
cp "$image/BOOTIA32.EFI" "$stick/EFI/BOOT/BOOTIA32.EFI"
mkdir -p "$stick/boot/grub/i386-pc"
cp "$image/grub/i386-pc/boot.img" "$image/grub/i386-pc/core.img" "$stick/boot/grub/i386-pc/"
# GRUB itself replaces $1 with the slot, so it stays literal here.
# shellcheck disable=SC2016
options=$(sed -n 's/^options[[:space:]]*//p' boot/entry.conf.in | sed 's|@SLOT@|$1|g')
sed "s|@OPTIONS@|$options|" boot/grub.cfg.in > "$stick/boot/grub/grub.cfg"
python3 scripts/grubenv.py "$stick/boot/grub/grubenv" titanos_prefer=$slot
install -m 0644 scripts/stick-bios.py "$stick/boot/grub/stick-bios.py"
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
if [ -n "$stick_image" ]; then
    rm -f "out/$name.img"
    python3 scripts/make-stick-image.py "$stick" "out/$name.img"
    echo "Stick image:  out/$name.img (legacy BIOS and UEFI)"
fi
