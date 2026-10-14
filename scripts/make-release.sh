#!/bin/sh
# Turns the stick built by make-zip.sh into the files of a release:
#
#   out/release/titanos-<version>.zip (+ .sha256)   everything for a new stick
#   out/release/titanos-<version>-vmlinuz           \
#   out/release/titanos-<version>-initrd.img         } one system slot, for updates
#   out/release/titanos-<version>-filesystem.squashfs /
#   out/release/titanos-<version>-sources.txt       where the sources of the Debian packages are
#   out/release/manifest.json                       what an update consists of
#
# manifest.json is what gets signed (minisign -S -m manifest.json): a NAS
# installs an update only if that signature is from a key in its image.
#
# Usage: scripts/make-release.sh <version> <stable|beta> [changelog file]
#   MIN_VERSION=x.y.z   oldest version that may update to this one directly
#   ALLOW_DEV=1         accept a development build; for tests only
#
# Expects out/stick and build/mkosi.output from "make zip" with the same VERSION.
set -eu

cd "$(dirname "$0")/.."

version=${1:-}
channel=${2:-}
changelog=${3:-}
case "$channel" in
    stable|beta) ;;
    *) echo "usage: $0 <version> <stable|beta> [changelog file]" >&2; exit 2 ;;
esac
if ! printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9.]+)?$'; then
    echo "the version must look like 1.2.3 or 1.2.3-beta.1, not '$version'" >&2
    exit 2
fi

stick=out/stick
slot=$stick/titanos/a
image=build/mkosi.output
if [ "$(cat "$slot/version" 2>/dev/null)" != "$version" ]; then
    echo "$stick does not hold version $version; run 'make zip VERSION=$version' first" >&2
    exit 1
fi
# A stick for testing logs root in on the console. It must never be released
# (ALLOW_DEV=1 is for trying updates out in a VM).
if [ -z "${ALLOW_DEV:-}" ] && unsquashfs -l "$slot/filesystem.squashfs" 2>/dev/null | grep -q 'getty@.*\.service\.d/autologin\.conf'; then
    echo "$stick is a development build (root autologin); build with 'make zip'" >&2
    exit 1
fi

release=out/release
rm -rf "$release"
mkdir -p "$release"

(cd "$stick" && zip -q -r -0 "../release/titanos-$version.zip" .)
(cd "$release" && sha256sum "titanos-$version.zip" > "titanos-$version.zip.sha256")
for file in vmlinuz initrd.img filesystem.squashfs; do
    cp "$slot/$file" "$release/titanos-$version-$file"
done

VERSION=$version CHANNEL=$channel CHANGELOG=$changelog RELEASE=$release IMAGE=$image \
    MIN_VERSION=${MIN_VERSION:-} python3 - <<'PY'
import datetime, hashlib, json, os, re

version, release, image = os.environ["VERSION"], os.environ["RELEASE"], os.environ["IMAGE"]

def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()

# The kernel command line of the boot entry, with @SLOT@ left in: the NAS
# fills in the slot it installs to.
options = [line.split(None, 1)[1].strip() for line in open("boot/entry.conf.in") if line.startswith("options")]
assert len(options) == 1 and "live-media-path=/titanos/@SLOT@" in options[0], options

files = []
for name in ("vmlinuz", "initrd.img", "filesystem.squashfs"):
    asset = f"titanos-{version}-{name}"
    path = os.path.join(release, asset)
    files.append({"name": name, "asset": asset, "sha256": sha256(path), "size": os.path.getsize(path)})

changelog = open(os.environ["CHANGELOG"], encoding="utf-8").read().strip() if os.environ["CHANGELOG"] else ""
manifest = {
    "version": version,
    "tag": "v" + version,
    "channel": os.environ["CHANNEL"],
    "kernel": open(os.path.join(image, "kernel-version")).read().strip(),
    "released": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "changelog": changelog,
    "cmdline": options[0],
    "files": files,
}
if os.environ["MIN_VERSION"]:
    manifest["minVersion"] = os.environ["MIN_VERSION"]
with open(os.path.join(release, "manifest.json"), "w", encoding="utf-8") as f:
    json.dump(manifest, f, indent=2, ensure_ascii=False)
    f.write("\n")

# The image is a collection of Debian packages, many of them under the GPL.
# Their exact sources stay available at snapshot.debian.org.
sources = set()
status = os.path.join(image, "rootfs/var/lib/dpkg/status")
for block in open(status, encoding="utf-8", errors="replace").read().split("\n\n"):
    fields = dict(re.findall(r"^(Package|Version|Source|Status): (.*)$", block, re.M))
    if "Package" not in fields or "installed" not in fields.get("Status", ""):
        continue
    source, source_version = fields.get("Source", fields["Package"]), fields["Version"]
    # "Source: name (version)" when the source has another version than the package.
    match = re.match(r"^(\S+) \((.+)\)$", source)
    if match:
        source, source_version = match.groups()
    # A "+b1" marks a rebuild of the same source.
    sources.add((source, re.sub(r"\+b\d+$", "", source_version)))
with open(os.path.join(release, f"titanos-{version}-sources.txt"), "w", encoding="utf-8") as f:
    f.write(f"TitanOS {version} is built from these Debian source packages.\n")
    f.write("Each link leads to the exact source that was used.\n\n")
    for source, source_version in sorted(sources):
        f.write(f"{source} {source_version}\n  https://snapshot.debian.org/package/{source}/{source_version.replace(':', '%3A')}/\n")
print(f"{len(sources)} source packages listed")
PY

echo
echo "Release $version ($channel) in $release:"
ls -l "$release" | awk 'NR > 1 { printf "  %10d  %s\n", $5, $9 }'
echo
echo "Sign the manifest before publishing:  minisign -S -m $release/manifest.json"
