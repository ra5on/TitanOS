#!/usr/bin/env python3
"""App updates in a disposable UEFI VM, from a release server on the host.

Boots the installation ZIP, then installs a newer titand as an app update
(no restart of the NAS, logins kept), confirms it, restarts the NAS and
finds it still running. An app that does not start is given up on after
three starts. Going back leads to the titand of the system image.

The releases are signed with the disposable rehearsal key whose public half
is in the image (pull requests and manual runs only; never a real key).

Usage: smoke-app-update.py <zip> --release out/release --key <secret key>
"""
import argparse
import hashlib
import http.client
import http.cookiejar
import http.server
import json
import os
import re
import secrets
import shutil
import socket
import ssl
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent


def command(*args, **kwargs):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL, **kwargs)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def bump(version, step):
    """The version step releases after version."""
    match = re.fullmatch(r"([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([a-z]+)\.([0-9]+))?", version)
    if not match:
        raise ValueError("cannot count on from " + version)
    major, minor, patch, pre, number = match.groups()
    if pre:
        return f"{major}.{minor}.{patch}-{pre}.{int(number) + step}"
    return f"{major}.{minor}.{int(patch) + step}"


class Releases:
    """A project page on the host with one release at a time."""

    def __init__(self, root, base_manifest, key):
        self.root, self.base, self.key = root, base_manifest, key
        handler = lambda *args: http.server.SimpleHTTPRequestHandler(*args, directory=str(root))
        http.server.SimpleHTTPRequestHandler.log_message = lambda *args: None
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.port = self.server.server_address[1]

    def publish(self, version, binary):
        asset = f"titanos-{version}-titand"
        assets = self.root / "releases/download" / ("v" + version)
        assets.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(binary, assets / asset)
        data = (assets / asset).read_bytes()
        manifest = dict(self.base, version=version, tag="v" + version, changelog="App update rehearsal.",
                        app={"base": self.base["app"]["base"], "asset": asset,
                             "sha256": hashlib.sha256(data).hexdigest(), "size": len(data)})
        latest = self.root / "releases/latest/download"
        latest.mkdir(parents=True, exist_ok=True)
        (latest / "manifest.json").write_text(json.dumps(manifest, indent=2))
        (latest / "manifest.json.minisig").unlink(missing_ok=True)
        command("minisign", "-S", "-s", str(self.key), "-m", str(latest / "manifest.json"), "-t", f"TitanOS {version}: manifest.json")


def smoke(archive, release, key, timeout, log, force_tcg=False):
    os.environ["PATH"] += ":/usr/sbin:/sbin"
    context = ssl._create_unverified_context()  # expected self-signed NAS certificate
    base_manifest = json.loads((release / "manifest.json").read_text())
    base = base_manifest["version"]
    good, bad = bump(base, 1), bump(base, 2)
    with tempfile.TemporaryDirectory(prefix="titanos-app-") as tmp:
        directory = Path(tmp)
        # A newer titand from this checkout, and one that cannot start.
        build = subprocess.run(["go", "build", "-trimpath", "-tags", "embedui",
                                "-ldflags", f"-s -w -X github.com/ra5on/TitanOS/backend/internal/version.Version={good}",
                                "-o", str(directory / "titand-good"), "./cmd/titand"],
                               cwd=REPO / "backend", env=dict(os.environ, CGO_ENABLED="0"))
        if build.returncode:
            raise RuntimeError("could not build the app update")
        (directory / "titand-bad").write_text("#!/bin/sh\necho 'this app update does not start' >&2\nexit 1\n")
        releases = Releases(directory / "server", base_manifest, key)

        stick = directory / "stick"
        with zipfile.ZipFile(archive) as packed:
            for item in packed.infolist():
                if not (stick / item.filename).resolve().is_relative_to(stick):
                    raise ValueError("unsafe path in installation ZIP")
            packed.extractall(stick)
        # The NAS asks the project page on the host (10.0.2.2 in QEMU's network).
        (stick / "config/system").mkdir(parents=True)
        (stick / "config/system/update.json").write_text(json.dumps(
            {"channel": "stable", "autoInstall": False, "installTime": "03:30", "source": f"http://10.0.2.2:{releases.port}"}))
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
        jar = http.cookiejar.CookieJar()
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar), urllib.request.HTTPSHandler(context=context))

        def request(path, data=None, method=None):
            req = urllib.request.Request(url + path, data=json.dumps(data).encode() if data is not None else None,
                                         headers={"Content-Type": "application/json", "Origin": url}, method=method)
            with opener.open(req, timeout=20) as response:
                body = response.read()
                return json.loads(body) if body else None

        def wait(what, check, patience):
            """Asks until check passes. Errors while titand restarts are expected; a lost login is not."""
            deadline = time.monotonic() + patience
            last = None
            while time.monotonic() < deadline:
                try:
                    result = check()
                    if result:
                        return result
                    last = "not yet"
                except urllib.error.HTTPError as error:
                    if error.code == 401:
                        raise AssertionError(f"{what}: the login was lost") from error
                    last = error
                except (OSError, ValueError, http.client.HTTPException) as error:
                    last = error
                time.sleep(3)
            try:
                state = update_status()
                last = f"{last}; version {state['version']}, app {state['app']}, progress {state['progress']}"
            except Exception as error:  # the NAS may not answer at all
                last = f"{last}; status not readable: {error}"
            raise TimeoutError(f"{what} did not happen in time ({last}); see {log}")

        def update_status():
            return request("/api/v1/update")

        def check():
            """Looks for a release. QEMU's user network sometimes cuts the answer
            off after the guest fetched from the host (see smoke-release.py);
            what was found is then read again."""
            try:
                return request("/api/v1/update/check", {})
            except http.client.IncompleteRead:
                return update_status()

        def restarting(path):
            """Starts what restarts titand; its answer may not get out any more."""
            try:
                request(path, {})
            except (OSError, http.client.HTTPException):
                pass

        def notified(kind, subject):
            messages = request("/api/v1/notifications")["messages"]
            return any(m["kind"] == kind and m["subject"] == subject for m in messages)

        accel = ["-accel", "kvm", "-cpu", "host"] if not force_tcg and os.access("/dev/kvm", os.W_OK) else ["-accel", "tcg", "-cpu", "max"]
        args = ["qemu-system-x86_64", "-machine", "q35", *accel, "-smp", "2", "-m", "4G",
                "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd",
                "-drive", f"if=pflash,format=raw,file={directory}/OVMF_VARS.fd",
                "-device", "qemu-xhci", "-drive", f"if=none,id=stick,format=raw,file={disk}",
                "-device", "usb-storage,drive=stick,removable=on,bootindex=0",
                "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443", "-device", "virtio-net-pci,netdev=net0",
                "-smbios", "type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200 systemd.journald.forward_to_console=1",
                "-display", "none", "-serial", "stdio", "-monitor", "none", "-no-reboot"]
        password = secrets.token_urlsafe(24)

        def boot(serial, first):
            process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
            wait("NAS ready", lambda: process.poll() is None and request("/api/v1/health").get("ready"), timeout)
            if first:
                request("/api/v1/setup", {"name": "apptester", "password": password})
            else:
                request("/api/v1/login", {"name": "apptester", "password": password})
            return process

        def stop(process):
            try:
                request("/api/v1/system/power", {"action": "reboot"})
                process.wait(timeout=90)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()

        with log.open("wb") as serial:
            print(f"Boot 1: TitanOS {base} ({accel[1]})", flush=True)
            process = boot(serial, True)
            try:
                status = update_status()
                if status["base"] != base or status["version"] != base:
                    raise AssertionError(f"image does not know its base version: {status}")

                # 1. An app update: titand alone, without a restart of the NAS.
                releases.publish(good, directory / "titand-good")
                status = check()
                if status["kind"] != "app" or status["available"]["version"] != good:
                    raise AssertionError(f"not offered as an app update: kind={status['kind']} available={status['available']}")
                restarting("/api/v1/update/install")
                status = wait("app update running and confirmed", lambda: (s := update_status())["version"] == good and not s["app"]["trial"] and s, 420)
                if status["app"]["current"] != good or status["slots"][0]["version"] != base or status["pending"]:
                    raise AssertionError(f"wrong state after the app update: {status}")
                wait("message about the installed app", lambda: notified("update_installed", good), 120)
                print(f"App update {base} → {good} without restart of the NAS, login kept, confirmed", flush=True)
            finally:
                stop(process)

            print("Boot 2: the app runs again after a restart of the NAS", flush=True)
            process = boot(serial, False)
            try:
                status = wait("app after restart", lambda: (s := update_status())["version"] == good and s, 120)
                if status["app"]["trial"]:
                    raise AssertionError("a confirmed app is on trial again after a restart")

                # 2. An app that does not start is given up on.
                releases.publish(bad, directory / "titand-bad")
                status = check()
                if status["kind"] != "app" or status["available"]["version"] != bad:
                    raise AssertionError(f"broken app not offered: {status['available']}")
                restarting("/api/v1/update/install")
                status = wait("back on the app before", lambda: (s := update_status())["version"] == good and not s["app"]["trial"] and s["app"]["current"] == good and s, 420)
                wait("message about the failed app", lambda: notified("update_failed", bad), 120)
                print(f"App {bad} that does not start: back on {good} by itself", flush=True)

                # 3. Back to the titand of the system image.
                restarting("/api/v1/update/app-rollback")
                status = wait("titand of the system image", lambda: (s := update_status())["version"] == base and s, 300)
                if status["app"]["current"] or status["app"]["previous"] != good:
                    raise AssertionError(f"wrong state after going back: {status['app']}")
                print(f"Back to the titand of system image {base}", flush=True)
            finally:
                stop(process)
    print("App update smoke test passed", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("zip", type=Path)
    parser.add_argument("--release", type=Path, required=True, help="the packed release of the ZIP (its manifest)")
    parser.add_argument("--key", type=Path, required=True, help="disposable secret key whose public half is in the image")
    parser.add_argument("--timeout", type=int, default=420)
    parser.add_argument("--log", type=Path, default=Path("out/app-update-smoke.log"))
    parser.add_argument("--tcg", action="store_true", help="force CPU emulation to reproduce CI without KVM")
    args = parser.parse_args()
    args.log.parent.mkdir(parents=True, exist_ok=True)
    smoke(args.zip.resolve(), args.release.resolve(), args.key.resolve(), args.timeout, args.log, args.tcg)
