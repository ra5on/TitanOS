#!/usr/bin/env python3
"""Which Debian packages of the latest release have security updates.

App releases keep the system image of their base, Debian packages included.
This compares the packages of the published image (its sources list) with
trixie-security and writes what is newer there. A full release brings them:
mkosi installs from trixie-security as well.

Usage: debian-security.py [--out FILE] [--source URL]
Prints updates=true|false for $GITHUB_OUTPUT; the list goes to FILE.
"""
import argparse
import json
import lzma
import re
import subprocess
import sys
import urllib.request
from pathlib import Path

SECURITY = "https://security.debian.org/debian-security/dists/trixie-security/{}/binary-amd64/Packages.xz"
COMPONENTS = ["main", "non-free-firmware"]


def fetch(url):
    request = urllib.request.Request(url, headers={"User-Agent": "TitanOS security check"})
    with urllib.request.urlopen(request, timeout=120) as response:
        return response.read()


def image_packages(sources):
    """Debian binary packages and versions from titanos-<version>-sources.txt."""
    packages = {}
    for line in sources.splitlines():
        match = re.fullmatch(r"Debian: (\S+) (\S+)", line.strip())
        if match:
            packages[match[1]] = match[2]
    return packages


def security_packages(index):
    """Package names and versions from a Packages file."""
    packages = {}
    for block in index.split("\n\n"):
        fields = dict(re.findall(r"^(Package|Version): (.*)$", block, re.M))
        if "Package" in fields and "Version" in fields:
            name, version = fields["Package"], fields["Version"]
            if name not in packages or newer(version, packages[name]):
                packages[name] = version
    return packages


def newer(a, b):
    return subprocess.run(["dpkg", "--compare-versions", a, "gt", b]).returncode == 0


def updates(installed, security):
    return sorted((name, installed[name], security[name]) for name in installed
                  if name in security and newer(security[name], installed[name]))


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--out", type=Path, default=Path("build/debian-security.txt"))
    parser.add_argument("--source", default="https://github.com/ra5on/TitanOS")
    args = parser.parse_args()
    source = args.source.rstrip("/")
    manifest = json.loads(fetch(f"{source}/releases/latest/download/manifest.json"))
    version = manifest["version"]
    base = (manifest.get("app") or {}).get("base", version)
    installed = image_packages(fetch(f"{source}/releases/download/v{version}/titanos-{version}-sources.txt").decode())
    if not installed:
        raise SystemExit(f"no Debian packages listed for {version}")
    security = {}
    for component in COMPONENTS:
        security.update(security_packages(lzma.decompress(fetch(SECURITY.format(component))).decode()))
    found = updates(installed, security)
    if found:
        lines = [f"Security updates in trixie-security for the image of TitanOS {base} (latest release {version}).",
                 "A full release (not an app release) brings them; merging this file makes the next tag one.", ""]
        lines += [f"{name}: {old} -> {new}" for name, old, new in found]
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text("\n".join(lines) + "\n")
    print(f"{len(found)} of {len(installed)} Debian packages have security updates", file=sys.stderr)
    print(f"updates={'true' if found else 'false'}")


if __name__ == "__main__":
    main()
