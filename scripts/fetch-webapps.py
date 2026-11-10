#!/usr/bin/env python3
"""Fetch and check the web apps of a release (PLAN 66, docs/WEBAPPS.md).

For every app in webapps/catalog.json: download <upstreamAsset> of the
GitHub release v<version> of <repo> (no GitHub API), check size and SHA-256
against the catalog, check its contents with the same rules titand applies
when unpacking it, and write it unchanged as titanos-webapp-<id>-<version>.zip
(so its SHA-256 stays the catalog's). Also writes

  webapps.json       the manifest entries [{"id","version","asset","sha256","size"}]
  licenses/<id>/     the license and notice files of each app (from the archive;
                     LICENSE-MIT/LICENSE-APACHE from the repository at the tag
                     if the archive lacks them)

Usage: fetch-webapps.py <out-dir> [--catalog FILE] [--source DIR]
  --source DIR   take the archives from DIR (upstream names) instead of GitHub
"""
import argparse
import hashlib
import json
import re
import shutil
import stat
import sys
import time
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
MAX_ARCHIVE = 256 << 20
MAX_UNPACKED = 256 << 20
MAX_FILES = 2000
ID = re.compile(r"[a-z][a-z0-9-]{0,31}")
VERSION = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")
SHA = re.compile(r"[0-9a-f]{64}")
# License and notice files copied into the release's license archive.
NOTICE = re.compile(r"(?i)(LICEN[CS]E[^/]*|COPYING[^/]*|NOTICE[^/]*|OFL[^/]*\.txt|ATTRIBUTION[^/]*|ASSETS\.md)")


def load_catalog(path):
    apps = json.loads(Path(path).read_text())["apps"]
    seen = set()
    for app in apps:
        if not ID.fullmatch(app["id"]) or app["id"] in seen or not VERSION.fullmatch(app["version"]) \
                or app["upstreamAsset"] != f"{app['id']}-web-{app['version']}.zip" or not SHA.fullmatch(app["sha256"]) \
                or not 0 < app["size"] <= MAX_ARCHIVE or app["bridge"] not in ("host", "adapter") \
                or not re.fullmatch(r"[A-Za-z0-9-]+/[A-Za-z0-9._-]+", app["repo"]):
            raise ValueError("invalid catalog entry: " + str(app.get("id")))
        seen.add(app["id"])
    return apps


def asset_name(app):
    return f"titanos-webapp-{app['id']}-{app['version']}.zip"


def digest(path):
    with open(path, "rb") as f:
        return hashlib.file_digest(f, "sha256").hexdigest()


def safe_name(rel):
    rel = rel.rstrip("/")
    if not rel or len(rel) > 512 or "\\" in rel or "\x00" in rel or rel.startswith("/"):
        return False
    return all(part not in ("", ".", "..") and len(part) <= 255 for part in rel.split("/"))


def validate_archive(path, app_id, version):
    """The rules of titand (backend/internal/webapps/archive.go): every entry
    below <id>-web-<version>/, only regular files and folders, no unsafe or
    repeated names, at most MAX_FILES files and MAX_UNPACKED bytes as read,
    index.html at the top. Returns the names below the top folder."""
    prefix = f"{app_id}-web-{version}/"
    names, seen, total, files = [], set(), 0, 0
    with zipfile.ZipFile(path) as archive:
        for info in archive.infolist():
            name = info.filename
            if not name.startswith(prefix):
                raise ValueError(f"{path}: entry {name!r} outside {prefix}")
            if name == prefix:
                continue
            rel = name[len(prefix):]
            if not safe_name(rel):
                raise ValueError(f"{path}: unsafe name {name!r}")
            if rel.rstrip("/").lower() in seen:
                raise ValueError(f"{path}: repeated name {name!r}")
            seen.add(rel.rstrip("/").lower())
            # As Go's archive/zip reads it: no type bits are a regular file,
            # a trailing slash a folder.
            kind = stat.S_IFMT(info.external_attr >> 16) if info.create_system == 3 else 0
            is_dir = name.endswith("/")
            if not is_dir and kind == stat.S_IFDIR:
                raise ValueError(f"{path}: {name!r} is not marked as a folder")
            if not is_dir and kind not in (0, stat.S_IFREG):
                raise ValueError(f"{path}: {name!r} is not a regular file")
            if is_dir:
                continue
            if info.compress_type not in (zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED):
                raise ValueError(f"{path}: {name!r} uses an unsupported compression")
            files += 1
            if files > MAX_FILES:
                raise ValueError(f"{path}: more than {MAX_FILES} files")
            with archive.open(info) as stream:
                for block in iter(lambda: stream.read(1 << 20), b""):
                    total += len(block)
                    if total > MAX_UNPACKED:
                        raise ValueError(f"{path}: more than {MAX_UNPACKED >> 20} MiB unpacked")
            names.append(rel)
    if "index.html" not in names:
        raise ValueError(f"{path}: no index.html")
    return names


def open_url(url, attempts=4, pause=10):
    """Opens a download; server errors and broken connections are tried again
    (PLAN 76, F-13-13: one 5xx from GitHub must not end the release run)."""
    request = urllib.request.Request(url, headers={"User-Agent": "TitanOS release"})
    for attempt in range(1, attempts + 1):
        try:
            return urllib.request.urlopen(request, timeout=120)
        except urllib.error.HTTPError as error:
            if error.code < 500 or attempt == attempts:
                raise
            failure = error
        except (urllib.error.URLError, TimeoutError, ConnectionError) as error:
            if attempt == attempts:
                raise
            failure = error
        print(f"{url}: try {attempt} failed ({failure}), again in {pause * attempt} s", flush=True)
        time.sleep(pause * attempt)


def download(url, dest, size):
    with open_url(url) as response, open(dest, "wb") as out:
        read = 0
        for block in iter(lambda: response.read(1 << 20), b""):
            read += len(block)
            if read > size:
                raise ValueError(f"{url}: larger than the catalog says")
            out.write(block)


def copy_licenses(path, app, names, dest, fetch_missing=True):
    prefix = f"{app['id']}-web-{app['version']}/"
    dest.mkdir(parents=True, exist_ok=True)
    found = []
    with zipfile.ZipFile(path) as archive:
        for rel in names:
            if NOTICE.fullmatch(rel.split("/")[-1]):
                target = dest / rel
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(archive.read(prefix + rel))
                found.append(rel)
    for name in ("LICENSE-MIT", "LICENSE-APACHE"):
        if name in found:
            continue
        if not fetch_missing:
            raise ValueError(f"{app['id']}: {name} missing")
        url = f"https://raw.githubusercontent.com/{app['repo']}/v{app['version']}/{name}"
        download(url, dest / name, 1 << 20)
        found.append(name)
    return sorted(found)


def fetch(catalog, out, source=None):
    out = Path(out)
    out.mkdir(parents=True, exist_ok=True)
    licenses = out / "licenses"
    shutil.rmtree(licenses, ignore_errors=True)
    entries = []
    for app in load_catalog(catalog):
        asset = asset_name(app)
        target = out / asset
        part = out / (asset + ".part")
        if source:
            shutil.copyfile(Path(source) / app["upstreamAsset"], part)
        else:
            url = f"https://github.com/{app['repo']}/releases/download/v{app['version']}/{app['upstreamAsset']}"
            download(url, part, app["size"])
        if part.stat().st_size != app["size"] or digest(part) != app["sha256"]:
            part.unlink()
            raise ValueError(f"{app['upstreamAsset']}: size or SHA-256 differs from webapps/catalog.json")
        names = validate_archive(part, app["id"], app["version"])
        part.replace(target)
        copied = copy_licenses(target, app, names, licenses / app["id"], fetch_missing=source is None)
        entries.append({"id": app["id"], "version": app["version"], "asset": asset, "sha256": app["sha256"], "size": app["size"]})
        print(f"{app['id']} {app['version']}: {len(names)} files, licenses: {', '.join(copied)}")
    (out / "webapps.json").write_text(json.dumps(entries, indent=2) + "\n")
    return entries


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("out")
    parser.add_argument("--catalog", default=str(REPO / "webapps/catalog.json"))
    parser.add_argument("--source")
    args = parser.parse_args()
    try:
        fetch(args.catalog, args.out, args.source)
    except (ValueError, OSError, zipfile.BadZipFile) as error:
        sys.exit(f"fetch-webapps: {error}")


if __name__ == "__main__":
    main()
