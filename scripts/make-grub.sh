#!/bin/sh
# Builds the GRUB images for boards without 64-bit UEFI:
#
#   <out>/BOOTIA32.EFI       32-bit UEFI (e.g. Bay Trail), goes to EFI/BOOT/
#   <out>/i386-pc/boot.img   legacy BIOS: the boot code for the MBR
#   <out>/i386-pc/core.img   legacy BIOS: GRUB itself, behind the MBR
#
# Both carry every module they need and boot/grub-early.cfg; the menu and the
# A/B rules come from /boot/grub/grub.cfg on the stick.
#
# Usage: make-grub.sh <early.cfg> <out> [grub library dir, default /usr/lib/grub]
# Runs in the image build (mkosi.postinst.chroot) with Debian's grub-pc-bin
# and grub-efi-ia32-bin, or on a development host with any grub-mkimage 2.12.
set -eu

early=$1
out=$2
lib=${3:-/usr/lib/grub}

common="part_msdos fat search search_label normal configfile linux loadenv test echo boot ls cat sleep reboot halt"
mkdir -p "$out/i386-pc"

grub-mkimage --directory "$lib/i386-pc" --format i386-pc --compression auto \
    --config "$early" --prefix /boot/grub --output "$out/i386-pc/core.img" \
    biosdisk $common
cp "$lib/i386-pc/boot.img" "$out/i386-pc/boot.img"

grub-mkimage --directory "$lib/i386-efi" --format i386-efi \
    --config "$early" --prefix /boot/grub --output "$out/BOOTIA32.EFI" \
    efi_gop efi_uga $common

# The BIOS core must fit between the MBR and a partition at 1 MiB.
size=$(wc -c < "$out/i386-pc/core.img")
if [ "$size" -gt $((1024 * 1024 - 1024)) ]; then
    echo "core.img is too large for the gap before the first partition: $size bytes" >&2
    exit 1
fi
