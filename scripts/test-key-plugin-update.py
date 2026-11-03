#!/usr/bin/env python3
"""0.45.1 → 0.46.2 over Settings › Updates with the key plugin (PLAN 34).

Boots the real installation ZIP of 0.45.1 in QEMU with two disposable data
disks, sets it up, and checks against the real GitHub releases:
  1. without the plugin 0.46.2 is refused (unknown signing key);
  2. the community plugin titanos-keys, installed by link, makes it trusted;
  3. the system update installs, the NAS restarts into 0.46.2 and confirms it.
Never touches host disks. Needs internet in QEMU's user network.

  scripts/test-key-plugin-update.py out/keytest/titanos-0.45.1.zip --log out/keytest/serial.log
"""
import argparse
import http.client
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

LINK = "https://github.com/ra5on/TitanOS-Plugins/releases/download/titanos-keys-2/titanos-keys.plugin.json"
TARGET = "0.46.2"


def command(*args, **kwargs):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL, **kwargs)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("archive", type=Path)
    parser.add_argument("--log", type=Path, required=True)
    parser.add_argument("--work", type=Path, default=None, help="folder for the disk images (default: a temporary one)")
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--remove-only", action="store_true", help="only install and remove the plugin on the ZIP's version")
    args = parser.parse_args()
    os.environ["PATH"] += ":/usr/sbin:/sbin"
    context = ssl._create_unverified_context()  # expected self-signed NAS certificate
    with tempfile.TemporaryDirectory(prefix="titanos-keys-", dir=args.work) as tmp:
        directory = Path(tmp)
        stick = directory / "stick"
        with zipfile.ZipFile(args.archive) as packed:
            for item in packed.infolist():
                if not (stick / item.filename).resolve().is_relative_to(stick):
                    raise ValueError("unsafe path in installation ZIP")
            packed.extractall(stick)
        base = (stick / "titanos/a/version").read_text().strip()
        disk = directory / "stick.img"
        with disk.open("wb") as file:
            file.truncate(4 * 1024**3)
        command("sfdisk", "--quiet", "--label", "dos", str(disk), input=b"start=1MiB, type=c, bootable\n")
        partition = str(disk) + "@@1M"
        command("mformat", "-i", partition, "-F", "-v", "TITANOS", "::")
        command("mcopy", "-i", partition, "-s", *map(str, stick.iterdir()), "::")
        shutil.rmtree(stick)
        shutil.copyfile("/usr/share/OVMF/OVMF_VARS_4M.fd", directory / "OVMF_VARS.fd")
        port = free_port()
        url = f"https://127.0.0.1:{port}"
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
                                            urllib.request.HTTPSHandler(context=context))

        def request(path, data=None, method=None, timeout=60):
            req = urllib.request.Request(url + path, data=json.dumps(data).encode() if data is not None else None,
                                         headers={"Content-Type": "application/json", "Origin": url}, method=method)
            try:
                with opener.open(req, timeout=timeout) as response:
                    body = response.read()
                    return json.loads(body) if body else None
            except urllib.error.HTTPError as error:
                error.body = error.read()
                print(f"{method or ('POST' if data is not None else 'GET')} {path}: {error.code} {error.body[:300]!r}", flush=True)
                raise

        def wait(what, check, patience):
            deadline = time.monotonic() + patience
            last = None
            while time.monotonic() < deadline:
                try:
                    result = check()
                    if result:
                        return result
                    last = "not yet"
                except urllib.error.HTTPError as error:
                    last = f"{error.code} {getattr(error, 'body', b'')[:300]!r}"
                except (OSError, ValueError, http.client.HTTPException) as error:
                    last = error
                time.sleep(5)
            raise TimeoutError(f"{what} did not happen in time ({last}); see {args.log}")

        disks = []
        for index in (1, 2):
            data = directory / f"data{index}.qcow2"
            command("qemu-img", "create", "-q", "-f", "qcow2", str(data), "6G")
            disks += ["-drive", f"if=none,id=data{index},format=qcow2,file={data}", "-device", f"virtio-blk-pci,drive=data{index},serial=KEYS{index}"]
        accel = ["-accel", "kvm", "-cpu", "host"] if os.access("/dev/kvm", os.W_OK) else ["-accel", "tcg", "-cpu", "max"]
        qemu = ["qemu-system-x86_64", "-machine", "q35", *accel, "-smp", "2", "-m", "4G",
                "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd",
                "-drive", f"if=pflash,format=raw,file={directory}/OVMF_VARS.fd",
                "-device", "qemu-xhci", "-drive", f"if=none,id=stick,format=raw,file={disk}",
                "-device", "usb-storage,drive=stick,removable=on,bootindex=0", *disks,
                "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443", "-device", "virtio-net-pci,netdev=net0",
                "-smbios", "type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200",
                "-display", "none", "-serial", "stdio", "-monitor", "none", "-no-reboot"]
        password = secrets.token_urlsafe(24)

        def boot(serial, first):
            process = subprocess.Popen(qemu, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
            wait("NAS ready", lambda: process.poll() is None and request("/api/v1/health", timeout=10).get("ready"), args.timeout)
            if first:
                request("/api/v1/setup", {"name": "keytester", "password": password})
            else:
                request("/api/v1/login", {"name": "keytester", "password": password})
            return process

        def reboot(process):
            try:
                request("/api/v1/system/power", {"action": "reboot"})
            except (OSError, http.client.HTTPException):
                pass
            try:
                process.wait(timeout=180)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()

        def check_update():
            try:
                request("/api/v1/update/check", {}, timeout=120)
            except urllib.error.HTTPError as error:
                print("check answered", error.code, error.read()[:300], flush=True)
            except http.client.IncompleteRead:
                pass
            return request("/api/v1/update")

        with args.log.open("wb") as serial:
            print(f"Boot 1: TitanOS {base} ({accel[1]})", flush=True)
            process = boot(serial, True)
            try:
                assert request("/api/v1/session")["version"] == base
                storage = request("/api/v1/storage")
                identities = {d["name"]: d["identity"] for d in storage["disks"]}
                request("/api/v1/storage/volumes", {"name": "keytest", "level": "raid1", "filesystem": "ext4", "disks": ["vda", "vdb"],
                                                    "erase": ["vda", "vdb"], "identities": identities}, timeout=300)
                wait("volume mounted", lambda: any(v["name"] == "keytest" and v.get("mounted") for v in request("/api/v1/storage")["volumes"]), 300)

                if args.remove_only:
                    request("/api/v1/plugins/external", {"url": LINK, "volume": "keytest", "confirm": "titanos-keys"}, timeout=300)
                    wait("key plugin active", lambda: any(p["name"] == "titanos-keys" and p["active"] for p in request("/api/v1/plugins")["plugins"]), 120)
                    print("update:", {k: v for k, v in request("/api/v1/update").items() if k in ("pending", "progress", "unconfirmed")}, flush=True)
                    request("/api/v1/plugins/titanos-keys/disable", {})
                    request("/api/v1/plugins/titanos-keys", method="DELETE")
                    print("ok: key plugin installed and removed on", base, flush=True)
                    reboot(process)
                    return
                status = check_update()
                print(f"Without the plugin: available={status.get('available') and status['available'].get('version')}, error={status.get('checkError')!r}", flush=True)
                if status.get("available"):
                    raise AssertionError("0.46.2 was trusted without the key plugin")

                found = request("/api/v1/plugins/external/inspect", {"url": LINK}, timeout=120)
                print("Link shows:", json.dumps({k: found.get(k) for k in ("name", "version", "official")}), flush=True)
                if found.get("name") != "titanos-keys" or found.get("official"):
                    raise AssertionError(f"unexpected plugin behind the link: {found}")
                request("/api/v1/plugins/external", {"url": LINK, "volume": "keytest", "confirm": "titanos-keys"}, timeout=300)
                wait("key plugin active", lambda: any(p["name"] == "titanos-keys" and p["active"] for p in request("/api/v1/plugins")["plugins"]), 120)
                print("Key plugin installed and active", flush=True)

                status = wait("0.46.2 offered", lambda: (lambda s: s if (s.get("available") or {}).get("version") == TARGET else None)(check_update()), 300)
                print(f"With the plugin: 0.46.2 offered (kernel {status['available'].get('kernel')})", flush=True)
                try:
                    request("/api/v1/update/install", {}, timeout=120)
                except (OSError, http.client.HTTPException) as error:
                    print("install answered:", error, flush=True)

                def installed():
                    state = request("/api/v1/update")
                    phase = state["progress"]["phase"]
                    if phase == "failed":
                        raise AssertionError(f"update failed: {state['progress']}")
                    return state if state.get("pending") else None
                state = wait("update prepared", installed, 3600)
                print(f"Update prepared: {state['pending']}", flush=True)
            except Exception:
                reboot(process)
                raise
            reboot(process)

            print(f"Boot 2: expecting TitanOS {TARGET}", flush=True)
            process = boot(serial, False)
            try:
                session = request("/api/v1/session")
                if session["version"] != TARGET:
                    raise AssertionError(f"running {session['version']} after the update")
                # The update counts once the trial is over; titand settles it
                # within a minute, until then plugins stay locked.
                state = wait("update settled", lambda: (lambda s: s if not s.get("pending") and not s.get("unconfirmed") and all(slot.get("triesLeft") is None for slot in s["slots"]) else None)(request("/api/v1/update")), 300)
                print("Running", TARGET, "slots:", [(s["name"], s["version"], s["running"]) for s in state["slots"]], flush=True)
                plugins = request("/api/v1/plugins")["plugins"]
                keys = next((p for p in plugins if p["name"] == "titanos-keys"), None)
                print("Key plugin after the update:", keys and {k: keys[k] for k in ("installed", "enabled", "active", "problem")}, flush=True)
                if keys and keys["installed"]:
                    request("/api/v1/plugins/titanos-keys/disable", {})
                    request("/api/v1/plugins/titanos-keys", method="DELETE")
                    print("Key plugin removed", flush=True)
                status = check_update()
                if status.get("checkError"):
                    raise AssertionError(f"0.46.2 cannot check for updates without the plugin: {status['checkError']}")
                print("Update check on", TARGET, "without the plugin works", flush=True)
            finally:
                reboot(process)
    print("ok: 0.45.1 → 0.46.2 over Settings › Updates with the key plugin", flush=True)


if __name__ == "__main__":
    main()
