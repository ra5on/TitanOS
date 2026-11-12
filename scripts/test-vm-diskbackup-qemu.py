#!/usr/bin/env python3
"""Backup of a running VM's disks against real libvirt, in a disposable NAS.

Boots a built TitanOS image (build/mkosi.output of `make zip`) in QEMU, logs
in over SSH with a key made for this run, copies the Go test binary of
backend/internal/vm into the guest and runs TestRealLibvirtDiskBackup there:
a small VM without an operating system inside the guest, real virsh,
virtqemud and qemu-img. No host disk is attached and nothing on the host is
changed; everything lives in --work, which is removed afterwards.

  cd backend && CGO_ENABLED=0 go test -c -o /tmp/vm.test ./internal/vm/
  python3 scripts/test-vm-diskbackup-qemu.py --image build/mkosi.output --test /tmp/vm.test --work /var/tmp/titanos-vmtest
"""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import ssl
import subprocess
import sys
import time
import urllib.request


def command(*args, **kwargs):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL, **kwargs)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--image", type=Path, required=True, help="folder with vmlinuz, initrd.img, filesystem.squashfs, BOOTX64.EFI")
    parser.add_argument("--test", type=Path, required=True, help="static test binary of backend/internal/vm")
    parser.add_argument("--work", type=Path, required=True, help="empty work folder (not on a tmpfs: about 1 GB)")
    parser.add_argument("--timeout", type=int, default=420)
    parser.add_argument("--memory", default="3G")
    options = parser.parse_args()
    os.environ["PATH"] += ":/usr/sbin:/sbin"
    root = Path(__file__).resolve().parents[1]
    work = options.work
    if work.exists() and any(work.iterdir()):
        sys.exit(f"{work} is not empty")
    work.mkdir(parents=True, exist_ok=True)
    try:
        stick = work / "stick"
        for folder in ["EFI/BOOT", "loader/entries", "titanos/a", "config"]:
            (stick / folder).mkdir(parents=True)
        shutil.copyfile(options.image / "BOOTX64.EFI", stick / "EFI/BOOT/BOOTX64.EFI")
        shutil.copyfile(root / "boot/loader.conf", stick / "loader/loader.conf")
        entry = (root / "boot/entry.conf.in").read_text().replace("@VERSION@", "0.0.0-vmtest").replace("@SLOT@", "a")
        (stick / "loader/entries/titanos-a.conf").write_text(entry)
        for name in ["vmlinuz", "initrd.img", "filesystem.squashfs"]:
            os.link(options.image / name, stick / "titanos/a" / name) if os.stat(options.image).st_dev == os.stat(work).st_dev else shutil.copyfile(options.image / name, stick / "titanos/a" / name)
        (stick / "titanos/a/version").write_text("0.0.0-vmtest\n")
        disk = work / "stick.img"
        with disk.open("wb") as file:
            file.truncate(4 * 1024**3)
        command("sfdisk", "--quiet", "--label", "dos", str(disk), input=b"start=1MiB, type=c, bootable\n")
        partition = str(disk) + "@@1M"
        command("mformat", "-i", partition, "-F", "-v", "TITANOS", "::")
        command("mcopy", "-i", partition, "-s", *map(str, stick.iterdir()), "::")
        shutil.rmtree(stick)
        shutil.copyfile("/usr/share/OVMF/OVMF_VARS_4M.fd", work / "OVMF_VARS.fd")
        command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(work / "key"))
        port, ssh_port = free_port(), free_port()
        url = f"https://127.0.0.1:{port}"
        context = ssl._create_unverified_context()  # the self-signed certificate of a new NAS
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
                                            urllib.request.HTTPSHandler(context=context))

        def request(path, data=None, method=None):
            req = urllib.request.Request(url + path, data=json.dumps(data).encode() if data is not None else None,
                                         headers={"Content-Type": "application/json", "Origin": url}, method=method)
            with opener.open(req, timeout=20) as response:
                body = response.read()
                return json.loads(body) if body else None

        accel = ["-accel", "kvm", "-cpu", "host"] if os.access("/dev/kvm", os.W_OK) else ["-accel", "tcg", "-cpu", "max"]
        args = ["qemu-system-x86_64", "-machine", "q35",
                "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd",
                "-drive", f"if=pflash,format=raw,file={work}/OVMF_VARS.fd", *accel, "-smp", "2", "-m", options.memory,
                "-device", "qemu-xhci", "-drive", f"if=none,id=stick,format=raw,file={disk}",
                "-device", "usb-storage,drive=stick,removable=on,bootindex=0",
                "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443,hostfwd=tcp:127.0.0.1:{ssh_port}-:22",
                "-device", "virtio-net-pci,netdev=net0",
                "-smbios", "type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200",
                "-display", "none", "-serial", "stdio", "-monitor", "none", "-no-reboot"]
        password = secrets.token_urlsafe(24)
        with (work / "serial.log").open("wb") as serial:
            process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
            try:
                deadline = time.monotonic() + options.timeout
                while True:
                    if process.poll() is not None:
                        raise RuntimeError("QEMU exited before the NAS was ready")
                    try:
                        if request("/api/v1/health").get("ready"):
                            break
                    except (OSError, ValueError):
                        pass
                    if time.monotonic() > deadline:
                        raise TimeoutError("the NAS did not become ready")
                    time.sleep(3)
                print("NAS ready:", request("/api/v1/session").get("version"), accel[1], flush=True)
                request("/api/v1/setup", {"name": "vmtester", "password": password})
                request("/api/v1/account/ssh-keys", {"key": (work / "key.pub").read_text().strip(), "password": password})
                request("/api/v1/system/ssh", {"enabled": True}, method="PUT")
                ssh = ["ssh", "-i", str(work / "key"), "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes",
                       "-o", "StrictHostKeyChecking=accept-new", "-o", f"UserKnownHostsFile={work}/known_hosts",
                       "-o", "ConnectTimeout=3", "-p", str(ssh_port), "vmtester@127.0.0.1"]
                deadline = time.monotonic() + 60
                while subprocess.run([*ssh, "true"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
                    if time.monotonic() > deadline:
                        raise TimeoutError("SSH did not start")
                    time.sleep(1)
                with options.test.open("rb") as stream:
                    subprocess.run([*ssh, "sudo sh -c 'cat > /usr/local/bin/vm.test && chmod 755 /usr/local/bin/vm.test'"], stdin=stream, check=True)
                print(subprocess.check_output([*ssh, "sudo sh -c 'virsh --version; qemu-system-x86_64 --version | head -1; uname -r'"], text=True), flush=True)
                result = subprocess.run([*ssh, "sudo sh -c 'mkdir -p /var/tmp/dbtest && cd /var/tmp && "
                                         "TITANOS_REAL_LIBVIRT=1 TITANOS_REAL_LIBVIRT_DIR=/var/tmp/dbtest "
                                         "/usr/local/bin/vm.test -test.run TestRealLibvirtDiskBackup -test.v -test.timeout 10m 2>&1'"],
                                        text=True, stdout=subprocess.PIPE, timeout=900)
                print(result.stdout, flush=True)
                if result.returncode != 0:
                    print(subprocess.run([*ssh, "sudo journalctl -b --no-pager -n 60 -u virtqemud"], text=True, stdout=subprocess.PIPE).stdout)
                    sys.exit("FAILED: backup of a running VM's disks against real libvirt")
                print("PASSED: backup of a running VM's disks against real libvirt", flush=True)
            finally:
                process.kill()
                process.wait()
    finally:
        shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    main()
