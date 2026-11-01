#!/usr/bin/env python3
"""Boot an installation ZIP on different virtual hardware.

Each variant starts the real image once in QEMU (UEFI, stick on USB) with
its own network cards, disk controllers and timing, sets up an owner and
checks the hardware report and the console log:

- every network card and disk of the variant is there and has a driver,
- no device is "not supported" or has a failing driver,
- the boot log shows no unexplained firmware, driver, timeout or unit
  problems (scripts/boot_log.py),
- "slow": disks throttled to a few operations per second and a USB disk
  plugged in only after the NAS is up; the disk must show up.

Only newly created, disposable images are attached. QEMU cannot stand in
for real chipsets, GPUs or firmware quirks; snapshots of real machines
(backend/internal/hwcheck/testdata/snapshots) cover those.
"""
import argparse
import http.cookiejar
import json
import os
import secrets
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.request
import zipfile
from pathlib import Path

import boot_log

# name: (devices, disks expected in the report, network cards expected)
VARIANTS = {
    "intel": {
        "about": "Intel e1000e network card, AHCI SATA and NVMe",
        "devices": lambda d: [
            "-device", "e1000e,netdev=net0",
            "-device", "ich9-ahci,id=ahci",
            *disk(d, "sata0", "-device", "ide-hd,drive=sata0,bus=ahci.0"),
            *disk(d, "sata1", "-device", "ide-hd,drive=sata1,bus=ahci.1"),
            *disk(d, "nvme0", "-device", "nvme,drive=nvme0,serial=SMOKENVME"),
        ],
        "disks": 3,
        "nics": {"e1000e"},
    },
    "mixed": {
        "about": "Realtek and Intel network cards side by side, virtio-scsi disks",
        "devices": lambda d: [
            "-device", "rtl8139,netdev=net0",
            "-netdev", "user,id=net1", "-device", "e1000,netdev=net1",
            "-device", "virtio-scsi-pci,id=scsi",
            *disk(d, "scsi0", "-device", "scsi-hd,drive=scsi0,bus=scsi.0"),
            *disk(d, "scsi1", "-device", "scsi-hd,drive=scsi1,bus=scsi.0"),
        ],
        "disks": 2,
        "nics": {"8139cp", "8139too", "e1000"},
    },
    "slow": {
        "about": "disks at 10 operations per second, a USB disk plugged in late",
        "devices": lambda d: [
            "-device", "virtio-net-pci,netdev=net0",
            *disk(d, "slow0", "-device", "virtio-blk-pci,drive=slow0,serial=SLOW0", throttle=True),
            *disk(d, "slow1", "-device", "virtio-blk-pci,drive=slow1,serial=SLOW1", throttle=True),
            *disk(d, "late", None),
        ],
        "disks": 3,
        "nics": {"virtio_net"},
        "late_usb": "late",
    },
}


def disk(directory, name, *device, throttle=False):
    path = directory / f"{name}.qcow2"
    subprocess.run(["qemu-img", "create", "-q", "-f", "qcow2", str(path), "1G"], check=True)
    options = f"if=none,id={name},format=qcow2,file={path}"
    if throttle:
        options += ",throttling.iops-total=10"
    return ["-drive", options, *[part for part in device if part]]


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def qmp(path, *commands):
    """Sends QMP commands over the monitor socket."""
    with socket.socket(socket.AF_UNIX) as sock:
        sock.connect(str(path))
        stream = sock.makefile("rw")
        stream.readline()  # greeting
        for command in [{"execute": "qmp_capabilities"}, *commands]:
            stream.write(json.dumps(command) + "\n")
            stream.flush()
            while True:
                answer = json.loads(stream.readline())
                if "event" in answer:
                    continue
                if "error" in answer:
                    raise RuntimeError(f"QMP {command}: {answer['error']}")
                break


def run_variant(name, variant, archive, timeout, logs):
    context = ssl._create_unverified_context()  # the NAS's own certificate
    with tempfile.TemporaryDirectory(prefix=f"titanos-hw-{name}-") as tmp:
        directory = Path(tmp)
        stick = directory / "stick"
        with zipfile.ZipFile(archive) as packed:
            for item in packed.infolist():
                if not (stick / item.filename).resolve().is_relative_to(stick):
                    raise ValueError("unsafe path in installation ZIP")
            packed.extractall(stick)
        image = directory / "stick.img"
        with image.open("wb") as file:
            file.truncate(4 * 1024**3)
        subprocess.run(["sfdisk", "--quiet", "--label", "dos", str(image)], input=b"start=1MiB, type=c, bootable\n", check=True)
        partition = str(image) + "@@1M"
        subprocess.run(["mformat", "-i", partition, "-F", "-v", "TITANOS", "::"], check=True)
        subprocess.run(["mcopy", "-i", partition, "-s", *map(str, stick.iterdir()), "::"], check=True)
        shutil.copyfile("/usr/share/OVMF/OVMF_VARS_4M.fd", directory / "OVMF_VARS.fd")
        port = free_port()
        monitor = directory / "qmp.sock"
        accel = ["-accel", "kvm", "-cpu", "host"] if os.access("/dev/kvm", os.W_OK) else ["-accel", "tcg", "-cpu", "max"]
        args = ["qemu-system-x86_64", "-machine", "q35", *accel, "-smp", "2", "-m", "4G",
                "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd",
                "-drive", f"if=pflash,format=raw,file={directory}/OVMF_VARS.fd",
                "-device", "qemu-xhci,id=xhci", "-drive", f"if=none,id=stick,format=raw,file={image}",
                "-device", "usb-storage,bus=xhci.0,drive=stick,removable=on,bootindex=0",
                "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443",
                *variant["devices"](directory),
                "-smbios", "type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200",
                "-qmp", f"unix:{monitor},server=on,wait=off",
                "-display", "none", "-serial", "stdio", "-monitor", "none", "-no-reboot"]
        url = f"https://127.0.0.1:{port}"
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
                                            urllib.request.HTTPSHandler(context=context))

        def request(path, data=None, method=None):
            req = urllib.request.Request(url + path, data=json.dumps(data).encode() if data is not None else None,
                                         headers={"Content-Type": "application/json", "Origin": url}, method=method)
            with opener.open(req, timeout=30) as response:
                body = response.read()
                return json.loads(body) if body else None

        log = logs / f"hardware-{name}.log"
        print(f"{name}: {variant['about']} ({accel[1]})", flush=True)
        with log.open("wb") as serial:
            process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
            try:
                deadline = time.monotonic() + timeout
                while True:
                    if process.poll() is not None:
                        raise RuntimeError(f"QEMU exited before the NAS was ready; see {log}")
                    try:
                        if request("/api/v1/health").get("ready"):
                            break
                    except (OSError, ValueError):
                        pass
                    if time.monotonic() > deadline:
                        raise TimeoutError(f"NAS did not become ready; see {log}")
                    time.sleep(3)
                request("/api/v1/setup", {"name": "hardwaretest", "password": secrets.token_urlsafe(24)})
                if variant.get("late_usb"):
                    # Plugged in a while after the start, like a disk switched on late.
                    time.sleep(20)
                    qmp(monitor, {"execute": "device_add", "arguments": {"driver": "usb-storage", "bus": "xhci.0", "drive": variant["late_usb"], "id": "late-usb"}})
                deadline = time.monotonic() + 90
                while True:
                    report = request("/api/v1/hardware/report")
                    disks = report["disks"]  # the variant's disks and the stick
                    if len(disks) >= variant["disks"] + 1 or time.monotonic() > deadline:
                        break
                    time.sleep(3)
                if len(disks) < variant["disks"] + 1:
                    raise AssertionError(f"{name}: disks {[(d['name'], d['transport']) for d in report['disks']]}, expected {variant['disks']} and the stick")
                drivers = {nic["driver"] for nic in report["network"]}
                if not drivers & variant["nics"] or any(not nic["driver"] for nic in report["network"]):
                    raise AssertionError(f"{name}: network cards {report['network']}, expected one of {variant['nics']}")
                bad = [d for d in report["unbound"] if d["hint"] in ("unsupported", "driver_failed")]
                if bad:
                    raise AssertionError(f"{name}: devices without a working driver: {bad}")
                print(f"{name}: {len(report['disks'])} disks, network {sorted(drivers)}, no device without a driver", flush=True)
                request("/api/v1/system/power", {"action": "reboot"})
                process.wait(timeout=120)
            finally:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait()
        boot_log.check(log, emulated=accel[1] == "tcg")
        print(f"{name}: boot log clean", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("zip", type=Path)
    parser.add_argument("--variant", action="append", choices=sorted(VARIANTS), help="only these variants (default: all)")
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--logs", type=Path, default=Path("out"))
    args = parser.parse_args()
    args.logs.mkdir(parents=True, exist_ok=True)
    os.environ["PATH"] += ":/usr/sbin:/sbin"
    for name in args.variant or sorted(VARIANTS):
        run_variant(name, VARIANTS[name], args.zip.resolve(), args.timeout, args.logs)
    print("Hardware variants passed", flush=True)


if __name__ == "__main__":
    main()
