#!/usr/bin/env python3
"""Prepare an app release: the published image of the base, with a new titand.

An app release changes titand (with the web UI built into it) and nothing
else. It is not built with mkosi: the system image of its base release is
downloaded, checked against the keys in this repository, unpacked, given the
new titand and the license inventory of the new titand, and packed again with
the options mkosi.finalize uses. Kernel, initrd, boot loader and plugins are
those of the base, bit for bit.

The result is the layout make-release.sh expects after "make zip":

    out/stick/                       the stick, with the new image in slot a
    build/mkosi.output/kernel-version
    build/mkosi.output/rootfs/usr/share/titanos/  license inventory
    out/plugins/                     the plugins of the base release

Usage: repack-release.py --base 0.8.0 --version 0.8.1 [--source URL]
Needs root for unsquashfs/mksquashfs (owners of the files in the image) and
the Go toolchain and web/node_modules that built backend/bin/titand.
"""
import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.request
import zipfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9.]+)?")
# For tests: no sudo (the owners in the image are then lost), and another
# script for the license inventory.
NO_SUDO = os.environ.get("REPACK_NO_SUDO") == "1"
LICENSES = os.environ.get("REPACK_LICENSES", str(REPO / "scripts/third-party-licenses.py"))


def run(*args, **kwargs):
    subprocess.run(args, check=True, **kwargs)


def sudo(*args):
    prefix = [] if os.geteuid() == 0 or NO_SUDO else ["sudo", "--preserve-env=PATH"]
    run(*prefix, *args)


def download(url, target):
    request = urllib.request.Request(url, headers={"User-Agent": "TitanOS release"})
    with urllib.request.urlopen(request, timeout=120) as response, open(target, "wb") as out:
        shutil.copyfileobj(response, out, 1 << 20)


def sha256(path):
    with open(path, "rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def fetch_base(source, base, work, keys):
    """Downloads every asset of the base release and verifies it like the publish job."""
    directory = work / "base"
    directory.mkdir()
    url = f"{source}/releases/download/v{base}/"
    for name in ["SHA256SUMS", "manifest.json", "manifest.json.minisig", "SHA256SUMS.minisig"]:
        download(url + name, directory / name)
    names = [line.split("  ", 1)[1] for line in (directory / "SHA256SUMS").read_text().splitlines()]
    manifest = json.loads((directory / "manifest.json").read_text())
    signatures = [f"titanos-{base}.zip.minisig"] + [p["asset"] + ".minisig" for p in manifest.get("plugins", [])]
    for name in names + signatures:
        if not (directory / name).exists():
            print("loading", name, flush=True)
            download(url + name, directory / name)
    run(sys.executable, str(REPO / "scripts/verify-release.py"), str(directory), str(keys))
    if manifest["version"] != base or manifest.get("app", {}).get("base") != base:
        raise SystemExit(f"v{base} is not a release with its own system image")
    return directory, manifest


def repack(base_dir, manifest, version, titand, work):
    base = manifest["version"]
    squashfs = base_dir / f"titanos-{base}-filesystem.squashfs"
    root = work / "rootfs"
    sudo("unsquashfs", "-q", "-no-progress", "-d", str(root), str(squashfs))
    if (root / "usr/share/titanos/base-version").read_text().strip() != base:
        raise SystemExit("the image of the base does not name its version")
    owner = [] if NO_SUDO else ["-o", "root", "-g", "root"]
    sudo("install", "-m", "0755", *owner, str(titand), str(root / "usr/bin/titand"))

    # The notices of Go modules and npm packages follow the new titand; the
    # Debian part is read from the image as mkosi.finalize does it.
    application = work / "application"
    run(sys.executable, LICENSES, "application", str(application))
    notices = root / "usr/share/titanos"
    sudo("rm", "-rf", str(notices / "licenses/go"), str(notices / "licenses/npm"))
    sudo("cp", "-r", str(application / "licenses/."), str(notices / "licenses"))
    sudo("cp", str(application / "components.json"), str(notices / "components.json"))
    sudo(sys.executable, LICENSES, "image", str(root))
    sudo("chmod", "-R", "go-w", str(notices))

    image = REPO / "build/mkosi.output"
    shutil.rmtree(image, ignore_errors=True)
    (image / "rootfs/usr/share").mkdir(parents=True)
    run("cp", "-r", str(notices), str(image / "rootfs/usr/share/titanos"))
    (image / "kernel-version").write_text(manifest["kernel"] + "\n")
    packed = work / "filesystem.squashfs"
    sudo("mksquashfs", str(root), str(packed), "-comp", "zstd", "-Xcompression-level", "19", "-b", "1M",
         "-noappend", "-no-progress", "-quiet")
    sudo("rm", "-rf", str(root))

    # The stick of the base, with the new image and version in slot a.
    stick = REPO / "out/stick"
    shutil.rmtree(stick, ignore_errors=True)
    stick.mkdir(parents=True)
    with zipfile.ZipFile(base_dir / f"titanos-{base}.zip") as archive:
        for item in archive.infolist():
            if not (stick / item.filename).resolve().is_relative_to(stick):
                raise SystemExit("unsafe path in the ZIP of the base")
        archive.extractall(stick)
    slot = stick / "titanos/a"
    for name in ["vmlinuz", "initrd.img"]:
        if sha256(slot / name) != next(f["sha256"] for f in manifest["files"] if f["name"] == name):
            raise SystemExit(f"{name} in the ZIP of the base is not the one of its manifest")
    shutil.copyfile(packed, slot / "filesystem.squashfs")
    (slot / "version").write_text(version + "\n")
    entry = (REPO / "boot/entry.conf.in").read_text().replace("@VERSION@", version).replace("@SLOT@", "a")
    entries = stick / "loader/entries"
    entries.mkdir(parents=True, exist_ok=True)
    for old in entries.glob("titanos-*.conf"):
        old.unlink()
    (entries / "titanos-a.conf").write_text(entry)

    # The plugins of the base, unchanged: they belong to its kernel.
    plugins = REPO / "out/plugins"
    shutil.rmtree(plugins, ignore_errors=True)
    plugins.mkdir(parents=True)
    for item in manifest.get("plugins", []):
        name = item["name"]
        assets = [name + ".raw", name + ".plugin.json", name + "-licenses.tar.gz"]
        if name == "nvidia":
            assets.append("nvidia-kernel-sources.tar.gz")
        # Driver plugins (since 0.32.0) bring the sources they were built from.
        if (base_dir / (name + "-sources.tar.gz")).exists():
            assets.append(name + "-sources.tar.gz")
        for asset in assets:
            shutil.copy2(base_dir / asset, plugins / asset)
    if sdk := manifest.get("nvidiaBuildSDK"):
        for asset in [sdk["asset"], "nvidia-build-sdk-sources.txt"]:
            shutil.copy2(base_dir / asset, plugins / asset)
        (plugins / "nvidia-build-sdk.json").write_text(json.dumps(sdk, indent=2) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--source", default="https://github.com/ra5on/TitanOS")
    parser.add_argument("--titand", type=Path, default=REPO / "backend/bin/titand")
    parser.add_argument("--keys", type=Path, default=REPO / "build/mkosi.extra/usr/share/titanos/update-keys",
                        help="public keys the base release must be signed with")
    args = parser.parse_args()
    for value in [args.base, args.version]:
        if not VERSION.fullmatch(value):
            raise SystemExit(f"not a version: {value}")
    if not args.titand.is_file():
        raise SystemExit(f"{args.titand} is missing, run 'make build' first")
    with tempfile.TemporaryDirectory(prefix="titanos-repack-", dir=REPO / "out" if (REPO / "out").is_dir() else None) as tmp:
        work = Path(tmp)
        base_dir, manifest = fetch_base(args.source.rstrip("/"), args.base, work, args.keys)
        repack(base_dir, manifest, args.version, args.titand.resolve(), work)
    print(f"Image of {args.base} with titand {args.version} in out/stick; next: BASE_VERSION={args.base} scripts/make-release.sh {args.version} …")


if __name__ == "__main__":
    main()
