#!/usr/bin/env python3
"""Full system updates (A/B slots) in a disposable VM, from a release server on the host.

Boots the installation ZIP and installs full system releases through the API,
each into the slot that is not running, as a NAS does it:

  good     a newer system: the boot entry carries its counter on the stick
           during the trial start, the NAS runs from the other slot, the boot
           check marks it good (entry without counter) and /api/v1/update shows
           it confirmed.
  nostart  a system whose titand does not start: the counter goes down with
           every start, the fourth start is the system of before again, and
           titand notes the failed release (config/system/update-failed.json,
           a message, no installation by itself of that release).
  panic    a system whose initrd is no initramfs: the kernel panics and panic=10
           on the command line restarts it, three times, then the system of
           before runs again.
  hang     a system that ends in emergency mode before titand (a mount that
           never comes, set on the kernel command line of the release):
           titanos-trial-watchdog.timer restarts it half an hour after each
           start, three times, then the system of before runs again.

The stick is read from the host with mtools between the starts (and during the
trial start of the good system): the entry names in loader/entries and, for
GRUB, its count in boot/grub/grubenv.

What the releases are, and where this differs from a real release:

  - They are signed with the disposable rehearsal key whose public half is in
    the image (pull requests and manual runs only; never a real key).
  - good and nostart are the system image of the ZIP, unpacked and packed again
    (zstd level 6 instead of 19) with another /usr/bin/titand and its version
    in /usr/share/titanos/base-version: for good a titand built from this
    checkout with the next version, for nostart a script that exits at once.
    Kernel and initrd are those of the ZIP. Unpacking needs the owners of the
    files: as root, with sudo, or in a user namespace (unshare --map-auto).
  - panic and hang keep the system image of the ZIP; only the initrd (panic) or
    the kernel command line in the manifest (hang) differ. They never get to
    run titand.
  - The manifests carry no kernel variant ("variants"): the test server holds
    the default system only.
  - hang runs its three trial starts with QEMU's instruction counter
    (-icount sleep=off, CPU emulation even where KVM is there): the clock of
    the VM jumps ahead while the system idles, so the 30 minutes of the timer
    pass in the guest but not on the host. The image and its timer are not
    changed; what is not shown is half an hour of real time on real hardware.
    --real-time waits it out instead (three times 30 minutes).

Never attaches host or existing test disks.

Usage: smoke-system-update.py <zip> --release out/release --key <secret key>
           [--firmware bios] [--scenarios good,nostart,panic,hang]
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
SCENARIOS = ["good", "nostart", "panic", "hang"]
TRIES = 3
# A mount that never comes: local-fs.target fails and the system changes to
# emergency mode, long before titand and the boot check.
HANG = " systemd.mount-extra=/dev/disk/by-label/TITANOS-ABSENT:/run/titanos-absent:ext4"
WATCHDOG_SECONDS = 30 * 60

# Unpacks the image once and packs it again for every (version, titand, output).
REPACK = r"""
set -eu
root="$1/rootfs"
trap 'rm -rf "$root"' EXIT
unsquashfs -q -no-progress -d "$root" "$2"
shift 2
while [ $# -gt 0 ]; do
    install -m 0755 "$2" "$root/usr/bin/titand"
    printf '%s\n' "$1" > "$root/usr/share/titanos/base-version"
    mksquashfs "$root" "$3" -comp zstd -Xcompression-level 6 -b 1M -noappend -no-progress -quiet
    chmod 0644 "$3"
    shift 3
done
"""


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


def other(slot):
    return "b" if slot == "a" else "a"


def as_root():
    """How to run a command that keeps the owners of the files of an image."""
    if os.geteuid() == 0:
        return []
    if subprocess.run(["sudo", "-n", "true"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        return ["sudo", "--preserve-env=PATH"]
    # Like scripts/make-zip.sh: the full subuid range of the user.
    return ["unshare", "--user", "--map-auto", "--map-root-user"]


def repack(squashfs, work, *images):
    """images: (version, titand, output) each."""
    command(*as_root(), "sh", "-c", REPACK, "repack", str(work), str(squashfs), *[str(part) for image in images for part in image])


def sha256(path):
    with open(path, "rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def tries_left(name, grub_trial="", grub_count=0):
    """What a boot entry name says about its slot: (slot, tries left), tries
    None for an entry marked good. GRUB cannot rename on FAT and counts the
    starts of the entry it has on trial in grubenv instead."""
    match = re.fullmatch(r"titanos-([ab])(?:\+([0-9]+)(?:-[0-9]+)?)?\.conf", name)
    if not match:
        return None
    if match[2] is None:
        return match[1], None
    left = int(match[2])
    if name == grub_trial:
        left = max(0, left - grub_count)
    return match[1], left


class Stick:
    """The FAT partition of the stick image, read from the host with mtools."""

    def __init__(self, disk):
        self.partition = str(disk) + "@@1M"
        self.env = dict(os.environ, MTOOLS_SKIP_CHECK="1")

    def read(self, path):
        result = subprocess.run(["mtype", "-i", self.partition, "::" + path], capture_output=True, env=self.env)
        return result.stdout.decode(errors="replace") if result.returncode == 0 else None

    def entries(self):
        result = subprocess.run(["mdir", "-b", "-i", self.partition, "::loader/entries"], capture_output=True, text=True, env=self.env)
        if result.returncode:
            return None
        return sorted(line.rsplit("/", 1)[-1] for line in result.stdout.split())

    def tries(self):
        """Tries left by slot; None while the stick cannot be read (the VM is writing)."""
        names = self.entries()
        if names is None:
            return None
        env = dict(line.split("=", 1) for line in (self.read("boot/grub/grubenv") or "").splitlines() if "=" in line and not line.startswith("#"))
        count = int(env.get("titanos_count") or 0)
        return dict(filter(None, (tries_left(name, env.get("titanos_trial", ""), count) for name in names)))

    def sort_key(self, slot):
        for name in self.entries() or []:
            if (tries_left(name) or ("",))[0] == slot:
                match = re.search(r"(?m)^sort-key[ \t]+(\S+)", self.read("loader/entries/" + name) or "")
                return match[1] if match else None
        return None


class Releases:
    """A project page on the host with one release at a time."""

    def __init__(self, root, base_manifest, key):
        self.root, self.base, self.key = root, base_manifest, key
        handler = lambda *args: http.server.SimpleHTTPRequestHandler(*args, directory=str(root))
        http.server.SimpleHTTPRequestHandler.log_message = lambda *args: None
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.port = self.server.server_address[1]

    def publish(self, version, files, cmdline=None, titand=None):
        """files: the three slot files by name; titand: the app of a full
        release, as scripts/make-release.sh lists it."""
        assets = self.root / "releases/download" / ("v" + version)
        assets.mkdir(parents=True, exist_ok=True)

        def asset(name, path):
            target = assets / f"titanos-{version}-{name}"
            target.unlink(missing_ok=True)
            target.symlink_to(path)
            return {"asset": target.name, "sha256": sha256(path), "size": path.stat().st_size}

        manifest = dict(self.base, version=version, tag="v" + version, changelog="System update rehearsal.",
                        cmdline=cmdline or self.base["cmdline"],
                        files=[dict(name=name, **asset(name, files[name])) for name in ["vmlinuz", "initrd.img", "filesystem.squashfs"]])
        manifest.pop("variants", None)
        manifest.pop("app", None)
        if titand:
            manifest["app"] = dict(base=version, **asset("titand", titand))
        latest = self.root / "releases/latest/download"
        latest.mkdir(parents=True, exist_ok=True)
        (latest / "manifest.json").write_text(json.dumps(manifest, indent=2))
        (latest / "manifest.json.minisig").unlink(missing_ok=True)
        command("minisign", "-S", "-s", str(self.key), "-m", str(latest / "manifest.json"), "-t", f"TitanOS {version}: manifest.json")


def smoke(archive, release, key, timeout, log, force_tcg=False, firmware="uefi", scenarios=SCENARIOS, memory="4G", real_time=False):
    os.environ["PATH"] += ":/usr/sbin:/sbin"
    context = ssl._create_unverified_context()  # expected self-signed NAS certificate
    base_manifest = json.loads((release / "manifest.json").read_text())
    base = base_manifest["version"]
    versions = {name: bump(base, step) for step, name in enumerate(SCENARIOS, 1)}
    slot_files = {name: release / f"titanos-{base}-{name}" for name in ["vmlinuz", "initrd.img", "filesystem.squashfs"]}
    with tempfile.TemporaryDirectory(prefix="titanos-system-") as tmp:
        directory = Path(tmp)
        releases = Releases(directory / "server", base_manifest, key)
        images = []
        if "good" in scenarios:
            # A newer titand from this checkout, in a system image of its own.
            build = subprocess.run(["go", "build", "-trimpath", "-tags", "embedui",
                                    "-ldflags", f"-s -w -X github.com/ra5on/TitanOS/backend/internal/version.Version={versions['good']}",
                                    "-o", str(directory / "titand-good"), "./cmd/titand"],
                                   cwd=REPO / "backend", env=dict(os.environ, CGO_ENABLED="0"))
            if build.returncode:
                raise RuntimeError("could not build the titand of the new system")
            images.append((versions["good"], directory / "titand-good", directory / "good.squashfs"))
        if "nostart" in scenarios:
            (directory / "titand-nostart").write_text("#!/bin/sh\necho 'smoke test: the titand of this system does not start' >&2\nexit 1\n")
            images.append((versions["nostart"], directory / "titand-nostart", directory / "nostart.squashfs"))
        if images:
            print("Packing the system images of the test releases", flush=True)
            repack(slot_files["filesystem.squashfs"], directory, *images)
        # No initramfs: the kernel finds no root and panics.
        (directory / "initrd-panic").write_bytes(b"TitanOS smoke test: this is not an initramfs.\n" * 4096)

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
        console = "console=tty0 console=ttyS0,115200 systemd.journald.forward_to_console=1"
        if firmware != "uefi":
            # GRUB cannot read QEMU's SMBIOS credentials: the serial console
            # comes from the stick's own custom.cfg instead.
            (stick / "boot/grub/custom.cfg").write_text(f'set titanos_extra="{console}"\n')
        disk = directory / "stick.img"
        with disk.open("wb") as file:
            file.truncate(4 * 1024**3)
        command("sfdisk", "--quiet", "--label", "dos", str(disk), input=b"start=1MiB, type=c, bootable\n")
        partition = str(disk) + "@@1M"
        command("mformat", "-i", partition, "-F", "-v", "TITANOS", "::")
        command("mcopy", "-i", partition, "-s", *map(str, stick.iterdir()), "::")
        if firmware == "bios":
            # The tool on the stick, as a user runs it.
            command("python3", str(stick / "boot/grub/stick-bios.py"), str(disk), str(stick / "boot/grub"))
        shutil.rmtree(stick)
        on_stick = Stick(disk)
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
            """Asks until check passes. Errors while the NAS starts are expected; a lost login is not."""
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

        def notified(kind, subject):
            messages = request("/api/v1/notifications")["messages"]
            return any(m["kind"] == kind and m["subject"] == subject for m in messages)

        def slot(status, name):
            return next(s for s in status["slots"] if s["name"] == name)

        def running(status):
            return next(s for s in status["slots"] if s["running"])["name"]

        kvm = not force_tcg and os.access("/dev/kvm", os.W_OK)
        accel = ["-accel", "kvm", "-cpu", "host"] if kvm else ["-accel", "tcg", "-cpu", "max"]
        # The clock of the VM follows the instructions it runs and jumps to
        # the next timer while it idles (see the module text).
        warp = ["-accel", "tcg", "-cpu", "max", "-icount", "shift=1,sleep=off", "-rtc", "clock=vm"]
        if firmware == "bios":
            # SeaBIOS on the old PC machine, like a board without UEFI.
            boot_firmware = ["-machine", "pc"]
        else:
            shutil.copyfile("/usr/share/OVMF/OVMF_VARS_4M.fd", directory / "OVMF_VARS.fd")
            boot_firmware = ["-machine", "q35", "-drive", "if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd",
                             "-drive", f"if=pflash,format=raw,file={directory}/OVMF_VARS.fd"]

        def qemu(speed):
            return ["qemu-system-x86_64", *boot_firmware, *speed, "-smp", "2", "-m", memory,
                    "-device", "qemu-xhci", "-drive", f"if=none,id=stick,format=raw,file={disk}",
                    "-device", "usb-storage,drive=stick,removable=on,bootindex=0",
                    "-netdev", f"user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443", "-device", "virtio-net-pci,netdev=net0",
                    "-smbios", "type=11,value=io.systemd.boot.kernel-cmdline-extra=" + console.replace(",", ",,"),
                    "-display", "none", "-serial", "stdio", "-monitor", "none", "-no-reboot"]

        password = secrets.token_urlsafe(24)
        loader = {"uefi": "systemd-boot", "bios": "grub-i386-pc"}[firmware]
        state = {"boot": 0, "process": None}

        with log.open("wb") as serial:

            def console_since(offset):
                serial.flush()
                with log.open("rb") as written:
                    written.seek(offset)
                    return written.read().decode(errors="replace")

            def start(speed=accel):
                state["boot"] += 1
                state["process"] = subprocess.Popen(qemu(speed), stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
                return state["process"]

            def boot(first=False):
                """Starts the VM and waits for the NAS; returns its update status."""
                process = start()
                wait("NAS ready", lambda: process.poll() is None and request("/api/v1/health").get("ready"), timeout)
                request("/api/v1/setup" if first else "/api/v1/login", {"name": "systemtester", "password": password})
                status = update_status()
                if status["bootLoader"] != loader:
                    raise AssertionError(f"started by {status['bootLoader']}, expected {loader}")
                return status

            def stop():
                """Restarts the NAS; QEMU ends instead (-no-reboot)."""
                process = state["process"]
                try:
                    request("/api/v1/system/power", {"action": "reboot"})
                    process.wait(timeout=180)
                finally:
                    kill()

            def kill():
                process = state["process"]
                if process and process.poll() is None:
                    process.kill()
                    process.wait()

            def install(version, files, cmdline=None, titand=None):
                """Publishes a full release, installs it into the other slot and
                stops the VM before the restart. Returns the slot it went to."""
                releases.publish(version, files, cmdline, titand)
                status = check()
                if status["kind"] != "system" or (status["available"] or {}).get("version") != version:
                    raise AssertionError(f"not offered as a system update: kind={status['kind']} available={status['available']}")
                before, old = running(status), status["version"]
                target = other(before)
                try:
                    request("/api/v1/update/install", {})
                except http.client.IncompleteRead:
                    pass  # cut off like the answer of check(); the progress below tells

                def installed():
                    now = update_status()
                    if now["progress"]["phase"] == "failed":
                        raise AssertionError(f"installation failed: {now['progress']['error']}")
                    return now["progress"]["phase"] == "done" and now

                status = wait(f"{version} written to slot {target}", installed, max(timeout, 1200))
                pending, there = status["pending"], slot(status, target)
                if not pending or (pending["kind"], pending["version"], pending["slot"], pending["from"]) != ("update", version, target, old):
                    raise AssertionError(f"wrong pending update: {pending}")
                if there["version"] != version or not there["complete"] or not there["next"] or there["triesLeft"] != TRIES:
                    raise AssertionError(f"slot {target} is not the one on trial next: {there}")
                stop()
                # On the stick, read from the host: the new entry with its
                # counter comes first, the running system is the fallback.
                tries = on_stick.tries()
                if tries != {target: TRIES, before: None}:
                    raise AssertionError(f"boot entries after the installation: {on_stick.entries()}")
                if (on_stick.sort_key(target), on_stick.sort_key(before)) != ("titanos-1", "titanos-2"):
                    raise AssertionError(f"sort keys after the installation: {target}={on_stick.sort_key(target)} {before}={on_stick.sort_key(before)}")
                if (on_stick.read(f"titanos/{target}/version") or "").strip() != version:
                    raise AssertionError(f"slot {target} on the stick does not name {version}")
                if json.loads(on_stick.read("config/system/update-pending.json") or "{}").get("version") != version:
                    raise AssertionError("no pending update on the stick")
                return target, before, old

            def fails(name, files, cmdline, proof, speed=accel, patience=None, uptime=0):
                """A release that does not come up: three starts that end by
                themselves, each costing a try, then the system of before."""
                version = versions[name]
                target, before, old = install(version, files, cmdline, directory / "titand-nostart" if name == "nostart" else None)
                for attempt in range(1, TRIES + 1):
                    offset, began = serial.tell(), time.monotonic()
                    process = start(speed)
                    try:
                        process.wait(timeout=patience or timeout)
                    except subprocess.TimeoutExpired:
                        kill()
                        raise AssertionError(f"{name}: trial start {attempt} of {version} did not restart by itself within {patience or timeout} s; see {log}")
                    output = console_since(offset)
                    for text in proof:
                        if text not in output:
                            raise AssertionError(f"{name}: trial start {attempt} ended without {text!r} on the console; see {log}")
                    if uptime:
                        # The clock of the guest when it restarted.
                        stamps = re.findall(r"\[ *([0-9]+)\.[0-9]+\] reboot: Restarting system", output)
                        if not stamps or int(stamps[-1]) < uptime:
                            raise AssertionError(f"{name}: trial start {attempt} restarted after {stamps[-1:] or 'unknown'} s of the guest, expected at least {uptime}")
                    tries = on_stick.tries()
                    if tries != {target: TRIES - attempt, before: None}:
                        raise AssertionError(f"{name}: tries after trial start {attempt}: {tries} ({on_stick.entries()})")
                    print(f"  trial start {attempt}: restarted by itself after {time.monotonic() - began:.0f} s, {TRIES - attempt} tries left on the stick", flush=True)
                status = boot()
                if running(status) != before or status["version"] != old:
                    raise AssertionError(f"{name}: not back in slot {before} with {old}: {status['slots']}")
                status = wait("failed update settled", lambda: (s := update_status())["pending"] is None and s, 180)
                if slot(status, target)["triesLeft"] != 0 or slot(status, target)["next"] or not slot(status, before)["next"] or status["unconfirmed"]:
                    raise AssertionError(f"{name}: wrong slots after the fallback: {status['slots']}")
                wait("message about the failed update", lambda: notified("update_failed", version), 120)
                failed = wait("note of the failed release", lambda: on_stick.read("config/system/update-failed.json"), 60)
                if json.loads(failed).get("version") != version:
                    raise AssertionError(f"{name}: update-failed.json names {failed}")
                print(f"System {version} ({name}): three trial starts, then back in slot {before} with {old} by itself; failed release noted", flush=True)
                return version

            def left_alone(version):
                """titand does not install the failed release by itself again,
                but still offers it."""
                status = check()
                if (status["available"] or {}).get("version") != version:
                    raise AssertionError(f"the failed release is no longer offered: {status['available']}")
                offset = serial.tell()
                # The install time is now, in the time zone of the NAS (UTC).
                with opener.open(url + "/api/v1/health", timeout=20) as response:
                    now = time.strptime(response.headers["Date"], "%a, %d %b %Y %H:%M:%S %Z")
                settings = {"channel": "stable", "autoInstall": True, "installTime": time.strftime("%H:%M", now)}
                request("/api/v1/update/settings", settings, method="PUT")
                try:
                    wait("scheduled installation left out",
                         lambda: "scheduled update left out: this release failed its trial on this NAS" in console_since(offset), 200)
                    status = update_status()
                    if status["pending"] or status["progress"]["phase"] in ("download", "switch"):
                        raise AssertionError(f"the failed release is installed again by itself: {status['progress']}")
                finally:
                    request("/api/v1/update/settings", dict(settings, autoInstall=False), method="PUT")
                print(f"System {version}: not installed again by itself, still offered", flush=True)

            try:
                print(f"Boot 1: TitanOS {base} ({loader}, {accel[1]})", flush=True)
                status = boot(first=True)
                if status["base"] != base or status["version"] != base or running(status) != "a":
                    raise AssertionError(f"image does not run {base} from slot a: {status}")
                if on_stick.tries() != {"a": None}:
                    raise AssertionError(f"boot entries of a fresh stick: {on_stick.entries()}")

                if "good" in scenarios:
                    good = versions["good"]
                    target, before, old = install(good, dict(slot_files, **{"filesystem.squashfs": directory / "good.squashfs"}), titand=directory / "titand-good")
                    # The loader takes a try off the entry before the system starts,
                    # and the boot check removes the counter seconds after titand
                    # is ready: the stick is watched all the while.
                    seen, done = set(), threading.Event()

                    def watch():
                        while not done.wait(0.5):
                            tries = on_stick.tries()
                            if tries:
                                seen.add(tries.get(target))

                    watcher = threading.Thread(target=watch, daemon=True)
                    watcher.start()
                    try:
                        status = boot()
                        status = wait("system confirmed", lambda: (s := update_status())["version"] == good and not s["unconfirmed"] and s["pending"] is None and s, 300)
                    finally:
                        done.set()
                        watcher.join()
                    if TRIES - 1 not in seen:
                        raise AssertionError(f"no trial start seen on the stick: tries of slot {target} were {seen}")
                    here = slot(status, target)
                    if running(status) != target or status["base"] != good or here["triesLeft"] is not None or not here["next"] or not status["canRollback"]:
                        raise AssertionError(f"wrong state after the system update: {status}")
                    if slot(status, before)["version"] != old or not slot(status, before)["complete"]:
                        raise AssertionError(f"the system of before is gone: {status['slots']}")
                    wait("message about the installed system", lambda: notified("update_installed", good), 120)
                    wait("entry without counter on the stick", lambda: on_stick.tries() == {target: None, before: None}, 60)
                    if on_stick.read("config/system/update-pending.json") is not None:
                        raise AssertionError("the pending update is still on the stick")
                    print(f"System update {old} → {good}: slot {target} on trial with {TRIES - 1} tries left, marked good by the boot check, confirmed", flush=True)

                if "nostart" in scenarios:
                    print("A system whose titand does not start", flush=True)
                    left_alone(fails("nostart", dict(slot_files, **{"filesystem.squashfs": directory / "nostart.squashfs"}), None,
                                     ["smoke test: the titand of this system does not start"], patience=timeout + 120))

                if "panic" in scenarios:
                    print("A system whose kernel panics (panic=10)", flush=True)
                    fails("panic", dict(slot_files, **{"initrd.img": directory / "initrd-panic"}), None, ["Kernel panic - not syncing"])

                if "hang" in scenarios:
                    print("A system that stops in emergency mode (titanos-trial-watchdog.timer)", flush=True)
                    fails("hang", slot_files, base_manifest["cmdline"] + HANG,
                          ["Reached target emergency.target", "this system is still on trial half an hour after its start"],
                          speed=accel if real_time else warp, patience=(WATCHDOG_SECONDS + timeout + 300) if real_time else max(4 * timeout, 1800),
                          uptime=WATCHDOG_SECONDS)
                stop()
            finally:
                kill()
    print("System update smoke test passed", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("zip", type=Path)
    parser.add_argument("--release", type=Path, required=True, help="the packed release of the ZIP (its manifest and slot files)")
    parser.add_argument("--key", type=Path, required=True, help="disposable secret key whose public half is in the image")
    parser.add_argument("--timeout", type=int, default=420)
    parser.add_argument("--log", type=Path, default=Path("out/system-update-smoke.log"))
    parser.add_argument("--tcg", "--force-tcg", action="store_true", help="force CPU emulation to reproduce CI without KVM")
    parser.add_argument("--firmware", choices=["uefi", "bios"], default="uefi", help="64-bit UEFI (systemd-boot) or legacy BIOS (GRUB)")
    parser.add_argument("--scenarios", default=",".join(SCENARIOS), help="which of good, nostart, panic, hang to run (default: all)")
    parser.add_argument("--memory", default="4G", help="RAM of the VM")
    parser.add_argument("--real-time", action="store_true", help="hang: wait three times 30 minutes instead of letting the clock of the VM jump")
    args = parser.parse_args()
    chosen = [name for name in SCENARIOS if name in args.scenarios.split(",")]
    if not chosen or set(args.scenarios.split(",")) - set(SCENARIOS):
        parser.error("--scenarios takes a list of " + ", ".join(SCENARIOS))
    args.log.parent.mkdir(parents=True, exist_ok=True)
    smoke(args.zip.resolve(), args.release.resolve(), args.key.resolve(), args.timeout, args.log, args.tcg,
          args.firmware, chosen, args.memory, args.real_time)
