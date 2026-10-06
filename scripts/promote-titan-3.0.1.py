#!/usr/bin/env python3
"""Publish only the exact Titan 3.0.1 candidate approved on 2026-10-06.

No signing secret, image rebuild, asset replacement or unrelated release write.
The small signed assets are independently downloaded and verified again.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile


REPOSITORY = "ra5on/TitanOS"
RELEASE_ID = 404374863
TAG = "titan-3.0.1"
SOURCE = "2cfd33a3d9c7001ee4c6ef14f49d3fde22f9e3f8"
IMAGE_RUN = 37418063214
CI_RUNS = (37418062992, 37418069028)
ASSETS = {
    "ab-test.json": (1198, "93fcd3486580c9252f8f480bb4b02d3ac88ad171f80c5ea944c2de995e28edf5"),
    "debian-base.json": (438, "16cadf65d0dc8085beb0842fdb8458e179006a1e41b54c7a887b44aa089596d7"),
    "debian-packages.json": (56057, "4f01927d5997eca6b9476d83265d8055bf9b1af72f6d5197c83e37696ea7d06d"),
    "INSTALLATION.md": (1745, "d916e033f8af194fb844dfcf3e937fc446a8a36ca8f4f1c9768de5f15ce27519"),
    "manifest.json": (7225, "9b407fc1eba233f8f162280b7ee41f3f10d4c64bd88ded343bf9d9149fbf443e"),
    "manifest.json.sig": (64, "a19dfb11e7d6545c57040d7bcecd92195140bcd9c51ad5874d608cf9615a3cce"),
    "rauc-root.pem": (558, "1f73399b8f7413c0156f4d23660551807de00e2928772dff38fa227490f14cbb"),
    "release-public.pem": (113, "503c465e8ab95c6b59f6b8e56a8f913ea4afa975ba698e98eebb089bf4b11ee3"),
    "runtime-test.json": (10068, "a3ba22799bb9ea64b0b1d6f237f16412f488b1cdc3afa75409ea3bf177fb74cf"),
    "SHA256SUMS": (840, "8121944305244d8bffde02d2a6ae56b30b4f6e85aef18664faf42d06a727473b"),
    "SHA256SUMS.sig": (64, "9dc1ca91879dc50804eb5f64519d5582f66372b9b7be6b2d0d7b0b2cda43cab1"),
    "titan-3.0.1-amd64.img.xz": (1192493740, "687164133cffe0f48b2144da0a5b3942f8001b2808a4d4207c071e392e8ac1f9"),
    "titan-3.0.1-amd64.raucb": (1403573517, "ec4682c203aa7ba4818771d369ead3970b6ed23be2d7d4cb50648514e2caaf11"),
}
NOTES = """# Titan 3.0.1

Der eigene Titan-NAS-Unterbau ist jetzt öffentlich verfügbar. Enthalten sind
die deutsche Desktop-Oberfläche, Docker-Verwaltung mit 374 vorbefüllten
LinuxServer.io- und Big-Bear-Vorlagen, eigene Fotos, Dateiverwaltung,
Benutzer und SMB-Freigaben, Speicherverwaltung und virtuelle Maschinen.

Debian und Titan werden gemeinsam über signierte Systemimages aktualisiert.
Die schreibgeschützten A/B-Systempartitionen ermöglichen Updates und Rollback;
persönliche Daten, Container und VM-Laufwerke liegen auf der Datenpartition.
Ein Betriebssystem-Rollback setzt Container-Datenbanken nicht zurück.

**Download:** [titan-3.0.1-amd64.img.xz](https://github.com/ra5on/TitanOS/releases/download/titan-3.0.1/titan-3.0.1-amd64.img.xz)

## Nachgewiesene Prüfungen

Der [vollständige Image-Lauf](https://github.com/ra5on/TitanOS/actions/runs/37418063214)
ist erfolgreich: Boot und HTTPS, echte NAS-/Docker-/Fotos-/SMB-Funktionen,
schreibgeschütztes System, signiertes A/B-Update, manuelles Rollback,
automatischer Fallback und RAM-Kaltstarts mit 8 → 3 → 8 GiB.
Die Bootstrap-Prüfung verwendet eine ältere signierte Identität derselben
Systemfamilie; sie ist kein Nachweis einer Migration von einem anderen System.

## Zum Start

Frischinstallation: mindestens 8 GB RAM und 64 GB Zielspeicher; UEFI (OVMF)
mit deaktiviertem Secure Boot. DATA wächst auf den restlichen verfügbaren Platz.
Oberfläche: https://NAS-IP:5000. Das Administratorkonto wird zuerst eingerichtet.
Für VMs und Hardware-Durchreichung muss der Host die benötigte Technik bereitstellen.

## Stand der praktischen Erprobung

Reale USB-/GPU-Durchreichung, ein installiertes VM-Gastsystem einschließlich
Browser-Tastatureingabe, weitere Hardware-/Clienttests und ein mehrtägiger
Dauerlauf sind offen. Eine vollständige Wiederherstellung aller Konten,
Speicher und App-Datenbanken auf einem frisch installierten NAS ist noch
nicht implementiert. Die öffentliche Freigabe ersetzt diese Prüfungen nicht.

Details: [Prüfprotokoll](https://github.com/ra5on/TitanOS/blob/main/docs/QA-3.0.0.md)
und [Praxistest](https://github.com/ra5on/TitanOS/blob/main/docs/PRAXISTEST-3.0.0.md).
Die beigefügten signierten Dateien stammen unverändert vom geprüften Build;
die aktuelle Freigabeinformation steht in dieser Release-Beschreibung.
"""


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def api(path, payload=None, binary=False, allow_missing=False):
    command = ["gh", "api", "--method", "GET" if payload is None else "PATCH",
               "-H", "Accept: application/octet-stream" if binary else "Accept: application/vnd.github+json",
               f"repos/{REPOSITORY}/{path}"]
    data = None
    if payload is not None:
        command.extend(["--input", "-"])
        data = json.dumps(payload).encode()
    result = subprocess.run(command, input=data, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=60, check=False)
    if allow_missing and result.returncode != 0 and b"(HTTP 404)" in result.stderr:
        return None
    require(result.returncode == 0, "GitHub request failed")
    return result.stdout if binary else json.loads(result.stdout)


def verify_release(release):
    require(release["id"] == RELEASE_ID and release["tag_name"] == TAG,
            "Unexpected release identity")
    require(release["target_commitish"] == SOURCE and release["prerelease"] is False,
            "Unexpected candidate source or channel")
    assets = {asset["name"]: asset for asset in release["assets"]}
    require(len(assets) == len(release["assets"]) and set(assets) == set(ASSETS),
            "Missing, duplicate or unexpected release asset")
    for name, (size, digest) in ASSETS.items():
        asset = assets[name]
        require(asset["state"] == "uploaded" and asset["size"] == size
                and asset["digest"] == f"sha256:{digest}",
                f"Verified asset changed: {name}")
    return assets


def verify_tag(allow_missing=False):
    ref = api(f"git/ref/tags/{TAG}", allow_missing=allow_missing)
    if ref is None:
        require(allow_missing, "Published tag is missing")
        return
    obj = ref["object"]
    for _ in range(5):
        require(re.fullmatch(r"[0-9a-f]{40}", obj.get("sha", "")) is not None,
                "Unexpected tag object identity")
        if obj.get("type") == "commit":
            require(obj["sha"] == SOURCE, "Release tag does not point to the verified source")
            return
        require(obj.get("type") == "tag", "Unsupported release tag object")
        obj = api(f"git/tags/{obj['sha']}")["object"]
    raise RuntimeError("Release tag nesting is too deep")


def main():
    require(os.environ.get("GITHUB_ACTIONS") == "true"
            and os.environ.get("GITHUB_REPOSITORY") == REPOSITORY,
            "Only the authorized repository workflow may publish")
    require(bool(os.environ.get("GH_TOKEN")), "GitHub workflow token is required")
    for run_id in (IMAGE_RUN, *CI_RUNS):
        run = api(f"actions/runs/{run_id}")
        require(run["status"] == "completed" and run["conclusion"] == "success"
                and run["head_sha"] == SOURCE,
                "The exact candidate has not passed its required workflow")
    release = api(f"releases/{RELEASE_ID}")
    assets = verify_release(release)
    verify_tag(allow_missing=release["draft"] is True)
    with tempfile.TemporaryDirectory(prefix="titan-publication-") as folder:
        directory = Path(folder)
        for name, (size, digest) in ASSETS.items():
            if size > 1024 * 1024:
                continue  # Original large assets were verified by the image workflow.
            data = api(f"releases/assets/{assets[name]['id']}", binary=True)
            require(len(data) == size and hashlib.sha256(data).hexdigest() == digest,
                    f"Downloaded asset digest mismatch: {name}")
            (directory / name).write_bytes(data)
        for name in ("release-public.pem", "rauc-root.pem"):
            require((directory / name).read_bytes() == (Path("packaging") / name).read_bytes(),
                    "Candidate does not use the repository's trusted keys")
        for name in ("SHA256SUMS", "manifest.json"):
            subprocess.run(["openssl", "pkeyutl", "-verify", "-rawin", "-pubin",
                            "-inkey", "packaging/release-public.pem", "-in", str(directory / name),
                            "-sigfile", str(directory / f"{name}.sig")], check=True, timeout=30)
        checksums = {}
        for line in (directory / "SHA256SUMS").read_text().splitlines():
            digest, name = line.split("  ", 1)
            require(name in ASSETS and name not in checksums, "Unexpected signed checksum")
            require(digest == ASSETS[name][1], "Signed checksum does not match candidate")
            checksums[name] = digest
        require({"titan-3.0.1-amd64.img.xz", "titan-3.0.1-amd64.raucb",
                 "manifest.json", "manifest.json.sig", "runtime-test.json", "ab-test.json",
                 "INSTALLATION.md", "debian-base.json", "debian-packages.json", "rauc-root.pem"}
                == set(checksums), "Incomplete signed checksum inventory")
        runtime = json.loads((directory / "runtime-test.json").read_text())
        ab = json.loads((directory / "ab-test.json").read_text())
        require(runtime.get("ok") is True and ab.get("ok") is True
                and runtime.get("raw_image_unchanged") is True
                and ab.get("raw_image_unchanged") is True
                and ab.get("bundle_unchanged") is True,
                "Runtime or A/B proof is incomplete")
        guard = ab.get("boot_memory_guard", {})
        require(len(guard) == 11 and all(value is True for value in guard.values()),
                "Reduced-RAM boot proof is incomplete")
        protected = next(check for check in runtime["checks"] if check["name"] == "os_root_protection")
        require(protected["status"] == "passed" and len(protected["values"]) == 10
                and all(value is True for value in protected["values"].values()),
                "Read-only OS proof is incomplete")
    # Check again immediately before the one bounded mutation; never upload/delete assets.
    current = api(f"releases/{RELEASE_ID}")
    verify_release(current)
    if current["draft"] is False:
        verify_tag()
        print(f"Candidate already published; no changes: {current['html_url']}")
        return  # A rerun must not demote a newer release by changing latest.
    require(current["draft"] is True, "Unexpected publication state")
    verify_tag(allow_missing=True)
    published = api(f"releases/{RELEASE_ID}", {
        "draft": False, "prerelease": False, "make_latest": "true",
        "name": "Titan 3.0.1", "body": NOTES,
    })
    verify_release(published)
    require(published["draft"] is False, "Release publication did not complete")
    verify_tag()
    print(f"Published verified candidate: {published['html_url']}")


if __name__ == "__main__":
    main()
