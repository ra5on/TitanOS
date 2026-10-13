#!/bin/sh
# Boots the stick contents (out/stick) in QEMU: UEFI firmware (OVMF), the
# stick as an emulated USB drive with one FAT32 partition labelled TITANOS.
#
# Usage: scripts/run-qemu.sh [extra qemu arguments]
#
#   MEM=4G        RAM of the VM
#   DISKS=0       number of empty data disks (test/disks/), for RAID tests
#   DISK_SIZE=8G  size of each new data disk
#   HTTPS_PORT=8443  host port forwarded to the web UI: https://127.0.0.1:8443
#   SMB_PORT=8445    host port forwarded to SMB: smbclient -p 8445 //127.0.0.1/<share>
#   FRESH=1       recreate the stick image, discarding /config
#   GRAPHIC=1     open a window instead of using the terminal as serial console
#
# The serial console is this terminal. Leave with Ctrl+A, then X.
set -eu

cd "$(dirname "$0")/.."

MEM=${MEM:-4G}
DISKS=${DISKS:-0}
DISK_SIZE=${DISK_SIZE:-8G}
HTTPS_PORT=${HTTPS_PORT:-8443}
SMB_PORT=${SMB_PORT:-8445}

stick=out/stick
image=test/stick.img
ovmf_code=/usr/share/OVMF/OVMF_CODE_4M.fd
ovmf_vars=/usr/share/OVMF/OVMF_VARS_4M.fd
PATH=$PATH:/usr/sbin:/sbin

if [ ! -f "$stick/titanos/a/filesystem.squashfs" ]; then
    echo "$stick is missing, run 'make zip' or 'make zip-dev' first" >&2
    exit 1
fi
for file in "$ovmf_code" "$ovmf_vars"; do
    if [ ! -f "$file" ]; then
        echo "$file is missing, install the package 'ovmf'" >&2
        exit 1
    fi
done

mkdir -p test/disks

# The stick image is kept between runs so that /config survives, like on a
# real stick. A newer build replaces the system files and keeps /config.
partition="$image@@1M"
if [ -n "${FRESH:-}" ] || [ ! -f "$image" ]; then
    rm -f "$image"
    truncate -s 2G "$image"
    # One FAT32 partition in an MBR partition table, like a freshly formatted stick.
    echo 'start=1MiB, type=c, bootable' | sfdisk --quiet --label dos "$image"
    mformat -i "$partition" -F -v TITANOS ::
    mcopy -i "$partition" -s "$stick"/* ::
elif [ "$stick/titanos/a/filesystem.squashfs" -nt "$image" ]; then
    mcopy -i "$partition" -s -o "$stick/EFI" "$stick/loader" "$stick/titanos" ::
    touch "$image"
fi

[ -f test/OVMF_VARS.fd ] || cp "$ovmf_vars" test/OVMF_VARS.fd

if [ -w /dev/kvm ]; then
    accel="-accel kvm -cpu host"
else
    echo "no access to /dev/kvm, falling back to slow emulation" >&2
    accel="-accel tcg -cpu max"
fi

if [ -n "${GRAPHIC:-}" ]; then
    display="-serial mon:stdio"
else
    display="-nographic"
fi

set --  "$@"
i=1
while [ "$i" -le "$DISKS" ]; do
    disk=test/disks/disk$i.qcow2
    [ -f "$disk" ] || qemu-img create -q -f qcow2 "$disk" "$DISK_SIZE"
    set -- "$@" \
        -drive "if=none,id=disk$i,format=qcow2,file=$disk" \
        -device "scsi-hd,drive=disk$i,serial=TITANTEST$i"
    i=$((i + 1))
done

# The kernel only talks to the serial port because QEMU asks for it here (via
# SMBIOS, read by systemd-boot). The stick stays exactly as it is released.
# shellcheck disable=SC2086
exec qemu-system-x86_64 \
    -machine q35 $accel -smp 2 -m "$MEM" \
    -drive "if=pflash,format=raw,readonly=on,file=$ovmf_code" \
    -drive "if=pflash,format=raw,file=test/OVMF_VARS.fd" \
    -device qemu-xhci \
    -drive "if=none,id=stick,format=raw,file=$image" \
    -device usb-storage,drive=stick,removable=on,bootindex=0 \
    -device virtio-scsi-pci \
    -netdev "user,id=net0,hostfwd=tcp:127.0.0.1:$HTTPS_PORT-:443,hostfwd=tcp:127.0.0.1:$SMB_PORT-:445" \
    -device virtio-net-pci,netdev=net0 \
    -smbios "type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200" \
    $display \
    "$@"
