#!/usr/bin/env python3
"""Signs the drafts of ra5on/TitanOS-Plugins (docs/PLUGIN-REPO.md).

Runs in .github/workflows/plugin-sign.yml with the release key. Everything it
signs comes from the plugin repository as data (the assets of a draft
release); no code of the plugin repository ever runs next to the key. It does
what scripts/sets.py and scripts/nvidia-update-release.py of the plugin
repository did when they signed, with the same files, trusted comments and
formats, but checks against the public keys of this repository's image.

  sign-plugin-set.py set     <set dir> <secret key> [--keys <dir>]
      Signs every <name>.plugin.json and index.json of a set, then checks the
      set as TitanOS does. Only a set the image accepts leaves the job.

  sign-plugin-set.py pointer <set dir> <current catalog dir or -> <output dir> <secret key> [--keys <dir>]
      Writes catalog.json with the set, serial one higher than the current
      (whose signature must be trusted), and signs it.

  sign-plugin-set.py nvidia  <draft dir> <secret key> [--keys <dir>] [--source <url>]
      Signs the one NVIDIA driver update of a draft and the new
      nvidia-updates.json of its kernel set (read from the published set),
      then checks every signature and checksum.

MINISIGN_PASSWORD, if set, is passed to minisign on standard input.
"""
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
KEYS = REPO / "build/mkosi.extra/usr/share/titanos/update-keys"
SOURCE = "https://github.com/ra5on/TitanOS-Plugins"
MAX_KERNELS = 64
NAME = re.compile(r"[a-z0-9][a-z0-9-]{0,40}")
HASH = re.compile(r"[0-9a-f]{64}")
KERNEL_TAG = re.compile(r"kernel-[0-9][0-9A-Za-z._~-]*-[1-9][0-9]*")
ASSET = re.compile(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,120}")


def digest(path):
    with open(path, "rb") as f:
        return hashlib.file_digest(f, "sha256").hexdigest()


def minisign_sign(path, key, comment=None):
    command = ["minisign", "-S", "-s", str(key), "-m", str(path)]
    if comment is not None:
        command += ["-t", comment]
    password = os.environ.get("MINISIGN_PASSWORD")
    subprocess.run(command, input=None if password is None else password + "\n", text=True, check=True,
                   stdout=subprocess.DEVNULL)


def verified(path, keys):
    for key in sorted(Path(keys).glob("*.pub")):
        if subprocess.run(["minisign", "-V", "-q", "-p", str(key), "-m", str(path)],
                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
            return True
    return False


# Plugin sets (common-<n>, kernel-<package>-<n>).

def set_sign(path, key):
    minisign_sign(path, key, "TitanOS plugins: " + path.name)


def item(directory, name):
    if not NAME.fullmatch(name):
        raise SystemExit(f"{name!r}: invalid plugin name")
    manifest_path = directory / (name + ".plugin.json")
    manifest = json.loads(manifest_path.read_text())
    if manifest["name"] != name or digest(directory / (name + ".raw")) != manifest["sha256"]:
        raise SystemExit(f"{name}: manifest and image do not match")
    return {"name": name, "version": manifest["version"], "manifest": digest(manifest_path),
            "sha256": manifest["sha256"], "size": manifest["size"]}


def check_set(directory, keys):
    """What sets.py verify checked: the index signature, every asset against
    the index and every plugin manifest against signature and index."""
    if not verified(directory / "index.json", keys):
        raise SystemExit("index.json: untrusted signature")
    index = json.loads((directory / "index.json").read_text())
    for asset in index["assets"]:
        if not ASSET.fullmatch(asset["name"]):
            raise SystemExit(f"{asset['name']!r}: invalid asset name")
        path = directory / asset["name"]
        if digest(path) != asset["sha256"] or path.stat().st_size != asset["size"]:
            raise SystemExit(f"{asset['name']}: does not match the index")
    for entry in index["plugins"]:
        path = directory / (entry["name"] + ".plugin.json")
        if not NAME.fullmatch(entry["name"]) or not verified(path, keys) \
                or item(directory, entry["name"]) | {k: entry[k] for k in ("inputs",) if k in entry} != entry:
            raise SystemExit(f"{entry['name']}: signature or hashes")
    print(f"{index['tag']}: verified")
    return index


def sign_set(directory, key, keys):
    directory = Path(directory)
    for path in sorted(directory.glob("*.plugin.json")) + [directory / "index.json"]:
        set_sign(path, key)
    check_set(directory, keys)


def pointer(directory, current, output, key, keys):
    """catalog.json with the new set, serial one higher than the current."""
    directory, output = Path(directory), Path(output)
    index = json.loads((directory / "index.json").read_text())
    ref = {"tag": index["tag"], "index": digest(directory / "index.json")}
    catalog = {"serial": 0, "debian": index["debian"], "sysextLevel": index["sysextLevel"], "common": None, "kernels": []}
    if current != "-":
        path = Path(current) / "catalog.json"
        if not verified(path, keys):
            raise SystemExit("current catalog.json: untrusted signature")
        catalog = json.loads(path.read_text())
    if (catalog["debian"], catalog["sysextLevel"]) != (index["debian"], index["sysextLevel"]):
        # Another Debian release or sysext level starts a new catalog.
        catalog.update(debian=index["debian"], sysextLevel=index["sysextLevel"], common=None, kernels=[])
    if index["kernel"]:
        ref = {"kernel": index["kernel"], "kernelPackage": index["kernelPackage"]} | ref
        catalog["kernels"] = [k for k in catalog["kernels"]
                              if (k["kernel"], k["kernelPackage"]) != (index["kernel"], index["kernelPackage"])]
        catalog["kernels"] = (catalog["kernels"] + [ref])[-MAX_KERNELS:]
    else:
        catalog["common"] = ref
    catalog["serial"] += 1
    output.mkdir(parents=True, exist_ok=True)
    (output / "catalog.json").write_text(json.dumps(catalog, indent=2) + "\n")
    set_sign(output / "catalog.json", key)
    if not verified(output / "catalog.json", keys):
        raise SystemExit("catalog.json: the key does not belong to the image")
    print(f"catalog.json serial {catalog['serial']}")


# NVIDIA driver updates (draft pending-nvidia-<run>-<kernel set>).

def fetch(base, name, limit=65536, optional=False):
    try:
        with urllib.request.urlopen(base + name, timeout=120) as response:
            if base.startswith("https://") and not response.url.startswith("https://"):
                raise SystemExit("insecure redirect")
            data = response.read(limit + 1)
    except urllib.error.HTTPError as error:
        if optional and error.code == 404:
            return None
        raise
    if len(data) > limit:
        raise SystemExit("metadata too large")
    return data


def nvidia_verify(path, keys):
    if not verified(path, keys):
        raise SystemExit("untrusted signature: " + path.name)


def channel(base, kernel, package, keys):
    """The published nvidia-updates.json of a kernel set, or an empty one."""
    data = fetch(base, "nvidia-updates.json", optional=True)
    if data is None:
        return {"titanos": "", "kernel": kernel, "kernelPackage": package, "versions": []}
    signature = fetch(base, "nvidia-updates.json.minisig", 4096)
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "nvidia-updates.json"
        path.write_bytes(data)
        path.with_name(path.name + ".minisig").write_bytes(signature)
        nvidia_verify(path, keys)
    data = json.loads(data)
    if data.get("titanos") != "" or data.get("kernel") != kernel or data.get("kernelPackage") != package \
            or len(data.get("versions", [])) > 32:
        raise SystemExit("wrong driver channel")
    seen, revisions = set(), set()
    for entry in data["versions"]:
        if not HASH.fullmatch(entry["sha256"]) or entry["sha256"] in seen or type(entry["revision"]) is not int \
                or entry["revision"] < 1 or entry["revision"] in revisions:
            raise SystemExit("invalid driver inventory")
        seen.add(entry["sha256"])
        revisions.add(entry["revision"])
    return data


def verify_package(directory, manifest):
    sha = manifest["sha256"]
    if not HASH.fullmatch(sha):
        raise SystemExit("invalid image hash")
    image = directory / ("nvidia-update-" + sha + ".raw")
    if digest(image) != sha or image.stat().st_size != manifest["size"]:
        raise SystemExit("driver image mismatch")
    expected = {"nvidia-update-" + sha + "-" + n + ".tar.gz" for n in ("licenses", "kernel-sources")}
    if set(manifest.get("archives", {})) != expected:
        raise SystemExit("missing source/license archives")
    for name, info in manifest["archives"].items():
        if digest(directory / name) != info["sha256"] or (directory / name).stat().st_size != info["size"]:
            raise SystemExit("archive checksum mismatch")


def nvidia_sign(path, key, keys):
    minisign_sign(path, key)
    nvidia_verify(path, keys)


def version_key(entry):
    return (tuple(int(n) for n in entry["driver"].split(".")), tuple(int(n) for n in entry["toolkit"].split(".")),
            entry["revision"])


def sign_nvidia(directory, key, keys, source):
    directory = Path(directory)
    manifests = list(directory.glob("nvidia-update-*.plugin.json"))
    if len(manifests) != 1:
        raise SystemExit("expected exactly one new plugin")
    path = manifests[0]
    plugin = json.loads(path.read_text())
    target = json.loads((directory / "target.json").read_text())
    if not KERNEL_TAG.fullmatch(target["tag"]) or plugin["kernel"] != target["kernel"] or plugin.get("titanos"):
        raise SystemExit("invalid target")
    if path.name != "nvidia-update-" + plugin["sha256"] + ".plugin.json":
        raise SystemExit("driver filename mismatch")
    verify_package(directory, plugin)
    releases = source.rstrip("/") + "/releases/download/"
    previous = channel(releases + target["tag"] + "/", target["kernel"], target["kernelPackage"], keys)
    if any(v["revision"] >= plugin["revision"] for v in previous["versions"]):
        raise SystemExit("revision must increase; start a new workflow run")
    nvidia_sign(path, key, keys)
    previous["versions"].append({k: plugin[k] for k in ("version", "sha256", "revision", "size", "driver", "toolkit")})
    previous["versions"] = sorted(previous["versions"], key=lambda v: v["revision"], reverse=True)[:32]
    previous["recommended"] = max(previous["versions"], key=version_key)["sha256"]
    index = directory / "nvidia-updates.json"
    index.write_text(json.dumps(previous, indent=2) + "\n")
    nvidia_sign(index, key, keys)
    verify_nvidia(directory, keys)


def verify_nvidia(directory, keys):
    """The individually authenticated driver update assets of a directory."""
    assets = set()
    index = directory / "nvidia-updates.json"
    if index.exists():
        nvidia_verify(index, keys)
        assets.update({index.name, index.name + ".minisig"})
    for path in directory.glob("nvidia-update-*.plugin.json"):
        nvidia_verify(path, keys)
        manifest = json.loads(path.read_text())
        if path.name != "nvidia-update-" + manifest["sha256"] + ".plugin.json":
            raise SystemExit("driver filename mismatch")
        verify_package(directory, manifest)
        assets.update({path.name, path.name + ".minisig", "nvidia-update-" + manifest["sha256"] + ".raw",
                       *manifest["archives"]})
    print(f"{directory.name}: {len(assets)} driver update assets verified")
    return assets


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("set")
    p.add_argument("directory")
    p.add_argument("key")
    p.add_argument("--keys", default=str(KEYS))
    p = sub.add_parser("pointer")
    p.add_argument("directory")
    p.add_argument("current")
    p.add_argument("output")
    p.add_argument("key")
    p.add_argument("--keys", default=str(KEYS))
    p = sub.add_parser("nvidia")
    p.add_argument("directory")
    p.add_argument("key")
    p.add_argument("--keys", default=str(KEYS))
    p.add_argument("--source", default=SOURCE)
    args = parser.parse_args()
    if args.command == "set":
        sign_set(args.directory, args.key, args.keys)
    elif args.command == "pointer":
        pointer(args.directory, args.current, args.output, args.key, args.keys)
    else:
        sign_nvidia(args.directory, args.key, args.keys, args.source)
    return 0


if __name__ == "__main__":
    sys.exit(main())
