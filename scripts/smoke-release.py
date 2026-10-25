#!/usr/bin/env python3
"""Boot an installation ZIP twice using isolated, disposable UEFI/USB media.

Checks first setup, HTTPS, the real version, persistent accounts/certificate,
locked-down SSH, HDD idle settings and About notices. Never attaches host or
existing test disks.
"""
import argparse
import http.cookiejar
import http.client
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

from network_smoke import check_network
from tailscale_smoke import check_tailscale


def command(*args, **kwargs):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL, **kwargs)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def smoke(archive, timeout, log, plugins=None, force_tcg=False):
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
        ssh_port = free_port()
        url = f"https://127.0.0.1:{port}"
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
                                            urllib.request.HTTPSHandler(context=context))

        def request(path, data=None, method=None):
            req = urllib.request.Request(url + path, data=json.dumps(data).encode() if data is not None else None,
                                         headers={"Content-Type": "application/json", "Origin": url}, method=method)
            # Plugin installation hashes and copies a large image before
            # activating sysext. CI uses CPU emulation without KVM, where
            # this legitimately takes longer than an ordinary API request.
            request_timeout = 180 if path.startswith("/api/v1/plugins") else 15
            with opener.open(req, timeout=request_timeout) as response:
                try:
                    body = response.read()
                except http.client.IncompleteRead as error:
                    if path.startswith("/api/v1/plugins"):
                        print("Incomplete plugin response", dict(response.headers), repr(error.partial), flush=True)
                    raise
                return json.loads(body) if body else None

        accel = ["-accel", "kvm", "-cpu", "host"] if not force_tcg and os.access("/dev/kvm", os.W_OK) else ["-accel", "tcg", "-cpu", "max"]
        args = ["qemu-system-x86_64", "-machine", "q35", *accel, "-smp", "2", "-m", "4G",
                "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd",
                "-drive", f"if=pflash,format=raw,file={directory}/OVMF_VARS.fd",
                "-device", "qemu-xhci", "-drive", f"if=none,id=stick,format=raw,file={disk}",
                "-device", "usb-storage,drive=stick,removable=on,bootindex=0",
                "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443,hostfwd=tcp:127.0.0.1:{ssh_port}-:22", "-device", "virtio-net-pci,netdev=net0",
                "-smbios", "type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200",
                "-display", "none", "-serial", "stdio", "-monitor", "none", "-no-reboot"]
        if plugins:
            for index in (1, 2):
                data_disk = directory / f"data{index}.qcow2"
                command("qemu-img", "create", "-q", "-f", "qcow2", str(data_disk), "2G")
                args += ["-drive", f"if=none,id=data{index},format=qcow2,file={data_disk}",
                         "-device", f"virtio-blk-pci,drive=data{index},serial=SMOKE{index}"]
            command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(directory / "ssh-key"))

        def plugin_test(boot):
            # Only the fresh VM receives this disposable key and test storage.
            if boot == 1:
                request("/api/v1/account/ssh-keys", {"key": (directory / "ssh-key.pub").read_text().strip()})
                request("/api/v1/storage/volumes", {"name": "smoketest", "level": "raid0", "filesystem": "ext4", "disks": ["vda", "vdb"], "erase": ["vda", "vdb"]})
            request("/api/v1/system/ssh", {"enabled": True}, method="PUT")
            ssh_options = ["-i", str(directory / "ssh-key"), "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes",
                           "-o", "StrictHostKeyChecking=accept-new", "-o", f"UserKnownHostsFile={directory}/known_hosts", "-o", "ConnectTimeout=3"]
            ssh = ["ssh", *ssh_options, "-p", str(ssh_port), "betatester@127.0.0.1"]
            deadline = time.monotonic() + 45
            while subprocess.run([*ssh, "true"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
                if time.monotonic() > deadline:
                    raise TimeoutError("SSH test access did not start")
                time.sleep(1)
            if boot == 1:
                command(*ssh, "sudo mkdir -p /mnt/smoketest/plugins && sudo chown betatester /mnt/smoketest/plugins")
                files = [plugins / "nvidia.raw", plugins / "nvidia.plugin.json"]
                signed = (plugins / "nvidia.plugin.json.minisig").is_file()
                if signed:
                    files.append(plugins / "nvidia.plugin.json.minisig")
                command("scp", *ssh_options, "-P", str(ssh_port), *map(str, files), "betatester@127.0.0.1:/mnt/smoketest/plugins/")
                try:
                    request("/api/v1/plugins", {"path": "/mnt/smoketest/plugins/nvidia.plugin.json", "volume": "smoketest", "confirm": "" if signed else "nvidia"})
                except urllib.error.HTTPError as error:
                    if error.code != 500 or json.loads(error.read()).get("error", {}).get("code") != "plugin_failed":
                        raise
                else:
                    raise AssertionError("NVIDIA activation unexpectedly succeeded without a GPU")
            deadline = time.monotonic() + 30
            while True:
                entries = request("/api/v1/plugins")["plugins"]
                gpu = next((p for p in entries if p["name"] == "nvidia" and p["installed"]), None)
                if gpu and gpu["problem"] == "activation":
                    break
                if time.monotonic() > deadline:
                    raise AssertionError(f"missing failed activation state: {entries}")
                time.sleep(1)
            if gpu["active"] or not gpu["enabled"] or gpu["official"] != (plugins / "nvidia.plugin.json.minisig").is_file():
                raise AssertionError(f"wrong plugin state: {gpu}")
            command(*ssh, "sudo sh -ec 'test -x /usr/bin/nvidia-smi; test -x /usr/bin/nvidia-ctk; test -f /run/titanos-plugin-modules/lib/modules/$(uname -r)/modules.dep.bin; modprobe -d /run/titanos-plugin-modules --show-depends nvidia_uvm; for tool in /usr/bin/nvidia-smi /usr/bin/nvidia-container-cli; do if ldd $tool | grep -q \"not found\"; then exit 1; fi; done; findmnt -n -o OPTIONS /usr | grep -w ro'")
            if boot == 2:
                request("/api/v1/plugins/nvidia/disable", {})
                request("/api/v1/plugins/nvidia", method="DELETE")
                command(*ssh, "test ! -e /run/extensions/nvidia.raw && test ! -e /usr/bin/nvidia-smi && test -f /mnt/smoketest/plugins/nvidia.raw")
            if boot == 2:
                check_tailscale(request, ssh, ssh_options, ssh_port, plugins)
            request("/api/v1/system/ssh", {"enabled": False}, method="PUT")
            print(f"Boot {boot}: plugin trust, sysext, RAM module index, missing-GPU handling and lifecycle passed", flush=True)
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
                    system_info = request("/api/v1/system")
                    memory = system_info["memory"]
                    assert memory["totalBytes"] > 0 and memory["usedBytes"] + memory["freeBytes"] == memory["totalBytes"], memory
                    assert 0 <= memory["cacheBytes"] <= memory["usedBytes"] and memory["availableBytes"] <= memory["totalBytes"], memory
                    if tuple(map(int, version.split('-')[0].split('.'))) >= (0, 21, 0):
                        assert memory["reserveBytes"] > 0, memory
                        policy = request("/api/v1/system/memory")
                        assert policy["settings"]["cachePercent"] == 20 and not policy.get("error"), policy
                        if "pressure" in memory:
                            assert 0 <= memory["pressure"]["fullAvg10"] <= memory["pressure"]["someAvg10"] <= 100, memory
                    print(f"Boot {boot}: memory sample " + json.dumps(memory, sort_keys=True), flush=True)
                    if system_info["accelerators"] != []:
                        raise AssertionError("virtual display must not appear as a physical accelerator")
                    if plugins:
                        try:
                            plugin_test(boot)
                            check_network(boot, request, directory, ssh_port)
                        except Exception:
                            with log.with_suffix(".services.log").open("wb") as diagnostic:
                                subprocess.run(["ssh", "-i", str(directory / "ssh-key"), "-p", str(ssh_port),
                                                "-o", "BatchMode=yes", "-o", f"UserKnownHostsFile={directory}/known_hosts",
                                                "-o", "ConnectTimeout=3", "betatester@127.0.0.1",
                                                "sudo journalctl -b -u titand.service -u titanos-nvidia.service --no-pager -n 80"],
                                               stdout=diagnostic, stderr=subprocess.STDOUT, timeout=15)
                            raise
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
    parser.add_argument("--plugins", type=Path, help="test NVIDIA plugin installation and removal using disposable VM disks")
    parser.add_argument("--tcg", action="store_true", help="force CPU emulation to reproduce CI without KVM")
    args = parser.parse_args()
    args.log.parent.mkdir(parents=True, exist_ok=True)
    smoke(args.zip.resolve(), args.timeout, args.log, args.plugins.resolve() if args.plugins else None, args.tcg)
