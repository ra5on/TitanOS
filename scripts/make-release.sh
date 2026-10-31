#!/usr/bin/env bash
# Turns the stick built by make-zip.sh into the files of a release:
#
#   out/release/titanos-<version>.zip (+ .sha256)   everything for a new stick
#   out/release/titanos-<version>-vmlinuz           \
#   out/release/titanos-<version>-initrd.img         } one system slot, for updates
#   out/release/titanos-<version>-filesystem.squashfs /
#   out/release/titanos-<version>-new-{vmlinuz,initrd.img,filesystem.squashfs}
#                                                   the slot with the newer kernel (kernel choice
#                                                   "Neu", docs/KERNEL.md), if out/kernel-new exists
#   out/release/titanos-<version>.img.xz            the stick as a disk image for legacy BIOS boards,
#                                                   if STICK_IMAGE=1
#   out/release/titanos-<version>-titand            titand alone, for an app update without restart
#   out/release/titanos-<version>-sources.txt       where the sources of the Debian packages are
#   out/release/manifest.json                       what an update consists of
#
# sign-release.sh signs the manifest, installation ZIP and complete checksums.
#
# Usage: scripts/make-release.sh <version> <stable|beta> [changelog file]
#   MIN_VERSION=x.y.z   oldest version that may update to this one directly
#   ALLOW_DEV=1         accept a development build; for tests only
#   WITHOUT_PLUGINS=1   pack only the system (image rehearsals on pull requests);
#                       verify-release.py rejects such a release unless told so
#   BASE_VERSION=x.y.z  the system image the release was made on; set for an app
#                       release (repack-release.py), else the version itself
#   TITAND=path         titand of the release (default backend/bin/titand)
#   STICK_IMAGE=1       also pack the stick as a disk image (legacy BIOS)
#
# Expects out/stick and build/mkosi.output from "make zip" with the same VERSION.
set -euo pipefail

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
case "$version" in *-*) expected=beta ;; *) expected=stable ;; esac
if [ "$channel" != "$expected" ]; then
    echo "version and release channel disagree" >&2; exit 2
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
listing=$(mktemp)
trap 'rm -f "$listing"' EXIT
# Do not turn a corrupt image (unsquashfs failure) into a successful dev check.
unsquashfs -l "$slot/filesystem.squashfs" > "$listing"
if [ "${ALLOW_DEV:-}" != 1 ] && grep -q 'getty@.*\.service\.d/autologin\.conf' "$listing"; then
    echo "$stick is a development build (root autologin); build with 'make zip'" >&2
    exit 1
fi
if [ -n "$(find "$stick/config" -mindepth 1 -print -quit)" ]; then
    echo "refusing to publish a stick containing user configuration" >&2; exit 1
fi
identity=$(unsquashfs -cat "$slot/filesystem.squashfs" usr/lib/os-release)
if [ "${ALLOW_DEV:-}" != 1 ] && printf '%s\n' "$identity" | grep -q '^VARIANT_ID=dev$'; then
    echo "refusing to publish a development image" >&2; exit 1
fi
# An app update replaces titand on a system image of this version alone.
base=${BASE_VERSION:-$version}
titand=${TITAND:-backend/bin/titand}
if [ "$(unsquashfs -cat "$slot/filesystem.squashfs" usr/share/titanos/base-version 2>/dev/null)" != "$base" ]; then
    echo "the image does not name $base as its base version" >&2; exit 1
fi
if [ ! -f "$titand" ] || ! unsquashfs -cat "$slot/filesystem.squashfs" usr/bin/titand | cmp -s - "$titand"; then
    echo "$titand is not the titand inside the image" >&2; exit 1
fi
# The same system with the newer kernel (make-zip.sh --kernel-new, or the
# variant of the base for an app release). Same checks as the default image.
variant=out/kernel-new
variant_image=build/mkosi.output-new
if [ -d "$variant" ]; then
    vsquash=$variant/filesystem.squashfs
    if [ "$(cat "$variant/version" 2>/dev/null)" != "$version" ]; then
        echo "$variant does not hold version $version" >&2; exit 1
    fi
    if [ "$(unsquashfs -cat "$vsquash" usr/share/titanos/kernel-variant)" != new ]; then
        echo "$variant is not the image with the new kernel" >&2; exit 1
    fi
    if [ "${ALLOW_DEV:-}" != 1 ] && { unsquashfs -l "$vsquash" | grep -q 'getty@.*\.service\.d/autologin\.conf' \
        || unsquashfs -cat "$vsquash" usr/lib/os-release | grep -q '^VARIANT_ID=dev$'; }; then
        echo "refusing to publish a development image with the new kernel" >&2; exit 1
    fi
    if [ "$(unsquashfs -cat "$vsquash" usr/share/titanos/base-version 2>/dev/null)" != "$base" ]; then
        echo "the image with the new kernel does not name $base as its base version" >&2; exit 1
    fi
    if ! unsquashfs -cat "$vsquash" usr/bin/titand | cmp -s - "$titand"; then
        echo "$titand is not the titand inside the image with the new kernel" >&2; exit 1
    fi
fi

release=out/release
rm -rf "$release"
mkdir -p "$release"

(cd "$stick" && zip -q -r -0 "../release/titanos-$version.zip" .)
(cd "$release" && sha256sum "titanos-$version.zip" > "titanos-$version.zip.sha256")
for file in vmlinuz initrd.img filesystem.squashfs; do
    cp "$slot/$file" "$release/titanos-$version-$file"
    if [ -d "$variant" ]; then
        cp "$variant/$file" "$release/titanos-$version-new-$file"
    fi
done
if [ "${STICK_IMAGE:-}" = 1 ]; then
    python3 scripts/make-stick-image.py "$stick" "$release/titanos-$version.img"
    xz -T0 -6 "$release/titanos-$version.img"
fi
cp "$titand" "$release/titanos-$version-titand"
if [ -x backend/bin/titanosctl ]; then
    cp backend/bin/titanosctl "$release/titanos-$version-cli-linux-amd64"
fi

VARIANT=$([ -d "$variant" ] && echo "$variant") VARIANT_IMAGE=$variant_image \
VERSION=$version BASE=$base CHANNEL=$channel CHANGELOG=$changelog RELEASE=$release IMAGE=$image \
    MIN_VERSION=${MIN_VERSION:-} WITHOUT_PLUGINS=${WITHOUT_PLUGINS:-} python3 - <<'PY'
import datetime, hashlib, json, os, re, tarfile, shutil, zipfile
from pathlib import Path

version, base, release, image = os.environ["VERSION"], os.environ["BASE"], os.environ["RELEASE"], os.environ["IMAGE"]



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

def kernel_package(kernel, image=image):
    """The Debian version of the image's kernel package: plugin sets are ordered by it."""
    for block in (Path(image) / "rootfs/var/lib/dpkg/status").read_text().split("\n\n"):
        fields = dict(re.findall(r"^(Package|Version|Status): (.*)$", block, re.M))
        if fields.get("Package") == "linux-image-" + kernel and fields.get("Status") == "install ok installed":
            return fields["Version"]
    raise SystemExit("linux-image-" + kernel + " is not installed in the image")

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
    # Optional; older TitanOS ignore it. Finds the plugin set of the target kernel.
    "kernelPackage": kernel_package(open(os.path.join(image, "kernel-version")).read().strip()),
    "released": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "changelog": changelog,
    "cmdline": options[0],
    "files": files,
    # titand alone, for systems running the image this release was made on.
    # Older TitanOS ignore the field and install the files above.
    "app": {"base": base, "asset": f"titanos-{version}-titand",
            "sha256": sha256(os.path.join(release, f"titanos-{version}-titand")),
            "size": os.path.getsize(os.path.join(release, f"titanos-{version}-titand"))},
}
if os.environ["MIN_VERSION"]:
    manifest["minVersion"] = os.environ["MIN_VERSION"]
# The same system with the newer kernel. Optional; older TitanOS ignore it.
# Its plugins come from the plugin repository (kernel set of kernelPackage).
variant = os.environ["VARIANT"]
if variant:
    kernel_new = open(os.path.join(variant, "kernel-version")).read().strip()
    if "bpo" not in kernel_new or kernel_new == manifest["kernel"]:
        raise SystemExit("the new kernel is not a backports kernel: " + kernel_new)
    new_files = []
    for name in ("vmlinuz", "initrd.img", "filesystem.squashfs"):
        asset = f"titanos-{version}-new-{name}"
        path = os.path.join(release, asset)
        new_files.append({"name": name, "asset": asset, "sha256": sha256(path), "size": os.path.getsize(path)})
    manifest["variants"] = {"new": {"kernel": kernel_new,
                                    "kernelPackage": kernel_package(kernel_new, os.environ["VARIANT_IMAGE"]),
                                    "files": new_files}}
# Plugins come from the sets of ra5on/TitanOS-Plugins (scripts/plugin-set.py
# puts them into out/plugins); they must match this exact system. The official
# ones are those the image has catalog entries for. Driver plugins carry their
# compiled sources.
OFFICIAL = {p.stem for p in Path("plugins/catalog").glob("*.json")}
manifest["plugins"] = []
plugin_sources = []
without_plugins = os.environ["WITHOUT_PLUGINS"] == "1"
for path in [] if without_plugins else sorted(Path("out/plugins").glob("*.plugin.json")):
    plugin = json.loads(path.read_text())
    name = plugin["name"]
    if name not in OFFICIAL or path.name != name + ".plugin.json":
        raise ValueError("unrecognized official plugin")
    if (name == "nvidia" or plugin.get("kernel")) and plugin.get("kernel") != manifest["kernel"]:
        raise ValueError("plugin kernel mismatch: " + name)
    # Plugins belong to the system image: an app release brings those of its base.
    # A plugin set's version with the base it is carried for; plugins of a base
    # before the plugin repository carry the base version itself.
    expected = plugin["version"].endswith("-titanos-" + base) or (name != "nvidia" and plugin["version"] == base)
    if not expected or sha256(Path("out/plugins") / (name + ".raw")) != plugin["sha256"] or (Path("out/plugins") / (name + ".raw")).stat().st_size != plugin["size"]:
        raise ValueError("plugin version/checksum mismatch: " + name)
    assets = [name + ".raw", path.name, name + "-licenses.tar.gz"]
    if name == "nvidia":
        assets.append("nvidia-kernel-sources.tar.gz")
    if name != "nvidia" and plugin.get("kernel"):
        if plugin.get("kernel") != manifest["kernel"] or not plugin.get("sources"):
            raise ValueError("driver plugin without kernel or sources: " + name)
        assets.append(name + "-sources.tar.gz")
    if plugin.get("sources"):
        plugin_sources.append((name, plugin["version"], plugin["sources"]))
    for asset in assets:
        shutil.copy2(Path("out/plugins") / asset, Path(release) / asset)
    manifest["plugins"].append({"name": name, "asset": path.name})
if not without_plugins:
    if not any(p["name"] == "nvidia" for p in manifest["plugins"]):
        raise ValueError("NVIDIA plugin missing")
    sdk = json.loads(Path("out/plugins/nvidia-build-sdk.json").read_text())
    if sdk["version"] != base or sdk["kernel"] != manifest["kernel"] or sdk["asset"] != "nvidia-build-sdk.tar.zst":
        raise ValueError("NVIDIA SDK target mismatch")
    if sha256(Path("out/plugins") / sdk["asset"]) != sdk["sha256"] or (Path("out/plugins") / sdk["asset"]).stat().st_size != sdk["size"]:
        raise ValueError("NVIDIA SDK integrity mismatch")
    manifest["nvidiaBuildSDK"] = sdk
    for asset in [sdk["asset"], "nvidia-build-sdk-sources.txt"]:
        shutil.copy2(Path("out/plugins") / asset, Path(release) / asset)
with open(os.path.join(release, "manifest.json"), "w", encoding="utf-8") as f:
    json.dump(manifest, f, indent=2, ensure_ascii=False)
    f.write("\n")

# Use the inventory generated for this actual image, including Go, npm and fonts.
notices = Path(image) / "rootfs/usr/share/titanos"
components = json.loads((notices / "components.json").read_text())
variant_notices = Path(os.environ["VARIANT_IMAGE"]) / "rootfs/usr/share/titanos"
variant_components = json.loads((variant_notices / "components.json").read_text()) if variant else []
if not any(c["ecosystem"] == "Debian" for c in components):
    raise SystemExit("missing Debian license inventory")
with open(os.path.join(release, f"titanos-{version}-sources.txt"), "w", encoding="utf-8") as f:
    f.write(f"TitanOS {version}: exact third-party package sources.\n")
    f.write("Includes browser/build dependencies; firmware may contain binary-only blobs.\n\n")
    for c in components:
        f.write(f"{c['ecosystem']}: {c['name']} {c['version']}\n  {c['source']}\n")
    for name, plugin_version, links in plugin_sources:
        f.write(f"Plugin: {name} {plugin_version}\n" + "".join(f"  {link}\n" for link in links))
    if variant:
        # Kernel and firmware of the image with the new kernel differ.
        known = {(c["ecosystem"], c["name"], c["version"]) for c in components}
        extra = [c for c in variant_components if (c["ecosystem"], c["name"], c["version"]) not in known]
        f.write("\nImage with the new kernel (titanos-" + version + "-new-*), additionally:\n")
        for c in extra:
            f.write(f"{c['ecosystem']}: {c['name']} {c['version']}\n  {c['source']}\n")
with tarfile.open(os.path.join(release, f"titanos-{version}-licenses.tar.gz"), "w:gz") as archive:
    for name in ["LICENSE.md", "THIRD_PARTY_LICENSES", "components.json", "licenses"]:
        archive.add(notices / name, arcname=name)
    if variant:
        for name in ["components.json", "licenses"]:
            archive.add(variant_notices / name, arcname="kernel-new/" + name)
with open(os.path.join(release, "SHA256SUMS"), "w", encoding="utf-8") as f:
    for path in sorted(Path(release).iterdir()):
        if path.name != "SHA256SUMS":
            f.write(f"{sha256(path)}  {path.name}\n")
print(f"{len(components)} components listed, original license texts included")
PY

echo
echo "Release $version ($channel) in $release:"
# The names are our own release files; ls only shows their sizes.
# shellcheck disable=SC2012
ls -l "$release" | awk 'NR > 1 { printf "  %10d  %s\n", $5, $9 }'
echo
echo "Sign before publishing: scripts/sign-release.sh $release <secret-key>"
echo "Verify: python3 scripts/verify-release.py $release build/mkosi.extra/usr/share/titanos/update-keys"
