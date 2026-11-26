#!/usr/bin/env bash
# Inspect a fresh FAT32 USB image immediately after a confirmed Config.Write.
# KERNEL and MODULES are a matching kernel and its kernel/ modules directory.
# Optional CONFIG_FAT_BASELINE is a git revision for a before/after comparison.
set -euo pipefail
export LC_ALL=C
kernel=${1:?kernel image required}
modules=${2:?kernel modules directory required}
backend=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
init="$work/init"
mkdir -p "$init/bin" "$init/lib" "$init/proc" "$init/sys" "$init/dev" "$init/stick"
cp "$(command -v busybox)" "$init/bin/busybox"
# The Debian release initrd stores these modules compressed, in dependency order below.
for module in usb-common usbcore scsi_common scsi_mod sd_mod xhci-hcd xhci-pci usb-storage fat vfat nls_cp437 nls_iso8859-1 nls_ascii nls_utf8; do
    src=$(find "$modules" -name "$module.ko*" -print -quit)
    [ -n "$src" ] || continue # built into some kernels
    case "$src" in
        *.xz) xz -dc "$src" > "$init/lib/$module.ko";;
        *.zst) zstd -qdc "$src" > "$init/lib/$module.ko";;
        *) cp "$src" "$init/lib/$module.ko";;
    esac
    echo "/bin/busybox insmod /lib/$module.ko" >> "$init/load-modules"
done
cat > "$init/init" <<'GUEST'
#!/bin/busybox sh
/bin/busybox --install -s /bin
export PATH=/bin
set -eu
mount -t proc proc /proc
mount -t sysfs sys /sys
mount -t devtmpfs dev /dev
sh /load-modules
for n in $(seq 1 120); do [ -b /dev/sda ] && break; sleep 0.1; done
if ! mount -t vfat -o flush,umask=0077 /dev/sda /stick; then dmesg | tail -60; poweroff -f; exit 1; fi
grep /stick /proc/mounts
/bin/config.test -test.v -test.run='^TestGuestFATStorePowerCut$'
poweroff -f
GUEST
chmod +x "$init/init"
cat > "$work/kill-on-ack.py" <<'PY'
import subprocess, sys
with subprocess.Popen(sys.argv[1:],stdout=subprocess.PIPE,stderr=subprocess.STDOUT) as q:
    try:
        for raw in q.stdout:
            line=raw.decode(errors="replace").strip()
            if line=="TITANOS_FAT_STORE_ACKNOWLEDGED":
                q.kill()
                q.wait()
                print("Killed guest after confirmed write.",flush=True)
                break
            if "Kernel panic" in line or "--- FAIL" in line or " /stick " in line or "FAT-fs" in line or "insmod:" in line:
                print(line,flush=True)
        else:
            raise SystemExit("guest ended before confirming the write")
    finally:
        if q.poll() is None:
            q.kill()
            q.wait()
PY
# Prepopulate unrelated config and any replaced file before the guest mounts FAT.
echo '{"name":"Documents","writable":true}' > "$work/share.json"
echo '{"channel":"stable"}' > "$work/update.json"
echo '{"volume":"previous","generation":1}' > "$work/previous.json"
modes=(current)
if [ -n "${CONFIG_FAT_BASELINE:-}" ]; then
    (cd "$backend" && git show "$CONFIG_FAT_BASELINE:backend/internal/config/store.go") > "$work/before.go"
    python3 -c 'import json,sys; json.dump({"Replace":{sys.argv[1]:sys.argv[2]}},sys.stdout)' "$backend/internal/config/store.go" "$work/before.go" > "$work/overlay.json"
    modes=(baseline current)
fi
for mode in "${modes[@]}"; do
    flags=()
    if [ "$mode" = baseline ]; then flags+=("-overlay=$work/overlay.json"); fi
    (cd "$backend" && CGO_ENABLED=0 go test "${flags[@]}" -c -ldflags='-s -w' -o "$init/bin/config.test" ./internal/config)
    (cd "$init" && find . -print0 | cpio -o --null -H newc --quiet | gzip -1) > "$work/guest.gz"
    for prior in new replace nested; do
        for stop in immediate partial-update; do
            if [ "$prior" = nested ] && [ "$stop" = partial-update ]; then continue; fi
            disk="$work/$mode-$prior-$stop.img"
            truncate -s 384M "$disk"
            mkfs.fat -F 32 -s 8 "$disk" > /dev/null
            mmd -i "$disk" ::/config ::/config/system ::/config/shares ::/titanos ::/titanos/.incoming-b
            mcopy -i "$disk" "$work/share.json" ::/config/shares/Documents.json
            mcopy -i "$disk" "$work/update.json" ::/config/system/update.json
            if [ "$prior" = replace ]; then mcopy -i "$disk" "$work/previous.json" ::/config/system/agent.json; fi
            arg=""
            if [ "$stop" = partial-update ]; then arg="titanos.update-part=1"; fi
            path="::/config/system/agent.json"
            if [ "$prior" = nested ]; then arg="titanos.nested=1"; path="::/config/system/new/node/agent.json"; fi
            echo "Mode=$mode previous=$prior cut=$stop"
            timeout 90s python3 "$work/kill-on-ack.py" qemu-system-x86_64 -machine q35 -accel tcg,thread=multi -m 320 -smp 2 -display none -nic none -serial stdio -monitor none -no-reboot \
                -kernel "$kernel" -initrd "$work/guest.gz" -append "console=ttyS0 rdinit=/init quiet panic=-1 titanos.fat-store-test=1 $arg" \
                -device qemu-xhci -drive "if=none,id=stick,format=raw,file=$disk" -device usb-storage,drive=stick,removable=on
            mcopy -i "$disk" ::/config/shares/Documents.json - | cmp "$work/share.json" -
            mcopy -i "$disk" ::/config/system/update.json - | cmp "$work/update.json" -
            # The dirty bit itself is expected. Inspect allocation before repairing anything.
            fsck_result=0
            fsck.fat -n "$disk" > "$work/fsck.log" || fsck_result=$?
            if [ "$fsck_result" -gt 1 ]; then cat "$work/fsck.log"; exit "$fsck_result"; fi
            python3 - "$mode" "$disk" "$work/fsck.log" "$path" <<'PY'
import json,subprocess,sys
mode,disk,log,path=sys.argv[1:]
proc=subprocess.run(["mcopy","-i",disk,path,"-"],capture_output=True)
expected=(json.dumps({"volume":"testdaten","generation":2,"padding":"x"*280},indent=2)+"\n").encode()
fsck=open(log).read()
lost="unused cluster" in fsck
valid=proc.returncode==0 and proc.stdout==expected
print(f"agent.json present={proc.returncode==0}; {len(proc.stdout)} bytes; exact contents={valid}; unused cluster={lost}",flush=True)
if mode=="current" and (not valid or lost): raise SystemExit("FAT checkpoint was not durable")
PY
            rm -- "$disk"
        done
    done
done
