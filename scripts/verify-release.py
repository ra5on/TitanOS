#!/usr/bin/env python3
"""Verify signatures, complete asset checksums and the update manifest before publishing."""
import importlib.util
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path


def newer_series(kernel, than):
    """Whether kernel is of a newer series (major.minor) than than; backports
    kernels say "bpo" only in their package version, not in uname."""
    def series(release):
        match = re.match(r"(\d+)\.(\d+)", str(release))
        return (int(match[1]), int(match[2])) if match else (0, 0)
    return series(kernel) > series(than)


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


# The official plugins are those the image has catalog entries for; they are
# built in ra5on/TitanOS-Plugins (docs/PLUGIN-REPO.md).
OFFICIAL = {p.stem for p in (Path(__file__).resolve().parent.parent / "plugins/catalog").glob("*.json")}
# The web apps a release of this repository carries (PLAN 66, docs/WEBAPPS.md).
CATALOG = Path(__file__).resolve().parent.parent / "webapps/catalog.json"


def verify_base_webapps(webapps, release, checksums):
    """The web apps of an earlier release that serves as the base of an app
    release: checked against that release's own signed checksums, not against
    today's catalog, which may have moved on (PLAN 76, F-13-3)."""
    seen = set()
    if not isinstance(webapps, list):
        raise ValueError("web apps of the base are no list")
    for entry in webapps:
        if not isinstance(entry, dict) or set(entry) != {"id", "version", "asset", "sha256", "size"}:
            raise ValueError("web app entry of the base is malformed")
        asset = entry["asset"]
        if not isinstance(asset, str) or not re.fullmatch(r"titanos-webapp-[a-z0-9-]+-[0-9A-Za-z.+-]+\.zip", asset) \
                or asset in seen or checksums.get(asset) != entry["sha256"] \
                or (release / asset).stat().st_size != entry["size"]:
            raise ValueError("web app mismatch in the base: " + str(asset))
        seen.add(asset)


def verify_webapps(manifest, release, checksums, as_base=False):
    """Every app of the catalog, unchanged upstream archive (the catalog's
    SHA-256 and size), listed in the checksums; optional for older releases."""
    webapps = manifest.get("webapps")
    if webapps is None:
        return
    if as_base:
        return verify_base_webapps(webapps, release, checksums)
    catalog = json.loads(CATALOG.read_text())["apps"]
    if not isinstance(webapps, list) or [e.get("id") for e in webapps if isinstance(e, dict)] != [a["id"] for a in catalog]:
        raise ValueError("web apps do not match the catalog")
    for entry, app in zip(webapps, catalog):
        asset = f"titanos-webapp-{app['id']}-{app['version']}.zip"
        if set(entry) != {"id", "version", "asset", "sha256", "size"} or entry["version"] != app["version"] \
                or entry["asset"] != asset or entry["sha256"] != app["sha256"] or entry["size"] != app["size"] \
                or checksums.get(asset) != entry["sha256"] or (release / asset).stat().st_size != entry["size"]:
            raise ValueError("web app mismatch: " + asset)


def verify(release, keys, without_plugins=False, as_base=False):
    manifest = json.loads((release / "manifest.json").read_text())
    version = manifest["version"]
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9.]+)?", version):
        raise ValueError("invalid release version")
    signed = ["manifest.json", f"titanos-{version}.zip", "SHA256SUMS"]
    plugins = manifest.get("plugins", [])
    names = set()
    for plugin in plugins:
        name = plugin.get("name")
        if name not in OFFICIAL or name in names or plugin.get("asset") != name + ".plugin.json":
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
    # The same system with the newer kernel (docs/KERNEL.md), optional.
    variants = manifest.get("variants", {})
    if set(variants) - {"new"}:
        raise ValueError("unknown kernel variant")
    if new := variants.get("new"):
        if not newer_series(new.get("kernel", ""), manifest["kernel"]):
            raise ValueError("the new kernel is not a backports kernel")
        if sorted(file["name"] for file in new.get("files", [])) != ["filesystem.squashfs", "initrd.img", "vmlinuz"]:
            raise ValueError("the variant must describe exactly one complete system slot")
        for file in new["files"]:
            asset = f"titanos-{version}-new-{file['name']}"
            if file["asset"] != asset or checksums.get(asset) != file["sha256"] or (release / asset).stat().st_size != file["size"]:
                raise ValueError(f"manifest mismatch: {asset}")
    # titand alone, for an app update on the image it was made on.
    app = manifest.get("app")
    if not isinstance(app, dict) or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9.]+)?", str(app.get("base", ""))):
        raise ValueError("app of the release missing or without base version")
    base = app["base"]
    asset = f"titanos-{version}-titand"
    if app.get("asset") != asset or checksums.get(asset) != app.get("sha256") or (release / asset).stat().st_size != app.get("size"):
        raise ValueError("app mismatch: " + asset)
    for item in plugins:
        name = item["name"]
        plugin = json.loads((release / item["asset"]).read_text())
        # A plugin set's version with the base it is carried for; plugins of a base
        # before the plugin repository carry the base version itself.
        correct_version = plugin["version"].endswith("-titanos-" + base) or (name != "nvidia" and plugin["version"] == base)
        if plugin["name"] != name or (plugin.get("kernel") != manifest["kernel"] if name == "nvidia" else plugin.get("kernel", "") not in ("", manifest["kernel"])) or not correct_version:
            raise ValueError("plugin kernel/version mismatch: " + name)
        if checksums[name + ".raw"] != plugin["sha256"] or (release / (name + ".raw")).stat().st_size != plugin["size"]:
            raise ValueError("plugin image mismatch: " + name)
        required = {name + "-licenses.tar.gz"}
        if name == "nvidia":
            required.add("nvidia-kernel-sources.tar.gz")
        if not required <= checksums.keys():
            raise ValueError("plugin source/license archive missing: " + name)
    # The image of the remote view (docs/FERNANSICHT.md), optional for older releases.
    if (viewer := manifest.get("viewer")) is not None:
        asset = f"titanos-viewer-{version}.tar.zst"
        if not isinstance(viewer, dict) or viewer.get("asset") != asset or checksums.get(asset) != viewer.get("sha256") \
                or (release / asset).stat().st_size != viewer.get("size") \
                or not re.fullmatch(r"sha256:[0-9a-f]{64}", str(viewer.get("imageId", ""))):
            raise ValueError("viewer image mismatch: " + asset)
    verify_webapps(manifest, release, checksums, as_base)
    if sdk := manifest.get("nvidiaBuildSDK"):
        if sdk.get("kernel") != manifest["kernel"] or sdk.get("version") != base or sdk.get("asset") != "nvidia-build-sdk.tar.zst":
            raise ValueError("SDK target mismatch")
        if checksums.get(sdk["asset"]) != sdk["sha256"] or (release / sdk["asset"]).stat().st_size != sdk["size"] or "nvidia-build-sdk-sources.txt" not in checksums:
            raise ValueError("SDK integrity/sources mismatch")
    return version


if __name__ == "__main__":
    args = sys.argv[1:]
    without_plugins = "--without-plugins" in args
    if without_plugins:
        args.remove("--without-plugins")
    # --base: an earlier release used as the base of an app release.
    as_base = "--base" in args
    if as_base:
        args.remove("--base")
    if len(args) != 2:
        sys.exit("usage: verify-release.py [--without-plugins] [--base] <release directory> <public keys directory>")
    try:
        print("Verified TitanOS", verify(Path(args[0]), Path(args[1]), without_plugins, as_base))
    except (ValueError, OSError, KeyError) as error:
        sys.exit(str(error))
