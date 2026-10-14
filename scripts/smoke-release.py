#!/usr/bin/env python3
"""Boot an installation ZIP twice using isolated, disposable UEFI/USB media.

Checks first setup, HTTPS, the real version, persistent accounts/certificate,
locked-down SSH, HDD idle settings and About notices. Never attaches host or
existing test disks.
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
import urllib.error
import urllib.request
import zipfile
from pathlib import Path


def command(*args, **kwargs):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL, **kwargs)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def smoke(archive, timeout, log):
    os.environ["PATH"] += ":/usr/sbin:/sbin"
    context = ssl._create_unverified_context()  # expected self-signed NAS certificate
    with tempfile.TemporaryDirectory(prefix="titanos-release-") as tmp:
        directory = Path(tmp)
        stick = directory / "stick"
        with zipfile.ZipFile(archive) as packed:
            for item in packed.infolist():
                if not (stick / item.filename).resolve().is_relative_to(stick):
                    raise ValueError("unsafe path in installation ZIP")
                if item.filename.startswith("config/") and not item.is_dir():
                    raise ValueError("installation ZIP contains user configuration")
            packed.extractall(stick)
        version = (stick / "titanos/a/version").read_text().strip()
        disk = directory / "stick.img"
        with disk.open("wb") as file:
            file.truncate(4 * 1024**3)
        command("sfdisk", "--quiet", "--label", "dos", str(disk), input=b"start=1MiB, type=c, bootable\n")
        partition = str(disk) + "@@1M"
        command("mformat", "-i", partition, "-F", "-v", "TITANOS", "::")
        command("mcopy", "-i", partition, "-s", *map(str, stick.iterdir()), "::")
        shutil.copyfile("/usr/share/OVMF/OVMF_VARS_4M.fd", directory / "OVMF_VARS.fd")
        port = free_port()
        url = f"https://127.0.0.1:{port}"
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
                                            urllib.request.HTTPSHandler(context=context))

        def request(path, data=None, method=None):
            req = urllib.request.Request(url + path, data=json.dumps(data).encode() if data is not None else None,
                                         headers={"Content-Type": "application/json", "Origin": url}, method=method)
            with opener.open(req, timeout=15) as response:
                body = response.read()
                return json.loads(body) if body else None

        accel = ["-accel", "kvm", "-cpu", "host"] if os.access("/dev/kvm", os.W_OK) else ["-accel", "tcg", "-cpu", "max"]
        args = ["qemu-system-x86_64", "-machine", "q35", *accel, "-smp", "2", "-m", "4G",
                "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd",
                "-drive", f"if=pflash,format=raw,file={directory}/OVMF_VARS.fd",
                "-device", "qemu-xhci", "-drive", f"if=none,id=stick,format=raw,file={disk}",
                "-device", "usb-storage,drive=stick,removable=on,bootindex=0",
                "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443", "-device", "virtio-net-pci,netdev=net0",
                "-smbios", "type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200",
                "-display", "none", "-serial", "stdio", "-monitor", "none", "-no-reboot"]
        password = secrets.token_urlsafe(24)
        certificate = None
        with log.open("wb") as serial:
            for boot in (1, 2):
                print(f"UEFI boot {boot}: TitanOS {version} ({accel[1]})", flush=True)
                process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
                try:
                    deadline = time.monotonic() + timeout
                    while True:
                        if process.poll() is not None:
                            raise RuntimeError("QEMU exited before becoming ready; see " + str(log))
                        try:
                            if request("/api/v1/health").get("ready"):
                                break
                        except (OSError, ValueError):
                            pass
                        if time.monotonic() > deadline:
                            raise TimeoutError("NAS did not become ready; see " + str(log))
                        time.sleep(3)
                    current_certificate = ssl.get_server_certificate(("127.0.0.1", port))
                    session = request("/api/v1/session")
                    if not session["configAvailable"] or session["setupRequired"] != (boot == 1):
                        raise AssertionError(f"unexpected setup state on boot {boot}: {session}")
                    if boot == 1:
                        certificate = current_certificate
                        request("/api/v1/setup", {"name": "betatester", "password": password})
                    else:
                        if certificate != current_certificate:
                            raise AssertionError("HTTPS certificate did not survive reboot")
                        request("/api/v1/login", {"name": "betatester", "password": password})
                    session = request("/api/v1/session")
                    if session["version"] != version or session["user"]["role"] != "owner":
                        raise AssertionError("wrong version or owner account")
                    # These authenticated endpoints also exercise the embedded backend/UI contract.
                    about = request("/api/v1/about")
                    if "Debian:" not in json.dumps(about) or "Go:" not in json.dumps(about):
                        raise AssertionError("generated third-party notices missing from About")
                    if "hd-idle" not in json.dumps(about):
                        raise AssertionError("HDD standby package missing from third-party inventory")
                    if request("/api/v1/system/ssh")["enabled"]:
                        raise AssertionError("SSH must be disabled on a fresh installation")
                    sleep_settings = request("/api/v1/storage/disk-sleep")
                    if not sleep_settings["available"] or sleep_settings["idleMinutes"] != (0 if boot == 1 else 5):
                        raise AssertionError("HDD idle setting did not default to off or survive reboot")
                    if boot == 1:
                        # This VM only has the excluded boot stick, never host/data disks.
                        changed = request("/api/v1/storage/disk-sleep", {"idleMinutes": 5}, method="PUT")
                        if changed["idleMinutes"] != 5:
                            raise AssertionError("HDD idle setting was not saved")
                    with opener.open(url + "/", timeout=15) as response:
                        if b"<html" not in response.read():
                            raise AssertionError("embedded web UI missing")
                    print(f"Boot {boot}: HTTPS, health, owner, version, UI, notices and HDD idle settings passed", flush=True)
                    # Flush configuration before stopping this disposable VM. A real reboot
                    # is requested through the API and QEMU exits because of -no-reboot.
                    request("/api/v1/system/power", {"action": "reboot"})
                    process.wait(timeout=90)
                finally:
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=10)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait()
        print("Release ZIP smoke test passed", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("zip", type=Path)
    parser.add_argument("--timeout", type=int, default=420)
    parser.add_argument("--log", type=Path, default=Path("out/release-smoke.log"))
    args = parser.parse_args()
    args.log.parent.mkdir(parents=True, exist_ok=True)
    smoke(args.zip.resolve(), args.timeout, args.log)
