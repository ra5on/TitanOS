#!/usr/bin/env python3
"""Disposable three-boot test of the public GitHub Stable update and rollback.

Verifies the old ZIP with the project's trusted public keys, then installs
the expected current Stable release directly from GitHub and rolls back.
No update mirror, host disks, host Docker or host network configuration.
Only new temporary QEMU media are used. The target must still be the public
latest Stable release when the test runs.
"""
import argparse
import hashlib
import http.client
import http.cookiejar
import json
import os
from pathlib import Path
import re
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

REPO = Path(__file__).resolve().parents[2]
os.environ["PATH"] += ":/usr/sbin:/sbin"


def command(*args, **kwargs):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL, **kwargs)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def run(archive, from_version, to_version, log, result_path, force_tcg=False, timeout=600):
    log.parent.mkdir(parents=True, exist_ok=True)
    result_path.parent.mkdir(parents=True, exist_ok=True)
    for key in sorted((REPO / 'build/mkosi.extra/usr/share/titanos/update-keys').glob('*.pub')):
        verified = subprocess.run(['minisign', '-V', '-q', '-p', str(key), '-m', str(archive)],
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if verified.returncode == 0:
            break
    else:
        raise ValueError('old ZIP signature is missing or not signed by a trusted project key')
    print(f'Old ZIP signature verified with {key.name}', flush=True)
    with archive.open('rb') as stream:
        archive_hash = hashlib.file_digest(stream, 'sha256').hexdigest()
    evidence = {"source": "https://github.com/ra5on/TitanOS", "channel": "stable",
                "from": from_version, "to": to_version, "mirror": False,
                "started": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "archiveSHA256": archive_hash, "oldZIPSigningKey": key.name,
                "runnerSHA256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                "checks": []}
    with tempfile.TemporaryDirectory(prefix="titanos-public-update-") as temporary:
        directory = Path(temporary)
        stick = directory / "stick"
        with zipfile.ZipFile(archive) as packed:
            for item in packed.infolist():
                if not (stick / item.filename).resolve().is_relative_to(stick):
                    raise ValueError("unsafe ZIP path")
                if item.filename.startswith("config/") and not item.is_dir():
                    raise ValueError("published image contains configuration")
            packed.extractall(stick)
        assert (stick / "titanos/a/version").read_text().strip() == from_version
        disk = directory / "stick.img"
        with disk.open("wb") as stream:
            stream.truncate(4 * 1024**3)
        command("sfdisk", "--quiet", "--label", "dos", str(disk),
                input=b"start=1MiB, type=c, bootable\n")
        command("mformat", "-i", str(disk) + "@@1M", "-F", "-v", "TITANOS", "::")
        command("mcopy", "-i", str(disk) + "@@1M", "-s", *map(str, stick.iterdir()), "::")
        shutil.copyfile("/usr/share/OVMF/OVMF_VARS_4M.fd", directory / "OVMF_VARS.fd")
        port = free_port()
        url = f"https://127.0.0.1:{port}"
        context = ssl._create_unverified_context()  # expected self-signed NAS certificate
        jar = http.cookiejar.CookieJar()
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar),
                                            urllib.request.HTTPSHandler(context=context))

        def request(path, data=None):
            req = urllib.request.Request(url + path,
                    data=json.dumps(data).encode() if data is not None else None,
                    headers={"Content-Type": "application/json", "Origin": url})
            try:
                with opener.open(req, timeout=120 if path.endswith("/check") else 20) as response:
                    body = response.read()
                    return json.loads(body) if body else None
            except urllib.error.HTTPError as error:
                raise RuntimeError(f"{path}: HTTP {error.code}: {error.read().decode()}") from error

        def wait(label, check, patience=600):
            deadline = time.monotonic() + patience
            last = None
            last_print = 0
            while time.monotonic() < deadline:
                try:
                    result = check()
                    if result:
                        return result
                    last = "not ready"
                except (OSError, ValueError, http.client.HTTPException) as error:
                    last = str(error)
                if time.monotonic() - last_print >= 30:
                    print(f"Waiting: {label} ({last})", flush=True)
                    last_print = time.monotonic()
                time.sleep(3)
            raise TimeoutError(f"{label}: {last}; see {log}")

        acceleration = ["-accel", "kvm", "-cpu", "host"] if not force_tcg and os.access("/dev/kvm", os.W_OK) else ["-accel", "tcg", "-cpu", "max"]
        evidence["acceleration"] = acceleration[1]
        args = ["qemu-system-x86_64", "-machine", "q35", *acceleration, "-smp", "2", "-m", "4G",
                "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd",
                "-drive", f"if=pflash,format=raw,file={directory}/OVMF_VARS.fd",
                "-device", "qemu-xhci", "-drive", f"if=none,id=stick,format=raw,file={disk}",
                "-device", "usb-storage,drive=stick,removable=on,bootindex=0",
                "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443",
                "-device", "virtio-net-pci,netdev=net0",
                "-smbios", "type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200 systemd.journald.forward_to_console=1",
                "-display", "none", "-serial", "stdio", "-monitor", "none", "-no-reboot"]
        password = secrets.token_urlsafe(24)
        certificate = None

        def status():
            return request("/api/v1/update")

        def settled(version, slot):
            current = status()
            if current["progress"]["phase"] == "failed":
                raise RuntimeError(current["progress"]["error"])
            running = next(item for item in current["slots"] if item["running"])
            return current if current["version"] == version and running["name"] == slot and not current["pending"] and not current["unconfirmed"] else None

        def pending(version, slot):
            current = status()
            if current["progress"]["phase"] == "failed":
                raise RuntimeError(current["progress"]["error"])
            value = current["pending"]
            return current if value and value["version"] == version and value["slot"] == slot else None

        with log.open("wb") as serial:
            for boot, version, slot in ((1, from_version, "a"), (2, to_version, "b"), (3, from_version, "a")):
                print(f"Boot {boot}: public TitanOS {version}, slot {slot}", flush=True)
                process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
                try:
                    def healthy():
                        if process.poll() is not None:
                            raise RuntimeError(f"QEMU exited {process.returncode}; see {log}")
                        return request("/api/v1/health").get("ready")
                    wait("NAS health", healthy, timeout)
                    current_certificate = ssl.get_server_certificate(("127.0.0.1", port))
                    if boot == 1:
                        certificate = current_certificate
                        request("/api/v1/setup", {"name": "publictester", "password": password})
                    else:
                        assert current_certificate == certificate, "HTTPS certificate changed"
                        jar.clear()
                        request("/api/v1/login", {"name": "publictester", "password": password})
                    session = request("/api/v1/session")
                    assert session["version"] == version and session["user"]["role"] == "owner"
                    state = wait("confirmed boot", lambda: settled(version, slot), timeout)
                    evidence["checks"].append({"boot": boot, "version": version, "slot": slot,
                                               "account": "owner preserved", "certificate": "preserved",
                                               "confirmed": True})
                    if boot == 1:
                        assert not state["settings"].get("source"), "update source is overridden"
                        try:
                            state = request("/api/v1/update/check", {})
                        except http.client.IncompleteRead:
                            state = status()
                        assert state["available"] and state["available"]["version"] == to_version, state
                        assert state["kind"] == "system" and state["canVerify"], state
                        assert state["available"]["app"]["base"] == to_version, state
                        print(f"Signed GitHub Stable {to_version} found as full system update", flush=True)
                        request("/api/v1/update/install", {})
                        state = wait("GitHub files downloaded and slot B prepared", lambda: pending(to_version, "b"), 1800)
                        print("Slot B prepared from real GitHub release", flush=True)
                    elif boot == 2:
                        assert state["base"] == to_version and state["canRollback"], state
                        request("/api/v1/update/rollback", {})
                        wait("rollback to slot A prepared", lambda: pending(from_version, "a"))
                        print(f"Rollback to {from_version} prepared", flush=True)
                    else:
                        assert state["base"] == from_version, state
                        print("Rollback confirmed, owner and HTTPS certificate preserved", flush=True)
                    try:
                        request("/api/v1/system/power", {"action": "reboot"})
                    except (OSError, http.client.HTTPException):
                        pass
                    process.wait(timeout=120)
                finally:
                    if process.poll() is None:
                        process.kill()
                        process.wait()
    evidence["completed"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    evidence["result"] = "passed"
    result_path.write_text(json.dumps(evidence, indent=2) + "\n")
    print(f"Public GitHub update and rollback passed: {result_path}", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('zip', type=Path, help='published old installation ZIP with adjacent .minisig')
    parser.add_argument('--from-version', required=True, help='version inside the old ZIP')
    parser.add_argument('--to-version', required=True, help='expected public latest Stable full-system release')
    parser.add_argument('--log', type=Path, default=REPO/'out/public-update-smoke.log')
    parser.add_argument('--result', type=Path, default=REPO/'out/validation/public-update.json')
    parser.add_argument('--timeout', type=int, default=600, help='seconds per boot/confirmation')
    parser.add_argument('--tcg', action='store_true', help='force CPU emulation without KVM')
    args = parser.parse_args()
    if not all(re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', value) for value in [args.from_version, args.to_version]):
        parser.error('both versions must be Stable versions such as 0.22.0')
    if args.from_version == args.to_version or args.timeout < 1:
        parser.error('versions must differ and timeout must be positive')
    run(args.zip.resolve(), args.from_version, args.to_version, args.log.resolve(), args.result.resolve(), args.tcg, args.timeout)
