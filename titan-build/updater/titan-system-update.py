#!/usr/bin/env python3
"""Install signed Titan Rugix releases; never download or execute update scripts."""
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import urllib.error
import urllib.parse
import urllib.request

REPOSITORY = "ra5on/TitanOS"
COMPATIBILITY = "titan-umbrel-rugix-amd64-v1"
IDENTITY = Path("/usr/share/titan/release.json")
PUBLIC_KEY = Path("/usr/share/titan/release-public.pem")
STAGING = Path("/data/.titan-updates")
LOCK = Path("/run/titan-system-update.lock")
MAX_BUNDLE = 2 * 1024 ** 3 - 1
VERSION = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-titan\.([1-9][0-9]*))?")
NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,180}")


class UpdateError(Exception):
    pass


class OtherChannel(UpdateError):
    pass


def version_key(value):
    if not isinstance(value, str) or len(value) > 100 or not VERSION.fullmatch(value):
        raise UpdateError("Ungültige Titan-Systemversion.")
    major, minor, patch, legacy_build = VERSION.fullmatch(value).groups()
    # Canonical stable succeeds every legacy build of the same base version.
    return (int(major), int(minor), int(patch), 1 if legacy_build is None else 0, int(legacy_build or 0))


def decode_json(contents):
    def unique_pairs(pairs):
        values = {}
        for key, value in pairs:
            if key in values:
                raise UpdateError("Mehrdeutige Update-Metadaten.")
            values[key] = value
        return values
    try:
        return json.loads(contents, object_pairs_hook=unique_pairs)
    except (ValueError, UnicodeDecodeError) as error:
        raise UpdateError("Ungültige Update-Metadaten.") from error


def protected_read(path, maximum):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size > maximum:
            raise UpdateError("Die installierte Update-Vertrauensbasis ist nicht geschützt.")
        return stream.read(maximum + 1)


def installed(current):
    value = decode_json(protected_read(IDENTITY, 65536))
    if (not isinstance(value, dict) or value.get("version") != current or value.get("osVersion") != current
            or value.get("systemCompatibility") != COMPATIBILITY or value.get("architecture") != "amd64"
            or value.get("stage") not in ("alpha", "beta", "stable") or platform.machine() != "x86_64"):
        raise UpdateError("Dieses System gehört nicht zur unterstützten Titan-Rugix-Basis. Alte Titan-Images können damit nicht aktualisiert werden.")
    version_key(current)
    protected_read(PUBLIC_KEY, 4096)
    return value


class GitHubRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        value = urllib.parse.urlsplit(newurl)
        if (value.scheme != "https" or value.username or value.password or value.port not in (None, 443)
                or value.hostname not in ("github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com")):
            raise UpdateError("Unzulässige Weiterleitung des Update-Downloads.")
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def response(url):
    value = urllib.parse.urlsplit(url)
    if (value.scheme != "https" or value.username or value.password or value.port not in (None, 443)
            or value.hostname not in ("api.github.com", "github.com")):
        raise UpdateError("Unzulässige Update-Adresse.")
    request = urllib.request.Request(url, headers={"User-Agent": "TitanOS-updater", "Accept": "application/vnd.github+json"})
    try:
        return urllib.request.build_opener(GitHubRedirect()).open(request, timeout=60)
    except (urllib.error.URLError, OSError) as error:
        raise UpdateError("Der Titan-Updatekanal auf GitHub ist gerade nicht erreichbar.") from error


def fetch_bytes(url, maximum):
    with response(url) as stream:
        length = stream.headers.get("Content-Length")
        if length is not None and (not length.isdigit() or int(length) > maximum):
            raise UpdateError("Update-Metadaten sind zu groß.")
        value = stream.read(maximum + 1)
        if len(value) > maximum:
            raise UpdateError("Update-Metadaten sind zu groß.")
        return value


def asset_url(version, name):
    version_key(version)
    if not isinstance(name, str) or not NAME.fullmatch(name):
        raise UpdateError("Ungültiger Update-Dateiname.")
    return f"https://github.com/{REPOSITORY}/releases/download/v{version}/{name}"


def update_asset_name(version):
    version_key(version)
    # Only old build identities retain their shipped -amd64 filename contract.
    suffix = "-amd64" if VERSION.fullmatch(version).group(4) is not None else ""
    return f"titan-{version}{suffix}.update"


def verify_signature(contents, signature, key=PUBLIC_KEY):
    if len(signature) != 64:
        raise UpdateError("Ungültige Titan-Update-Signatur.")
    with tempfile.TemporaryDirectory(prefix="titan-signature-") as temporary:
        directory = Path(temporary)
        checksums, detached = directory / "SHA256SUMS", directory / "SHA256SUMS.sig"
        checksums.write_bytes(contents); detached.write_bytes(signature)
        result = subprocess.run(["/usr/bin/openssl", "pkeyutl", "-verify", "-rawin", "-pubin", "-inkey", str(key),
                                 "-in", str(checksums), "-sigfile", str(detached)], capture_output=True, timeout=15)
        if result.returncode:
            raise UpdateError("Die Update-Signatur stimmt nicht mit dem eingebetteten Titan-Schlüssel überein.")


def checksum_entries(contents):
    try:
        text = contents.decode("ascii")
    except UnicodeDecodeError as error:
        raise UpdateError("Ungültige signierte Prüfsummenliste.") from error
    if not text.endswith("\n") or not 0 < len(text) <= 65536:
        raise UpdateError("Ungültige signierte Prüfsummenliste.")
    values = {}
    for line in text.splitlines():
        match = re.fullmatch(r"([a-f0-9]{64})  ([A-Za-z0-9][A-Za-z0-9._-]{0,180})", line)
        if not match or match[2] in values:
            raise UpdateError("Unsichere oder doppelte Datei in der signierten Prüfsummenliste.")
        values[match[2]] = match[1]
    return values


def verified_json(version, name, sums):
    if name not in sums:
        raise UpdateError("Die signierte Prüfsummenliste enthält nicht alle Release-Metadaten.")
    contents = fetch_bytes(asset_url(version, name), 262144)
    if hashlib.sha256(contents).hexdigest() != sums[name]:
        raise UpdateError("Die Release-Metadaten wurden verändert.")
    return decode_json(contents)


def verify_release(version, channel, github_release):
    if channel != "stable":
        raise UpdateError("TitanOS verwendet ausschließlich den Stable-Updatekanal.")
    sums_bytes = fetch_bytes(asset_url(version, "SHA256SUMS"), 65536)
    signature = fetch_bytes(asset_url(version, "SHA256SUMS.sig"), 64)
    verify_signature(sums_bytes, signature, PUBLIC_KEY)
    sums = checksum_entries(sums_bytes)
    manifest = verified_json(version, "build-manifest.json", sums)
    release = verified_json(version, "release.json", sums)
    if not isinstance(manifest, dict) or not isinstance(release, dict):
        raise UpdateError("Ungültige Titan-Releasebeschreibung.")
    if (manifest.get("stage") in ("alpha", "beta", "stable")
            and release.get("stage") == manifest["stage"] and manifest["stage"] != channel):
        raise OtherChannel("Dieses Update gehört zu einem anderen Kanal.")
    required = {"schemaVersion": 1, "releaseVersion": version, "osVersion": version, "architecture": "amd64",
                "firmware": "UEFI", "updateFormat": "rugix", "systemCompatibility": COMPATIBILITY,
                "ownTitanUpdateChannel": True, "releaseEligible": True, "stage": channel,
                "compatibleWithPreviousTitanRaucImages": False}
    if any(type(manifest.get(key)) is not type(value) or manifest.get(key) != value for key, value in required.items()):
        raise UpdateError("Dieses Update ist nicht für die installierte Titan-Rugix-Basis und den gewählten Kanal freigegeben.")
    for key, value in {"version": version, "osVersion": version, "architecture": "amd64", "stage": channel,
                       "systemCompatibility": COMPATIBILITY}.items():
        if release.get(key) != value:
            raise UpdateError("Die signierten Versions- und Kompatibilitätsdaten widersprechen sich.")
    verification = manifest.get("imageVerification", {})
    if (not isinstance(verification, dict) or verification.get("structuralCheck") != "passed"
            or not isinstance(verification.get("uefiHttpSmoke"), dict)
            or verification["uefiHttpSmoke"].get("status") != "passed"):
        raise UpdateError("Für dieses Update fehlt ein erfolgreicher Image-Boot-Test.")
    name = update_asset_name(version)
    assets = manifest.get("assets")
    if not isinstance(assets, list) or not 1 <= len(assets) <= 16 or any(not isinstance(item, dict) for item in assets):
        raise UpdateError("Ungültige Update-Dateiliste.")
    found = [item for item in assets if item.get("name") == name]
    if (len(found) != 1 or type(found[0].get("sizeBytes")) is not int or not 0 < found[0]["sizeBytes"] <= MAX_BUNDLE
            or not re.fullmatch(r"[a-f0-9]{64}", str(found[0].get("sha256")))
            or sums.get(name) != found[0]["sha256"]):
        raise UpdateError("Das Update-Bundle ist nicht eindeutig in den signierten Metadaten enthalten.")
    public_assets = github_release.get("assets", [])
    matches = [item for item in public_assets if isinstance(item, dict) and item.get("name") == name]
    if (len(matches) != 1 or matches[0].get("browser_download_url") != asset_url(version, name)
            or matches[0].get("size") != found[0]["sizeBytes"]):
        raise UpdateError("GitHub-Datei und signiertes Update-Bundle stimmen nicht überein.")
    notes = release.get("releaseNotes", "Signiertes TitanOS-Systemupdate über GitHub.")
    if not isinstance(notes, str) or len(notes) > 32768:
        raise UpdateError("Ungültige Release-Informationen.")
    label = release.get("versionName", "TitanOS 2.0")
    if not isinstance(label, str) or len(label) > 80 or not label.startswith("TitanOS "):
        raise UpdateError("Ungültiger Titan-Systemname.")
    return {"version": version, "name": label, "releaseNotes": notes,
            "asset": {**found[0], "url": asset_url(version, name)}, "stage": channel}


def latest(current, channel):
    local = installed(current)
    if channel != "stable":
        raise UpdateError("TitanOS verwendet ausschließlich den Stable-Updatekanal.")
    releases = decode_json(fetch_bytes(f"https://api.github.com/repos/{REPOSITORY}/releases?per_page=100", 2 * 1024 ** 2))
    if not isinstance(releases, list) or len(releases) > 100:
        raise UpdateError("Ungültige Antwort des Titan-Updatekanals.")
    candidates = []
    for item in releases:
        if (not isinstance(item, dict) or item.get("draft") is not False or type(item.get("prerelease")) is not bool
                or item["prerelease"] is not False or not isinstance(item.get("tag_name"), str)
                or not item["tag_name"].startswith("v")):
            continue
        version = item["tag_name"][1:]
        try:
            if version_key(version) > version_key(current): candidates.append((version, item))
        except UpdateError:
            continue  # Previous Titan/foreign release lines are never installed.
    rejected, invalid = False, 0
    # Authenticated releases from another channel do not consume the rejection
    # budget; many alpha builds must not hide the newest compatible beta build.
    for version, release in sorted(candidates, key=lambda item: version_key(item[0]), reverse=True):
        try:
            return verify_release(version, channel, release)
        except OtherChannel:
            continue
        except UpdateError:
            rejected = True
            invalid += 1
            if invalid >= 10: break
    if rejected:
        raise UpdateError("Es gibt noch kein vertrauenswürdig signiertes, kompatibles Update im ausgewählten Kanal.")
    return {"version": current, "name": local.get("versionName", "TitanOS 2.0"), "releaseNotes": ""}


def status(**values):
    print("umbrel-update: " + json.dumps(values, ensure_ascii=False), flush=True)


def staging_directory():
    mount = decode_json(subprocess.check_output(["/usr/bin/findmnt", "--json", "--target", "/data", "--output", "TARGET,FSTYPE,OPTIONS"], timeout=15))
    records = mount.get("filesystems", [])
    if (len(records) != 1 or records[0].get("target") != "/data" or records[0].get("fstype") not in ("ext4", "xfs", "zfs", "btrfs")
            or "ro" in records[0].get("options", "").split(",")):
        raise UpdateError("Der persistente Datenspeicher ist nicht verfügbar. Das Update wird nicht auf der Systempartition zwischengespeichert.")
    parent = STAGING.parent.lstat()
    if not stat.S_ISDIR(parent.st_mode) or parent.st_uid != 0 or parent.st_mode & 0o022:
        raise UpdateError("Der Update-Datenspeicher ist nicht geschützt.")
    STAGING.mkdir(mode=0o700, exist_ok=True)
    info = STAGING.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
        raise UpdateError("Der Update-Arbeitsbereich ist nicht geschützt.")
    return STAGING


def download_bundle(asset, target):
    if shutil.disk_usage(target.parent).free < asset["sizeBytes"] + 256 * 1024 ** 2:
        raise UpdateError("Für das Systemupdate ist nicht genügend freier Datenspeicher vorhanden.")
    digest, size = hashlib.sha256(), 0
    with response(asset["url"]) as stream, target.open("xb") as output:
        for block in iter(lambda: stream.read(1024 ** 2), b""):
            size += len(block)
            if size > asset["sizeBytes"]:
                raise UpdateError("Das Update-Bundle ist größer als signiert.")
            digest.update(block); output.write(block)
            status(description="Update herunterladen und prüfen…", progress=min(40, int(40 * size / asset["sizeBytes"])))
        output.flush(); os.fsync(output.fileno())
    if size != asset["sizeBytes"] or digest.hexdigest() != asset["sha256"]:
        raise UpdateError("Die Prüfsumme des Systemupdates stimmt nicht. Es wird nichts installiert.")


def install(current, channel, expected):
    if version_key(expected) <= version_key(current):
        raise UpdateError("Systemupdates dürfen keine ältere oder bereits installierte Version einspielen.")
    descriptor = latest(current, channel)
    if descriptor["version"] != expected or "asset" not in descriptor:
        raise UpdateError("Das ausgewählte Update ist nicht mehr verfügbar oder nicht neuer als das installierte System.")
    info = decode_json(subprocess.check_output(["/usr/bin/rugix-ctrl", "system", "info"], timeout=15))
    if info.get("boot", {}).get("bootFlow") != "grub":
        raise UpdateError("Dieses Update unterstützt nur native AMD64-Rugix-Images mit GRUB, keine Mender- oder Raspberry-Pi-Layouts.")
    staging = staging_directory()
    with tempfile.TemporaryDirectory(prefix="release-", dir=staging) as temporary:
        bundle = Path(temporary) / "verified.update"
        download_bundle(descriptor["asset"], bundle)
        status(description="Geprüftes Systemupdate installieren…", progress=40)
        # The outer Ed25519 signature authenticates every byte before the native
        # Rugix 1.x installer is allowed to write the inactive system slot.
        subprocess.run(["/usr/bin/rugix-ctrl", "update", "install", "--reboot", "set",
                        "--insecure-skip-bundle-verification", str(bundle)], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("check", "install"))
    parser.add_argument("--current-version", required=True)
    parser.add_argument("--channel", choices=("stable",), default="stable")
    parser.add_argument("--version")
    args = parser.parse_args()
    try:
        if args.action == "check":
            print(json.dumps(latest(args.current_version, args.channel), ensure_ascii=False))
        else:
            if os.geteuid() != 0 or not args.version:
                raise UpdateError("Systemupdates benötigen den geschützten Systemdienst.")
            fd = os.open(LOCK, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, "w") as lock:
                info = os.fstat(lock.fileno())
                if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
                    raise UpdateError("Die Update-Sperre ist nicht geschützt.")
                try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except BlockingIOError: raise UpdateError("Ein Systemupdate läuft bereits.") from None
                install(args.current_version, args.channel, args.version)
        return 0
    except (UpdateError, OSError, ValueError, subprocess.SubprocessError) as error:
        if args.action == "install": status(error=str(error))
        else: print(str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
