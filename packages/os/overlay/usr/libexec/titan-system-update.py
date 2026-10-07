#!/usr/bin/env python3
"""Install signed Titan Rugix releases; never download or execute update scripts."""
import argparse
import base64
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
COMPATIBILITY = "titan-rugix-amd64-v2"
IDENTITY = Path("/usr/share/titan/release.json")
PUBLIC_KEY = Path("/usr/share/titan/release-public.pem")
STAGING = Path("/data/.titan-updates")
LOCK = Path("/run/titan-system-update.lock")
MAX_BUNDLE = 2 * 1024 ** 3 - 1
VERSION = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,180}")


class UpdateError(Exception):
    pass


def version_key(value):
    if not isinstance(value, str) or len(value) > 100 or not VERSION.fullmatch(value):
        raise UpdateError("Ungültige Titan-Systemversion.")
    return tuple(int(number) for number in VERSION.fullmatch(value).groups())


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
            or value.get("stage") != "stable" or platform.machine() != "x86_64"):
        raise UpdateError("Dieses System gehört nicht zur unterstützten TitanOS-Installationsbasis.")
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
    return f"titan-{version}.update"


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


def verified_json(version, name, sums, fetch=fetch_bytes):
    if name not in sums:
        raise UpdateError("Die signierte Prüfsummenliste enthält nicht alle Release-Metadaten.")
    contents = fetch(asset_url(version, name), 262144)
    if hashlib.sha256(contents).hexdigest() != sums[name]:
        raise UpdateError("Die Release-Metadaten wurden verändert.")
    return decode_json(contents)


def verify_release(version, channel, github_release, evidence=None):
    if channel != "stable":
        raise UpdateError("TitanOS verwendet ausschließlich den Stable-Updatekanal.")
    files = {} if evidence is None else evidence
    def fetch(url, maximum):
        name = urllib.parse.urlsplit(url).path.rsplit("/", 1)[-1]
        if evidence is None:
            contents = fetch_bytes(url, maximum)
            files[name] = base64.b64encode(contents).decode("ascii")
            return contents
        try:
            contents = base64.b64decode(files[name], validate=True)
        except (KeyError, ValueError, TypeError) as error:
            raise UpdateError("Die gespeicherten Release-Nachweise sind unvollständig.") from error
        if len(contents) > maximum:
            raise UpdateError("Die gespeicherten Release-Nachweise sind zu groß.")
        return contents
    sums_bytes = fetch(asset_url(version, "SHA256SUMS"), 65536)
    signature = fetch(asset_url(version, "SHA256SUMS.sig"), 64)
    verify_signature(sums_bytes, signature, PUBLIC_KEY)
    sums = checksum_entries(sums_bytes)
    manifest = verified_json(version, "build-manifest.json", sums, fetch)
    release = verified_json(version, "release.json", sums, fetch)
    if not isinstance(manifest, dict) or not isinstance(release, dict):
        raise UpdateError("Ungültige Titan-Releasebeschreibung.")
    required = {"schemaVersion": 1, "releaseVersion": version, "osVersion": version, "architecture": "amd64",
                "firmware": "UEFI", "updateFormat": "rugix", "systemCompatibility": COMPATIBILITY,
                "ownTitanUpdateChannel": True, "releaseEligible": True, "stage": "stable"}
    if any(type(manifest.get(key)) is not type(value) or manifest.get(key) != value for key, value in required.items()):
        raise UpdateError("Dieses Update ist nicht für die installierte TitanOS-Basis freigegeben.")
    for key, value in {"version": version, "osVersion": version, "architecture": "amd64", "stage": channel,
                       "systemCompatibility": COMPATIBILITY}.items():
        if release.get(key) != value:
            raise UpdateError("Die signierten Versions- und Kompatibilitätsdaten widersprechen sich.")
    verification = manifest.get("imageVerification", {})
    if (not isinstance(verification, dict) or verification.get("structuralCheck") != "passed"
            or not isinstance(verification.get("uefiHttpSmoke"), dict)
            or verification["uefiHttpSmoke"].get("status") != "passed"):
        raise UpdateError("Für dieses Update fehlt ein erfolgreicher Image-Boot-Test.")
    boot = verification["uefiHttpSmoke"]
    if (boot.get("installedRelease") != {"version": version, "name": f"TitanOS {version}"}
            or type(boot.get("bootDiskSizeBytes")) is not int or boot["bootDiskSizeBytes"] < 32 * 1024**3):
        raise UpdateError("Der Boot-Test gehört nicht zur angebotenen TitanOS-Version.")
    name = update_asset_name(version)
    assets = manifest.get("assets")
    if (not isinstance(assets, list) or len(assets) != 2
            or any(not isinstance(item, dict) or not isinstance(item.get("name"), str) for item in assets)
            or {item.get("name") for item in assets} != {name, f"titan-{version}.img.xz"}):
        raise UpdateError("Ungültige Update-Dateiliste.")
    for asset in assets:
        if (type(asset.get("sizeBytes")) is not int or not 0 < asset["sizeBytes"] <= MAX_BUNDLE
                or not re.fullmatch(r"[a-f0-9]{64}", str(asset.get("sha256")))
                or sums.get(asset["name"]) != asset["sha256"]):
            raise UpdateError("Die signierte TitanOS-Dateiliste widerspricht den Prüfsummen.")
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
    label = release.get("versionName")
    if label != f"TitanOS {version}":
        raise UpdateError("Ungültiger Titan-Systemname.")
    return {"version": version, "name": label, "releaseNotes": notes,
            "asset": {**found[0], "url": asset_url(version, name)}, "stage": channel,
            "evidence": {"files": files, "github": {"assets": matches}}}


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
            continue
    rejected, invalid = False, 0
    for version, release in sorted(candidates, key=lambda item: version_key(item[0]), reverse=True):
        try:
            return verify_release(version, channel, release)
        except UpdateError:
            rejected = True
            invalid += 1
            if invalid >= 10: break
    if rejected:
        raise UpdateError("Es gibt noch kein vertrauenswürdig signiertes, kompatibles TitanOS-Update.")
    return {"version": current, "name": local.get("versionName", f"TitanOS {current}"), "releaseNotes": ""}


def status(**values):
    print("titan-update: " + json.dumps(values, ensure_ascii=False), flush=True)


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


def system_info():
    info = decode_json(subprocess.check_output(["/usr/bin/rugix-ctrl", "system", "info", "--json"], timeout=15))
    boot = info.get("boot", {})
    if (boot.get("bootFlow") != "grub" or boot.get("activeGroup") not in ("a", "b")
            or boot.get("defaultGroup") not in ("a", "b") or set(boot.get("groups", {})) != {"a", "b"}
            or info.get("state", {}).get("status") != "Active"):
        raise UpdateError("Der native TitanOS-Systemzustand ist nicht für Updates oder Wiederherstellung bereit.")
    for group in ("a", "b"):
        for kind in ("boot", "system"):
            if not isinstance(info.get("slots", {}).get(f"{kind}-{group}"), dict):
                raise UpdateError("Der native TitanOS-Systemslot fehlt.")
    return info


def slot_snapshot(info, group):
    # These fingerprints are Rugix's persistent payload database, not user input.
    # Invalidate the journal before any install can overwrite the spare slot.
    return {name: {key: info["slots"][name].get(key) for key in ("hashes", "size", "updatedAt")}
            for name in (f"boot-{group}", f"system-{group}")}


def empty_journal():
    return {"schemaVersion": 1, "systemCompatibility": COMPATIBILITY, "slots": {}}


def read_journal(directory):
    try:
        value = decode_json(protected_read(directory / "slots.json", 65536))
    except FileNotFoundError:
        return empty_journal()
    if (not isinstance(value, dict) or type(value.get("schemaVersion")) is not int or value.get("schemaVersion") != 1
            or value.get("systemCompatibility") != COMPATIBILITY or not isinstance(value.get("slots"), dict)
            or set(value["slots"]) - {"a", "b"}):
        raise UpdateError("Die geschützten TitanOS-Slotnachweise sind ungültig.")
    for record in value["slots"].values():
        if (not isinstance(record, dict) or type(record.get("confirmed")) is not bool
                or not isinstance(record.get("snapshot"), dict) or not isinstance(record.get("name"), str)):
            raise UpdateError("Die geschützten TitanOS-Slotnachweise sind unvollständig.")
        version_key(record.get("version"))
    if "pending" in value:
        pending = value["pending"]
        if (not isinstance(pending, dict) or pending.get("type") not in ("update", "rollback")
                or pending.get("slot") not in ("a", "b")):
            raise UpdateError("Die vorgemerkte Systemumschaltung ist ungültig.")
        version_key(pending.get("version"))
    return value


def atomic_json(path, value):
    fd, temporary = tempfile.mkstemp(prefix=".state-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(value, stream, ensure_ascii=False)
            stream.write("\n"); stream.flush(); os.fsync(stream.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try: os.fsync(directory)
        finally: os.close(directory)
    finally:
        with contextlib.suppress(FileNotFoundError): os.unlink(temporary)


def store_evidence(directory, descriptor):
    version_key(descriptor["version"])
    path = directory / f'evidence-{descriptor["version"]}.json'
    atomic_json(path, descriptor["evidence"])


def verified_evidence(directory, version):
    version_key(version)
    value = decode_json(protected_read(directory / f"evidence-{version}.json", 1024 ** 2))
    if not isinstance(value, dict) or not isinstance(value.get("files"), dict) or not isinstance(value.get("github"), dict):
        raise UpdateError("Der signierte Nachweis dieses Systemstands fehlt.")
    return verify_release(version, "stable", value["github"], value["files"])


def valid_record(directory, info, group, record):
    if (not isinstance(record, dict) or record.get("confirmed") is not True
            or record.get("snapshot") != slot_snapshot(info, group)):
        raise UpdateError("Dieser Systemslot wurde nicht bestätigt oder inzwischen verändert.")
    descriptor = verified_evidence(directory, record.get("version"))
    if record.get("name") != descriptor["name"]:
        raise UpdateError("Die gespeicherte Systemversion stimmt nicht mit dem signierten Nachweis überein.")
    return descriptor


def selection_token(group, record):
    return hashlib.sha256(json.dumps({"group": group, "record": record}, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def recovery_status(current, directory=None, info=None, journal=None):
    installed(current)
    directory = staging_directory() if directory is None else directory
    info = system_info() if info is None else info
    journal = read_journal(directory) if journal is None else journal
    active = info["boot"]["activeGroup"]
    result = {"current": {"version": current, "name": f"TitanOS {current}", "slot": active,
                           "confirmed": active == info["boot"]["defaultGroup"]},
              "previous": [], "reason": "Noch kein vorheriger bestätigter Systemstand vorhanden."}
    if active != info["boot"]["defaultGroup"] or journal.get("pending"):
        result["reason"] = "Der aktuelle Systemstart wurde noch nicht bestätigt."
        return result
    # Never infer a previous version from the application store's previousVersion
    # or a factory-created clone: only a signed, recorded install is selectable.
    try:
        now = valid_record(directory, info, active, journal["slots"].get(active))
        if now["version"] != current:
            raise UpdateError("Der aktive Versionsnachweis passt nicht zum gestarteten System.")
        group = "b" if active == "a" else "a"
        record = journal["slots"].get(group)
        previous = valid_record(directory, info, group, record)
        if previous["version"] == current:
            return result
        result["previous"] = [{"version": previous["version"], "name": previous["name"], "slot": group,
                                "selection": selection_token(group, record)}]
        result["reason"] = ""
    except (UpdateError, OSError, ValueError):
        pass
    return result


def sync_menu(current, directory=None, info=None, journal=None):
    state = recovery_status(current, directory, info, journal)
    versions = {state["current"]["slot"]: current}
    for item in state["previous"]:
        versions[item["slot"]] = item["version"]
    # This helper changes only Titan's additional menu metadata; Rugix owns its
    # checksummed default/try-boot environments and native rollback operation.
    previous = state["previous"][0] if state["previous"] else None
    metadata = {"versions": versions, "previous": {"slot": previous["slot"], "version": previous["version"]} if previous else None}
    subprocess.run(["/usr/bin/python3", "/usr/libexec/titan-recovery-menu.py", "--metadata", json.dumps(metadata)],
                   check=True, timeout=30)


def remember_current(directory, info, journal, current):
    active = info["boot"]["activeGroup"]
    record = journal["slots"].get(active)
    if record is not None:
        descriptor = valid_record(directory, info, active, record)
        if descriptor["version"] != current:
            raise UpdateError("Der aktive Systemnachweis gehört nicht zur installierten Version.")
        return
    # Seed only the actually running, committed system, after validating its
    # published signature. A dormant old slot is never guessed or imported.
    descriptor = signed_published_release(current)
    store_evidence(directory, descriptor)
    journal["slots"][active] = {"version": current, "name": descriptor["name"], "confirmed": True,
                                "snapshot": slot_snapshot(info, active)}


def install(current, channel, expected):
    if version_key(expected) <= version_key(current):
        raise UpdateError("Systemupdates dürfen keine ältere oder bereits installierte Version einspielen.")
    descriptor = latest(current, channel)
    if descriptor["version"] != expected or "asset" not in descriptor:
        raise UpdateError("Das ausgewählte Update ist nicht mehr verfügbar oder nicht neuer als das installierte System.")
    info = system_info()
    if info["boot"]["activeGroup"] != info["boot"]["defaultGroup"]:
        raise UpdateError("Der aktuelle Systemstart muss vor einem Update bestätigt sein.")
    staging = staging_directory()
    journal = read_journal(staging)
    if journal.get("pending"):
        raise UpdateError("Eine Systemumschaltung wartet bereits auf den Neustart.")
    with tempfile.TemporaryDirectory(prefix="release-", dir=staging) as temporary:
        bundle = Path(temporary) / "verified.update"
        download_bundle(descriptor["asset"], bundle)
        remember_current(staging, info, journal, current)
        store_evidence(staging, descriptor)
        target = "b" if info["boot"]["activeGroup"] == "a" else "a"
        journal["slots"].pop(target, None)
        atomic_json(staging / "slots.json", journal)
        sync_menu(current, staging, info, journal)
        status(description="Geprüftes Systemupdate installieren…", progress=40)
        subprocess.run(["/usr/bin/rugix-ctrl", "update", "install", "--reboot", "set",
                        "--insecure-skip-bundle-verification", str(bundle)], check=True)
        updated = system_info()
        journal["slots"][target] = {"version": expected, "name": descriptor["name"], "confirmed": False,
                                    "snapshot": slot_snapshot(updated, target)}
        journal["pending"] = {"type": "update", "slot": target, "version": expected}
        atomic_json(staging / "slots.json", journal)


def rollback(current, selection):
    directory = staging_directory()
    info = system_info()
    journal = read_journal(directory)
    available = recovery_status(current, directory, info, journal)["previous"]
    if len(available) != 1 or available[0]["selection"] != selection:
        raise UpdateError("Der ausgewählte Systemstand ist nicht mehr für die Wiederherstellung verfügbar.")
    previous = available[0]
    journal["pending"] = {"type": "rollback", "slot": previous["slot"], "version": previous["version"]}
    atomic_json(directory / "slots.json", journal)
    status(description=f'{previous["name"]} starten…', progress=90)
    try:
        # Rugix 1.2.1-dev.1 supports this native try-boot operation; it keeps the
        # current default as the automatic fallback until healthy confirmation.
        subprocess.run(["/usr/bin/rugix-ctrl", "system", "reboot", "--spare"], check=True)
    except (OSError, subprocess.SubprocessError):
        journal.pop("pending", None)
        atomic_json(directory / "slots.json", journal)
        raise


def health_check(current, port):
    info = system_info()
    for name in ("system.online", "system.version"):
        request = urllib.request.Request(f"http://127.0.0.1:{port}/trpc/{name}", headers={"Accept": "application/json"})
        with urllib.request.urlopen(request, timeout=10) as response:
            value = decode_json(response.read(65537))
        result = value.get("result", {}).get("data")
        if (name == "system.online" and result is not True
                or name == "system.version" and (not isinstance(result, dict) or result.get("version") != current)):
            raise UpdateError("Die TitanOS-Verwaltung hat den neuen Systemstart noch nicht erfolgreich bestätigt.")
    return info


def signed_published_release(version):
    release = decode_json(fetch_bytes(f"https://api.github.com/repos/{REPOSITORY}/releases/tags/v{version}", 2 * 1024 ** 2))
    if (not isinstance(release, dict) or release.get("draft") is not False
            or release.get("prerelease") is not False or release.get("tag_name") != f"v{version}"):
        raise UpdateError("Der Systemstand ist nicht als signiertes Stable-Release veröffentlicht.")
    return verify_release(version, "stable", release)


def read_spare_identity(group):
    partition = 4 if group == "a" else 5
    resolved = decode_json(subprocess.check_output(["/usr/bin/rugix-ctrl", "utils", "resolve-partition", str(partition)], timeout=15))
    device = resolved.get("device")
    if (not isinstance(device, str) or not re.fullmatch(r"/dev/[A-Za-z0-9/_-]+", device)
            or not stat.S_ISBLK(os.stat(device).st_mode)):
        raise UpdateError("Der vorherige Systemslot konnte nicht sicher ermittelt werden.")
    target = Path(tempfile.mkdtemp(prefix="titan-previous-", dir="/run"))
    mounted = False
    try:
        subprocess.run(["/usr/bin/mount", "-t", "ext4", "-o", "ro,noload,nodev,nosuid,noexec", device, str(target)], check=True, timeout=30)
        mounted = True
        identity = decode_json(protected_read(target / "usr/share/titan/release.json", 65536))
        key = protected_read(target / "usr/share/titan/release-public.pem", 4096)
    finally:
        if mounted:
            try:
                subprocess.run(["/usr/bin/umount", str(target)], check=True, timeout=30)
            except (OSError, subprocess.SubprocessError):
                subprocess.run(["/usr/bin/umount", "--lazy", str(target)], check=True, timeout=30)
        # Never recursively remove a still-mounted system filesystem.
        target.rmdir()
    if key != protected_read(PUBLIC_KEY, 4096):
        raise UpdateError("Der vorherige Systemstand verwendet eine andere Vertrauensbasis.")
    return identity


def import_legacy_transition(current, directory, info, journal):
    # Existing 2.0.3 installations did not write Titan's journal. Only during an
    # actual native trial boot may we recover their old committed default. A
    # freshly cloned spare, arbitrary /data files or store.previousVersion do
    # not qualify. Both immutable identities must match signed public releases.
    active, previous = info["boot"]["activeGroup"], info["boot"]["defaultGroup"]
    if active == previous:
        return
    if journal["slots"]:
        pending = journal.get("pending", {})
        # A rollback to the pre-journal updater can subsequently install another
        # signed update. Its old code does not clear our pending marker; re-attest
        # only that real native transition from the unchanged recorded default.
        if pending.get("type") != "rollback" or pending.get("slot") != previous:
            return
        previous_release = valid_record(directory, info, previous, journal["slots"].get(previous))
        if pending.get("version") != previous_release["version"]:
            raise UpdateError("Die vorherige Umschaltung passt nicht zum nativen Standard-Systemslot.")
    identity = read_spare_identity(previous)
    old = identity.get("version")
    if (identity.get("systemCompatibility") != COMPATIBILITY or identity.get("architecture") != "amd64"
            or identity.get("stage") != "stable" or identity.get("osVersion") != old
            or version_key(old) >= version_key(current)):
        return
    for group, version in ((previous, old), (active, current)):
        descriptor = signed_published_release(version)
        if group == previous and identity.get("versionName") != descriptor["name"]:
            raise UpdateError("Der vorherige Systemname widerspricht dem signierten Release.")
        store_evidence(directory, descriptor)
        journal["slots"][group] = {"version": version, "name": descriptor["name"],
                                    "confirmed": group == previous, "snapshot": slot_snapshot(info, group)}
    journal["pending"] = {"type": "update", "slot": active, "version": current}
    atomic_json(directory / "slots.json", journal)


def confirm(current, port):
    installed(current)
    directory = staging_directory()
    info = health_check(current, port)
    journal = read_journal(directory)
    active = info["boot"]["activeGroup"]
    import_legacy_transition(current, directory, info, journal)
    record = journal["slots"].get(active)
    pending = journal.get("pending")
    if pending and pending.get("slot") == active:
        if pending.get("version") != current or not isinstance(record, dict) or record.get("version") != current:
            raise UpdateError("Der gestartete Systemstand gehört nicht zur vorgemerkten Umschaltung.")
        # The installed update is still unconfirmed. Check its exact recorded
        # payload fingerprint and signed release before promoting it.
        valid_record(directory, info, active, {**record, "confirmed": True})
    elif active != info["boot"]["defaultGroup"]:
        if record is None:
            # Upgrade from pre-journal releases may boot this image without a
            # journal. Healthy native commit is allowed, but exposes no rollback.
            if journal["slots"]:
                raise UpdateError("Für diesen ausgewählten Systemslot fehlt ein bestätigter Nachweis.")
        else:
            descriptor = valid_record(directory, info, active, record)
            if descriptor["version"] != current:
                raise UpdateError("Die Bootmenüauswahl gehört nicht zur gestarteten Version.")
    # Prove durable data writes before making this slot the boot default.
    atomic_json(directory / "slots.json", journal)
    subprocess.run(["/usr/bin/rugix-ctrl", "system", "commit"], check=True, timeout=30)
    if record is not None and record.get("version") == current:
        record["confirmed"] = True
    # A failed trial returned to the old default. Discard the failed/unconfirmed
    # target instead of accidentally offering it for another rollback.
    if pending and pending.get("slot") != active:
        failed = pending.get("slot")
        if failed in journal["slots"] and journal["slots"][failed].get("confirmed") is not True:
            journal["slots"].pop(failed)
    journal.pop("pending", None)
    atomic_json(directory / "slots.json", journal)
    sync_menu(current, directory, system_info(), journal)


@contextlib.contextmanager
def operation_lock():
    fd = os.open(LOCK, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as lock:
        info = os.fstat(lock.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
            raise UpdateError("Die Update-Sperre ist nicht geschützt.")
        try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError: raise UpdateError("Ein Systemupdate oder eine Wiederherstellung läuft bereits.") from None
        yield


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("check", "install", "status", "rollback", "confirm"))
    parser.add_argument("--current-version", required=True)
    parser.add_argument("--channel", choices=("stable",), default="stable")
    parser.add_argument("--version")
    parser.add_argument("--selection")
    parser.add_argument("--port", type=int, default=3001)
    args = parser.parse_args()
    try:
        if args.action == "check":
            print(json.dumps(latest(args.current_version, args.channel), ensure_ascii=False))
        else:
            if os.geteuid() != 0:
                raise UpdateError("Systemaktionen benötigen den geschützten Systemdienst.")
            with operation_lock():
                if args.action == "install":
                    if not args.version: raise UpdateError("Die ausgewählte Update-Version fehlt.")
                    install(args.current_version, args.channel, args.version)
                elif args.action == "status":
                    print(json.dumps(recovery_status(args.current_version), ensure_ascii=False))
                elif args.action == "rollback":
                    if not args.selection or not re.fullmatch(r"[a-f0-9]{64}", args.selection):
                        raise UpdateError("Ungültige Auswahl des vorherigen Systemstands.")
                    rollback(args.current_version, args.selection)
                elif args.action == "confirm":
                    if not 1 <= args.port <= 65535: raise UpdateError("Ungültiger Verwaltungsport.")
                    confirm(args.current_version, args.port)
        return 0
    except (UpdateError, OSError, ValueError, subprocess.SubprocessError) as error:
        if args.action in ("install", "rollback"): status(error=str(error))
        else: print(str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
