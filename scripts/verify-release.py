#!/usr/bin/env python3
"""Verify signatures, complete asset checksums and the update manifest before publishing."""
import importlib.util
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def verify(release, keys, without_plugins=False):
    manifest = json.loads((release / "manifest.json").read_text())
    version = manifest["version"]
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9.]+)?", version):
        raise ValueError("invalid release version")
    signed = ["manifest.json", f"titanos-{version}.zip", "SHA256SUMS"]
    plugins = manifest.get("plugins", [])
    names = set()
    for plugin in plugins:
        name = plugin.get("name")
        if name not in {"nvidia", "intel-gpu-tools", "coral-usb", "tailscale"} or name in names or plugin.get("asset") != name + ".plugin.json":
            raise ValueError("invalid plugin inventory")
        names.add(name)
        signed.append(name + ".plugin.json")
    # A release without the matching NVIDIA plugin is an image rehearsal only.
    if not without_plugins and ("nvidia" not in names or "nvidiaBuildSDK" not in manifest):
        raise ValueError("NVIDIA plugin or build SDK missing (image-only rehearsal?)")
    # Every signature must verify using the same trusted key.
    for key in sorted(keys.glob("*.pub")):
        if all(subprocess.run(["minisign", "-V", "-q", "-p", str(key), "-m", str(release / name)],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0 for name in signed):
            break
    else:
        raise ValueError("manifest, ZIP and checksums need signatures from the same trusted key")
    checksums = {}
    for line in (release / "SHA256SUMS").read_text().splitlines():
        match = re.fullmatch(r"([a-f0-9]{64})  ([a-zA-Z0-9][a-zA-Z0-9._-]*)", line)
        if not match or match[2] in checksums:
            raise ValueError("invalid or duplicate checksum entry")
        checksums[match[2]] = match[1]
    # Independent drivers have their own signed inventory and immutable assets.
    # Authenticate every additional image/archive before excluding it from the
    # original OS checksum list, which is deliberately never replaced.
    spec = importlib.util.spec_from_file_location("nvidia_release", Path(__file__).with_name("nvidia-update-release.py"))
    driver_release = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(driver_release)
    extra = driver_release.verify_directory(release, keys)
    expected = {p.name for p in release.iterdir() if p.is_file()} - extra - {"SHA256SUMS", *(name + ".minisig" for name in signed)}
    if set(checksums) != expected:
        raise ValueError("checksum inventory does not match release assets")
    for name, checksum in checksums.items():
        if digest(release / name) != checksum:
            raise ValueError(f"checksum mismatch: {name}")
    if manifest["tag"] != "v" + version or manifest["channel"] != ("beta" if "-" in version else "stable"):
        raise ValueError("tag or channel does not match version")
    if {file["name"] for file in manifest["files"]} != {"vmlinuz", "initrd.img", "filesystem.squashfs"} or len(manifest["files"]) != 3:
        raise ValueError("manifest must describe exactly one complete system slot")
    for file in manifest["files"]:
        asset = f"titanos-{version}-{file['name']}"
        if file["asset"] != asset or checksums[asset] != file["sha256"] or (release / asset).stat().st_size != file["size"]:
            raise ValueError(f"manifest mismatch: {asset}")
    for item in plugins:
        name = item["name"]
        plugin = json.loads((release / item["asset"]).read_text())
        correct_version = plugin["version"].endswith("-titanos-" + version) if name == "nvidia" else plugin["version"] == version
        if plugin["name"] != name or (plugin.get("kernel") != manifest["kernel"] if name == "nvidia" else plugin.get("kernel", "") not in ("", manifest["kernel"])) or not correct_version:
            raise ValueError("plugin kernel/version mismatch: " + name)
        if checksums[name + ".raw"] != plugin["sha256"] or (release / (name + ".raw")).stat().st_size != plugin["size"]:
            raise ValueError("plugin image mismatch: " + name)
        required = {name + "-licenses.tar.gz"}
        if name == "nvidia":
            required.add("nvidia-kernel-sources.tar.gz")
        if not required <= checksums.keys():
            raise ValueError("plugin source/license archive missing: " + name)
    if sdk := manifest.get("nvidiaBuildSDK"):
        if sdk.get("kernel") != manifest["kernel"] or sdk.get("version") != version or sdk.get("asset") != "nvidia-build-sdk.tar.zst":
            raise ValueError("SDK target mismatch")
        if checksums.get(sdk["asset"]) != sdk["sha256"] or (release / sdk["asset"]).stat().st_size != sdk["size"] or "nvidia-build-sdk-sources.txt" not in checksums:
            raise ValueError("SDK integrity/sources mismatch")
    return version


if __name__ == "__main__":
    args = sys.argv[1:]
    without_plugins = "--without-plugins" in args
    if without_plugins:
        args.remove("--without-plugins")
    if len(args) != 2:
        sys.exit("usage: verify-release.py [--without-plugins] <release directory> <public keys directory>")
    try:
        print("Verified TitanOS", verify(Path(args[0]), Path(args[1]), without_plugins))
    except (ValueError, OSError, KeyError) as error:
        sys.exit(str(error))
