#!/usr/bin/env bash
# Run from any directory on an x86_64 Linux development host.
# Usage: file-backup-disks.sh KERNEL KERNEL_RELEASE RESTIC
# The kernel's virtio-blk, ext4 and xHCI drivers must be built in. Other
# drivers come from /lib/modules/KERNEL_RELEASE; busybox must be static.
set -euo pipefail
kernel=$(realpath "${1:?supply a readable Linux kernel}")
release=${2:?supply its matching module version}
restic=$(realpath "${3:?supply restic 0.18 or newer}")
backend=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
init="$work/init"
mkdir -p "$init"/{bin,dev,proc,sys,tmp,root,modules,move-qa/{source,internal,backup,usb}}
cp "$(command -v busybox)" "$init/bin/busybox"
cp "$restic" "$init/bin/restic"
# A distribution restic can be dynamically linked. Include its actual loader
# and libraries; the two Go test executables are built without cgo below.
while read -r lib; do
    install -D -m 755 "$lib" "$init$lib"
done < <(ldd "$restic" 2>/dev/null | awk '{ for(i=1;i<=NF;i++) if($i ~ /^\//) print $i }')
for module in usb_storage exfat nls_utf8; do
    modprobe -S "$release" --show-depends "$module"
done | awk '/^insmod / && !seen[$2]++ {print $2}' > "$work/modules"
while read -r module; do
    name=$(basename "${module%.zst}")
    name=${name%.xz}
    case "$module" in
        *.zst) zstd -dcq "$module" > "$init/modules/$name" ;;
        *.xz) xz -dc "$module" > "$init/modules/$name" ;;
        *) cp "$module" "$init/modules/$name" ;;
    esac
    echo "/bin/busybox insmod /modules/$name"
done < "$work/modules" > "$init/load-modules"
for name in files backup; do
    (cd "$backend" && CGO_ENABLED=0 go test -c -ldflags='-s -w' -o "$init/bin/$name.test" "./internal/$name")
done
for device in source internal backup usb; do
    truncate -s 128M "$work/$device.img"
done
for device in source internal backup; do
    mkfs.ext4 -q -F "$work/$device.img"
done
mkfs.exfat "$work/usb.img"
cat > "$init/init" <<'GUEST'
#!/bin/busybox sh
/bin/busybox --install -s /bin
export PATH=/bin GOMAXPROCS=2
trap 'poweroff -f' EXIT
set -eu
mount -t proc proc /proc
mount -t devtmpfs dev /dev
mount -t sysfs sys /sys
mount -t tmpfs -o size=700m tmp /tmp
sh /load-modules
for i in $(seq 1 60); do [ -b /dev/sda ] && break; sleep 1; done
mount -t ext4 /dev/vda /move-qa/source
mount -t ext4 /dev/vdb /move-qa/internal
mount -t ext4 /dev/vdc /move-qa/backup
mount -t exfat /dev/sda /move-qa/usb
grep /move-qa /proc/mounts
for arg in $(cat /proc/cmdline); do
    case "$arg" in
        MOVE_CUT_PHASE=*|MOVE_CUT_KIND=*|MOVE_CUT_POINT=*) export "$arg" ;;
    esac
done
cut_worker() {
    if [ "${MOVE_CUT_POINT:-}" = readable-copy ]; then
        /bin/backup.test -test.v -test.run='^TestGuestUndatedRecoveryAcrossPowerCut$'
    else
        /bin/files.test -test.v -test.run='^TestGuestMoveAcrossPowerCut$'
    fi
}
case "${MOVE_CUT_PHASE:-}" in
    arm)
        cut_worker &
        worker=$!
        while [ ! -e /tmp/reset-guest ]; do
            kill -0 "$worker" || exit 1
            sleep 0.1
        done
        echo NORDLICHT_RESETTING_GUEST
        # Immediate guest reset, without sync or unmount. -no-reboot ends QEMU.
        echo b > /proc/sysrq-trigger
        exit 1 ;;
    check) cut_worker ;;
    '') /bin/files.test -test.v -test.run='^TestMove(PublicationBarriers|CrashAtPublication|KeepsSourceIfPublishedTargetIsReplaced)$'
        RESTIC_TEST_BINARY=/bin/restic /bin/backup.test -test.v -test.timeout=25m -test.run='^Test(PreviousSnapshotRestoresAfterFailure|UndatedRecoveryKeepsMatchingPair)$' ;;
esac
sync
echo NORDLICHT_DISK_TESTS_PASSED
GUEST
chmod +x "$init/init"
(cd "$init" && find . -print0 | cpio --null -o -H newc --quiet | gzip -1) > "$work/init.gz"
boot() {
    local expected=$1
    shift
    timeout 30m qemu-system-x86_64 -accel tcg,thread=multi -machine q35 -m 1600 -smp 2 \
        -display none -nic none -serial stdio -monitor none -no-reboot \
        -kernel "$kernel" -append "console=ttyS0 rdinit=/init quiet panic=-1 titanos.move-test=1 $*" -initrd "$work/init.gz" \
        -drive "file=$work/source.img,if=virtio,format=raw" -drive "file=$work/internal.img,if=virtio,format=raw" \
        -drive "file=$work/backup.img,if=virtio,format=raw" -device qemu-xhci,id=usbh \
        -drive "file=$work/usb.img,format=raw,if=none,id=removable" -device usb-storage,bus=usbh.0,drive=removable | tee "$work/serial"
    grep -q "$expected" "$work/serial"
}
boot NORDLICHT_DISK_TESTS_PASSED
for kind in internal usb; do
    for point in before-target-sync after-source-unlink readable-copy; do
        boot NORDLICHT_RESETTING_GUEST MOVE_CUT_PHASE=arm "MOVE_CUT_KIND=$kind" "MOVE_CUT_POINT=$point"
        boot NORDLICHT_DISK_TESTS_PASSED MOVE_CUT_PHASE=check "MOVE_CUT_KIND=$kind" "MOVE_CUT_POINT=$point"
    done
done
