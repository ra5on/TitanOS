"""Disruption checks for a disposable guest of smoke-system-update.py."""
import copy
import hashlib
import http.client
import http.server
import ipaddress
import json
import os
import re
import subprocess
import threading
import time
import urllib.error


class ReleaseHandler(http.server.SimpleHTTPRequestHandler):
    """Hold the system image partway through a download until the VM is off."""

    def log_message(self, *_args):
        pass

    def copyfile(self, source, output):
        hold = self.server.hold
        if hold is None or not self.path.endswith("-filesystem.squashfs"):
            return super().copyfile(source, output)
        chunk = source.read(8 * 1024**2)
        if not chunk or source.tell() >= os.fstat(source.fileno()).st_size:
            raise ValueError("the interruption test needs a partial image")
        # The daemon samples progress once per second. Deliver two parts on
        # opposite sides of that interval even on a fast KVM host.
        half = len(chunk) // 2
        output.write(chunk[:half])
        output.flush()
        time.sleep(1.5)
        output.write(chunk[half:])
        output.flush()
        hold.wait(timeout=180)


def check_share(directory, port, password, upload=False, remote_name="pruefung.bin"):
    """Read the same file over SMB2/3 and compare every byte to the original."""
    assert remote_name in ("pruefung.bin", "frisch.bin")
    source = directory / "original.bin"
    if upload and not source.exists():
        source.write_bytes(os.urandom(32768))
    target = directory / "retrieved.bin"
    target.unlink(missing_ok=True)
    commands = (f'put "{source}" {remote_name}; ' if upload else '') + f'get {remote_name} "{target}"'
    last = directory / "smb-observation.txt"
    try:
        result = subprocess.run([
            "smbclient", "//127.0.0.1/Pruefung", "-p", str(port), "-m", "SMB3",
            "-U", "systemtester", "-c", commands,
        ], env=dict(os.environ, PASSWD=password), capture_output=True, timeout=60)
    except subprocess.TimeoutExpired:
        problem = "smbclient timed out"
    else:
        output = (result.stdout + result.stderr).decode(errors="replace")
        status = sorted(set(re.findall(r"(?:NT_STATUS|WERR)_[A-Z0-9_]+", output)) - {"NT_STATUS_OK", "WERR_OK"})
        if result.returncode or status:
            problem = f"smbclient exit {result.returncode}; status {', '.join(status) or 'unspecified'}"
        elif not target.is_file():
            problem = "the client returned without downloading the file"
        else:
            expected, received = source.read_bytes(), target.read_bytes()
            if received == expected:
                last.unlink(missing_ok=True)
                return True
            problem = (f"received {len(received)} bytes SHA-256 {hashlib.sha256(received).hexdigest()}; "
                       f"expected {len(expected)} bytes SHA-256 {hashlib.sha256(expected).hexdigest()}")
    # Log a change of outcome and keep retries readable. Never print the
    # command environment, which holds the random guest account password.
    if not last.exists() or last.read_text() != problem:
        print(f"SMB readback {remote_name}: {problem}", flush=True)
        last.write_text(problem)
    return False


def diagnose_share(request, fetch_file, directory):
    """Read the failed disposable fixture directly; diagnostics never replace the SMB gate."""
    try:
        volumes = request("/api/v1/storage")["volumes"]
        mounted = any(v["name"] == "testdaten" and v["mounted"] for v in volumes)
        print(f"SMB diagnosis: testdaten mounted={mounted}", flush=True)
        data = fetch_file()
        expected = (directory / "original.bin").read_bytes()
        print(f"Native Files read {len(data)} bytes; identical to original={data == expected}", flush=True)
    except (OSError, http.client.HTTPException) as error:
        print(f"Native Files or storage check failed: {error}", flush=True)


def create_share(request, wait, directory, port, password):
    def submit(path, body):
        # A client timeout cannot tell whether the guest finished a format or
        # SMB reconfiguration. Poll the resulting state; never format twice.
        try:
            request(path, body, timeout=180)
        except urllib.error.HTTPError:
            raise
        except (OSError, http.client.HTTPException):
            pass

    disks = request("/api/v1/storage")["disks"]
    disk = next(d for d in disks if d["serial"] == "NORDLICHT-DATA")
    name = disk["name"]
    submit("/api/v1/storage/volumes", {
        "name": "testdaten", "level": "raid0", "filesystem": "ext4",
        "disks": [name], "erase": [name], "identities": {name: disk["identity"]},
    })
    wait("disposable volume mounted", lambda: any(v["name"] == "testdaten" and v["mounted"]
         for v in request("/api/v1/storage")["volumes"]), 240)
    submit("/api/v1/shares", {"name": "Pruefung", "volume": "testdaten", "smb": True})
    wait("test file written and read over SMB", lambda: check_share(directory, port, password, True), 180)


def interrupt_install(request, check, wait, releases, files, version, kill, boot, stick):
    before = request("/api/v1/update")
    # The SMB file was just read back through this share. Verify its persisted
    # configuration independently of any optional application or background job.
    share_path = "config/shares/shares.json"
    saved_share = stick.read(share_path)
    assert any(sh.get("name") == "Pruefung" for sh in json.loads(saved_share))

    releases.publish(version, files)
    offer = check()
    assert offer["kind"] == "system" and offer["available"]["version"] == version, offer["available"]
    hold = releases.server.hold = threading.Event()
    try:
        try:
            request("/api/v1/update/install", {})
        except http.client.IncompleteRead:
            pass

        partial = "::/titanos/.incoming-b/filesystem.squashfs.part"
        def part_on_stick():
            result = subprocess.run(["mcopy", "-i", stick.partition, partial, "-"], capture_output=True)
            return result.stdout if result.returncode == 0 else b""

        def writing():
            p = request("/api/v1/update")["progress"]
            if p["phase"] == "failed":
                raise AssertionError(f"installation failed before test interruption: {p}")
            return p["phase"] == "download" and part_on_stick()

        observed = wait("partial filesystem.squashfs on test stick", writing, 180)
        kill()
        # Read again with the guest off, before fsck or any startup cleanup.
        copied = part_on_stick()
        assert 0 < len(observed) <= len(copied) < files["filesystem.squashfs"].stat().st_size
        with files["filesystem.squashfs"].open("rb") as source:
            assert copied == source.read(len(copied))
    finally:
        hold.set()
        releases.server.hold = None
    assert stick.tries() == {"a": None}, stick.entries()
    after = boot()
    active = next(s for s in after["slots"] if s["running"])
    assert active["name"] == "a" and active["complete"] and after["version"] == before["version"], active
    assert not after["pending"] and not after["unconfirmed"], after
    assert stick.read(share_path) == saved_share
    print(f"Power cut after {len(copied)} verified bytes of filesystem.squashfs; slot A started {after['version']} with no pending switch.", flush=True)


def network_trials(request, wait, verify_share, log):
    path = "/api/v1/network"
    top = request(path)["network"]
    management = next(net for net in top["topology"]["networks"] if net["management"])
    spare = next(i["name"] for i in top["interfaces"] if i["mac"].lower() == "52:54:00:da:7a:02")
    trials = [
        ("IP", {"ipv4": "static", "address": "192.0.2.17/24", "gateway": "192.0.2.1"}),
        ("VLAN", {"vlan": 37}),
        ("Bridge on isolated port", {"parent": spare, "bridge": True}),
    ]
    original_vlan_option = top["vlans"]
    old_ipv4 = {address for address in top["addresses"] if ipaddress.ip_interface(address).version == 4}
    assert old_ipv4, "the old management interface has no IPv4 address"
    for name, edits in trials:
        if name == "VLAN" and not original_vlan_option:
            request("/api/v1/network/options", {"vlans": True}, "PUT")
        # Use the same multi-network object and PUT endpoint as the settings page.
        draft = copy.deepcopy(top["topology"])
        next(net for net in draft["networks"] if net["id"] == management["id"]).update(edits)
        old = top["config"]
        offset = log.stat().st_size
        started = time.monotonic()
        try:
            result = request(path, draft, "PUT")
            assert result["network"]["pending"] is not None, result["network"]
        except urllib.error.HTTPError:
            raise
        except (OSError, http.client.HTTPException):
            pass  # changing the management path can cut off the initiating response

        def offline():
            try:
                request("/api/v1/health", timeout=2)
                return False
            except urllib.error.HTTPError:
                raise
            except (OSError, http.client.HTTPException):
                return True

        wait(f"{name} broke the original management connection", offline, 45)
        def restored():
            state = request(path, timeout=3)["network"]
            return state["pending"] is None and state["config"] == old and state["device"] == top["device"] and bool(old_ipv4.intersection(state["addresses"]))
        wait(f"{name} automatically restored the management connection", restored, 215)
        # The log also proves the change was accepted when its HTTP response
        # did not arrive. A brief unrelated client timeout cannot pass.
        with log.open("rb") as serial:
            serial.seek(offset)
            trial_log = serial.read()
        assert b"network change on trial" in trial_log, name
        assert b"network change was not confirmed, going back" in trial_log, name
        wait("file reachable over SMB after network rollback", verify_share, 60)
        print(f"{name}: management became unreachable and recovered without confirmation after {time.monotonic()-started:.0f}s; original configuration and SMB bytes verified.", flush=True)
    if not original_vlan_option:
        request("/api/v1/network/options", {"vlans": False}, "PUT")
