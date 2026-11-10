#!/usr/bin/env python3
"""Which Debian packages of the latest release have updates.

App releases keep the system image of their base, Debian packages included.
This compares the packages of the published image (its sources list) with
what Debian has now and writes what is newer there. A full release brings
them: mkosi installs from trixie, trixie-updates and trixie-security.

The image with the newer kernel (kernel choice "Neu", docs/KERNEL.md) takes
kernel and firmware from trixie-backports. The sources list names its extra
packages in a section of their own; those are compared with trixie-backports
as well. Point releases count: Debian ships kernel ABI bumps and other fixes
through them, not only through trixie-security.

Usage: debian-security.py [--out FILE] [--source URL]
Prints updates=, kernel=, version= and base= for $GITHUB_OUTPUT; the list
goes to FILE.
"""
import argparse
import json
import lzma
import re
import subprocess
import sys
import time
import urllib.request
from pathlib import Path

COMPONENTS = ["main", "non-free-firmware"]
MIRROR = "https://deb.debian.org/debian/dists/{suite}/{component}/binary-amd64/Packages.xz"
SECURITY = "https://security.debian.org/debian-security/dists/{suite}/{component}/binary-amd64/Packages.xz"
# Where the packages of the default image come from, as in build/mkosi.conf.
BASE_SUITES = [(MIRROR, "trixie"), (MIRROR, "trixie-updates"), (SECURITY, "trixie-security")]
# The image with the newer kernel additionally pins kernel and firmware to backports.
VARIANT_SUITES = BASE_SUITES + [(MIRROR, "trixie-backports")]
VARIANT_HEADER = re.compile(r"Image with the new kernel .*additionally:")
OTHER_HEADER = re.compile(r"(Viewer image \(|Web apps \()")
KERNEL = re.compile(r"linux-image-")


def fetch(url):
    request = urllib.request.Request(url, headers={"User-Agent": "TitanOS security check"})
    with urllib.request.urlopen(request, timeout=120) as response:
        return response.read()


def image_sections(sources):
    """Debian packages of the default image and the extra ones of the image
    with the newer kernel, each {name: version}, from titanos-<v>-sources.txt."""
    base, variant = {}, {}
    current = base
    for line in sources.splitlines():
        if VARIANT_HEADER.fullmatch(line.strip()):
            current = variant
            continue
        # The remote view is a container of its own with its own Debian
        # packages (Chromium, GStreamer …), and the web apps follow it:
        # neither belongs to the system image.
        if OTHER_HEADER.match(line):
            current = {}
            continue
        match = re.fullmatch(r"Debian: (\S+) (\S+)", line.strip())
        if match:
            current[match[1]] = match[2]
    return base, variant


def image_packages(sources):
    """Debian binary packages and versions of the default image."""
    return image_sections(sources)[0]


def index_packages(index):
    """Package names and their newest version from a Packages file."""
    packages = {}
    for block in index.split("\n\n"):
        fields = dict(re.findall(r"^(Package|Version): (.*)$", block, re.M))
        if "Package" in fields and "Version" in fields:
            name, version = fields["Package"], fields["Version"]
            if name not in packages or newer(version, packages[name]):
                packages[name] = version
    return packages


# Former name, kept for callers of the weekly check.
security_packages = index_packages


def merge(*indexes):
    """The newest version of every package over several suites."""
    merged = {}
    for index in indexes:
        for name, version in index.items():
            if name not in merged or newer(version, merged[name]):
                merged[name] = version
    return merged


def newer(a, b):
    return subprocess.run(["dpkg", "--compare-versions", a, "gt", b]).returncode == 0


def updates(installed, available):
    return sorted((name, installed[name], available[name]) for name in installed
                  if name in available and newer(available[name], installed[name]))


def suites(entries, load):
    return merge(*(load(template.format(suite=suite, component=component))
                   for template, suite in entries for component in COMPONENTS))


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--out", type=Path, default=Path("build/debian-security.txt"))
    parser.add_argument("--source", default="https://github.com/ra5on/TitanOS")
    args = parser.parse_args()
    source = args.source.rstrip("/")
    manifest = json.loads(fetch(f"{source}/releases/latest/download/manifest.json"))
    version = manifest["version"]
    base = (manifest.get("app") or {}).get("base", version)
    installed, variant = image_sections(fetch(f"{source}/releases/download/v{version}/titanos-{version}-sources.txt").decode())
    if not installed:
        raise SystemExit(f"no Debian packages listed for {version}")
    cache = {}

    def load(url):
        if url not in cache:
            cache[url] = index_packages(lzma.decompress(fetch(url)).decode())
        return cache[url]

    found = updates(installed, suites(BASE_SUITES, load))
    found_new = updates(variant, suites(VARIANT_SUITES, load)) if variant else []
    kernel = any(KERNEL.match(name) for name, _, _ in found + found_new)
    if found or found_new:
        lines = [f"Debian updates for the image of TitanOS {base} (latest release {version}).",
                 "A full release (not an app release) brings them.",
                 # Keeps every security commit a change, even with the same list.
                 f"Checked on {time.strftime('%Y-%m-%d', time.gmtime())}.", ""]
        lines += [f"{name}: {old} -> {new}" for name, old, new in found]
        if found_new:
            lines += ["", "Image with the newer kernel (trixie-backports):"]
            lines += [f"{name}: {old} -> {new}" for name, old, new in found_new]
        args.out.parent.mkdir(parents=True, exist_ok=True)
        args.out.write_text("\n".join(lines) + "\n")
    print(f"{len(found)} of {len(installed)} Debian packages have updates, "
          f"{len(found_new)} of {len(variant)} of the image with the newer kernel", file=sys.stderr)
    print(f"updates={'true' if found or found_new else 'false'}")
    print(f"kernel={'true' if kernel else 'false'}")
    print(f"version={version}")
    print(f"base={base}")


if __name__ == "__main__":
    main()
