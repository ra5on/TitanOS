#!/usr/bin/env python3
"""The plugin sets of ra5on/TitanOS-Plugins for a release (docs/PLUGIN-REPO.md).

  plugin-set.py fetch --base <version> --kernel <uname -r> --kernel-package <version>
                      --dest <dir> [--keys <dir>] [--source <url>]
      Downloads the kernel set and the common set the signed catalog names for
      this exact kernel into <dir>/kernel and <dir>/common and checks every
      signature and checksum. Exit status 3: the catalog has no set for this
      kernel yet (the plugin repository builds it on request).

  plugin-set.py package <image dir>
      Prints the Debian version of the image's kernel package.

  plugin-set.py import --base <version> [--keys <dir>] [--out <dir>] <set dir>...
      Puts the plugins of the sets into out/plugins for make-release.sh. Each
      manifest gets the version <set version>-titanos-<base>, which TitanOS up to
      0.35 requires, and is signed again with the release (sign-release.sh). The
      images stay byte for byte those of the set.
"""
import argparse
import hashlib
import json
import re
import shutil
import subprocess
import time
import sys
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
KEYS = REPO / "build/mkosi.extra/usr/share/titanos/update-keys"
SOURCE = "https://github.com/ra5on/TitanOS-Plugins"
VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.]+)?")
HASH = re.compile(r"[0-9a-f]{64}")
NAME = re.compile(r"[a-z0-9][a-z0-9-]{0,40}")
TAG = re.compile(r"(common|kernel-[0-9][0-9A-Za-z._~-]*)-[1-9][0-9]*")
NOT_FOUND = 3


def digest(path):
    with open(path, "rb") as f:
        return hashlib.file_digest(f, "sha256").hexdigest()


def verified(path, keys):
    for key in sorted(Path(keys).glob("*.pub")):
        if subprocess.run(["minisign", "-V", "-q", "-p", str(key), "-m", str(path)],
                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
            return True
    return False


def numbers(version):
    return tuple(int(n) for n in re.findall(r"[0-9]+", version.split("-")[0]))


def download(url, dest, limit=None, attempts=4, pause=5):
    """Downloads url to dest; False if it does not exist (404). A server
    error or a broken connection is tried again (GitHub answered a release
    download of 0.45.0 once with 500)."""
    for attempt in range(1, attempts + 1):
        try:
            with urllib.request.urlopen(url, timeout=300) as response, open(dest, "wb") as out:
                if not response.url.startswith("https://") and url.startswith("https://"):
                    raise SystemExit("insecure redirect: " + url)
                total = 0
                while block := response.read(1 << 20):
                    total += len(block)
                    if limit and total > limit:
                        raise SystemExit("larger than expected: " + url)
                    out.write(block)
            return True
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return False
            if error.code < 500 or attempt == attempts:
                raise
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            if attempt == attempts:
                raise
        print(f"download failed, trying again ({attempt}/{attempts - 1}): {url}", file=sys.stderr, flush=True)
        time.sleep(pause * attempt)
    return False


def check_index(directory, keys):
    """The signed index of a set and the files it lists."""
    if not verified(directory / "index.json", keys):
        raise SystemExit(f"{directory}: untrusted index signature")
    index = json.loads((directory / "index.json").read_text())
    if not TAG.fullmatch(index.get("tag", "")):
        raise SystemExit(f"{directory}: index without a valid tag")
    for asset in index["assets"]:
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,120}", asset["name"]) or not HASH.fullmatch(asset["sha256"]):
            raise SystemExit(f"{directory}: invalid asset entry")
        path = directory / asset["name"]
        if digest(path) != asset["sha256"] or path.stat().st_size != asset["size"]:
            raise SystemExit(f"{asset['name']}: does not match the index of {index['tag']}")
    for item in index["plugins"]:
        manifest = directory / (item["name"] + ".plugin.json")
        if not NAME.fullmatch(item["name"]) or not verified(manifest, keys) or digest(manifest) != item["manifest"]:
            raise SystemExit(f"{item['name']}: manifest signature or checksum")
        data = json.loads(manifest.read_text())
        if data["name"] != item["name"] or data["version"] != item["version"] or data["sha256"] != item["sha256"] \
                or digest(directory / (item["name"] + ".raw")) != item["sha256"] or data.get("kernel", "") != index["kernel"]:
            raise SystemExit(f"{item['name']}: does not match the index of {index['tag']}")
    return index


def fetch(args):
    keys, base = Path(args.keys), args.base
    releases = args.source.rstrip("/") + "/releases/download/"
    dest = Path(args.dest)
    shutil.rmtree(dest, ignore_errors=True)
    dest.mkdir(parents=True)
    pointer = dest / "catalog.json"
    if not download(releases + "catalog/catalog.json", pointer, 64 << 10):
        print("the plugin repository has no catalog yet", file=sys.stderr)
        return NOT_FOUND
    download(releases + "catalog/catalog.json.minisig", dest / "catalog.json.minisig", 4096)
    if not verified(pointer, keys):
        raise SystemExit("catalog.json: untrusted signature")
    catalog = json.loads(pointer.read_text())
    refs = {"kernel": next((k for k in catalog["kernels"] if k["kernel"] == args.kernel
                            and k["kernelPackage"] == args.kernel_package), None),
            "common": catalog.get("common")}
    if refs["kernel"] is None:
        print(f"no kernel set for {args.kernel} ({args.kernel_package}) yet", file=sys.stderr)
        return NOT_FOUND
    # The signer publishes the kernel set before the common set; a release
    # waits for both instead of going out without the common plugins.
    if refs["common"] is None:
        print("no common set yet", file=sys.stderr)
        return NOT_FOUND
    for kind, ref in refs.items():
        if not TAG.fullmatch(ref["tag"]) or not HASH.fullmatch(ref["index"]):
            raise SystemExit("catalog.json: invalid entry")
        directory = dest / kind
        directory.mkdir()
        base_url = releases + ref["tag"] + "/"
        for name in ("index.json", "index.json.minisig"):
            if not download(base_url + name, directory / name, 1 << 20):
                raise SystemExit(f"{ref['tag']}: {name} missing")
        if digest(directory / "index.json") != ref["index"]:
            raise SystemExit(f"{ref['tag']}: index does not match the catalog")
        index = json.loads((directory / "index.json").read_text())
        if numbers(index["minBase"]) > numbers(base):
            raise SystemExit(f"{ref['tag']} is meant for TitanOS {index['minBase']} and newer")
        for asset in index["assets"]:
            if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]{0,120}", asset["name"]) or not download(base_url + asset["name"], directory / asset["name"]):
                raise SystemExit(f"{ref['tag']}: {asset['name']} missing")
        for item in index["plugins"]:
            name = item["name"] + ".plugin.json.minisig"
            if not NAME.fullmatch(item["name"]) or not download(base_url + name, directory / name, 4096):
                raise SystemExit(f"{ref['tag']}: {name} missing")
        check_index(directory, keys)
        print(f"{ref['tag']}: {', '.join(p['name'] for p in index['plugins'])}")
    return 0


def import_sets(args):
    if not VERSION.fullmatch(args.base):
        raise SystemExit("invalid base version")
    out = Path(args.out)
    shutil.rmtree(out, ignore_errors=True)
    out.mkdir(parents=True)
    seen = set()
    for directory in map(Path, args.sets):
        index = check_index(directory, args.keys) if args.keys else json.loads((directory / "index.json").read_text())
        for item in index["plugins"]:
            name = item["name"]
            if name in seen:
                raise SystemExit(f"{name} is in more than one set")
            seen.add(name)
            manifest = json.loads((directory / (name + ".plugin.json")).read_text())
            manifest["version"] = f"{manifest['version']}-titanos-{args.base}"
            (out / (name + ".plugin.json")).write_text(json.dumps(manifest, indent=2, ensure_ascii=False) + "\n")
            for suffix in (".raw", "-licenses.tar.gz", "-sources.tar.gz"):
                if (directory / (name + suffix)).exists():
                    shutil.copy2(directory / (name + suffix), out / (name + suffix))
        for name in ("nvidia-kernel-sources.tar.gz", "nvidia-build-sdk.tar.zst", "nvidia-build-sdk-sources.txt"):
            if (directory / name).exists():
                shutil.copy2(directory / name, out / name)
        if (directory / "nvidia-build-sdk.json").exists():
            # The SDK fits every base with this kernel; titand passes its own
            # version to the build (local_build.go).
            sdk = json.loads((directory / "nvidia-build-sdk.json").read_text())
            sdk["version"] = args.base
            (out / "nvidia-build-sdk.json").write_text(json.dumps(sdk, indent=2) + "\n")
        print(f"{index['tag']}: {', '.join(p['name'] for p in index['plugins'])} -> {out}")
    return 0


def kernel_package(image):
    image = Path(image)
    kernel = (image / "kernel-version").read_text().strip()
    for block in (image / "rootfs/var/lib/dpkg/status").read_text().split("\n\n"):
        fields = dict(re.findall(r"^(Package|Version|Status): (.*)$", block, re.M))
        if fields.get("Package") == "linux-image-" + kernel and fields.get("Status") == "install ok installed":
            return fields["Version"]
    raise SystemExit("linux-image-" + kernel + " is not installed in the image")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("fetch")
    p.add_argument("--base", required=True)
    p.add_argument("--kernel", required=True)
    p.add_argument("--kernel-package", required=True)
    p.add_argument("--dest", required=True)
    p.add_argument("--keys", default=str(KEYS))
    p.add_argument("--source", default=SOURCE)
    p = sub.add_parser("package")
    p.add_argument("image")
    p = sub.add_parser("import")
    p.add_argument("--base", required=True)
    p.add_argument("--keys", default="")
    p.add_argument("--out", default=str(REPO / "out/plugins"))
    p.add_argument("sets", nargs="+")
    args = parser.parse_args()
    if args.command == "package":
        print(kernel_package(args.image))
        return 0
    return fetch(args) if args.command == "fetch" else import_sets(args)


if __name__ == "__main__":
    sys.exit(main())
