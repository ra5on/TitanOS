#!/usr/bin/env python3
"""Reuse the viewer image of the previous release when nothing in it changed.

Every release carries titanos-viewer-<version>.tar.zst (docs/FERNANSICHT.md).
Rebuilding it gives a new image ID each time, so every NAS would download
about 320 MB again. If nothing that goes into the image changed since the
newest earlier release that has one, this takes that release's archive:

  * its manifest.json must carry a valid signature of a trusted key,
  * the archive must match the size and SHA-256 of the manifest's "viewer",
  * the package list comes from that release's licenses archive, whose
    checksum is in its signed SHA256SUMS.

It is never reused (Chromium security updates):

  * when the previous image is older than 7 days (the date of the oldest
    release that carries the same image ID), or
  * when Debian's candidate version (trixie + trixie-security, asked in
    the pinned base image) of a security-relevant package (chromium*,
    libx264*, gstreamer*, mesa* …) differs from that release's
    packages.txt, or the candidates cannot be determined.

It writes <out-dir>/titanos-viewer-<version>.tar.zst, .packages.txt and
.image-id (the same image ID) and exits 0. Exit code 3 means: build it
(changes, or no earlier release with an image). Anything else is an error.

Usage: viewer-image-reuse.py <version> <out-dir> [--keys DIR] [--source URL]
"""
import argparse
import hashlib
import http.client
import importlib.util
import io
import json
import re
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
# What goes into the image: its recipe, the agent and the protocol it shares
# with titand, and the build script.
INPUTS = (
    "viewer/",
    "backend/cmd/titanos-viewer/",
    "backend/internal/viewer/proto/",
    "scripts/build-viewer-image.sh",
)
# Go dependencies change the agent too – but only those it is built from.
# 0.96 added a library for titand alone and every NAS loaded 320 MB again
# (owner 16 Nov 2026). When these files changed, the modules of the agent
# are compared (agent_modules); only a difference there builds the image.
MODULE_FILES = ("backend/go.mod", "backend/go.sum")
AGENT = "./cmd/titanos-viewer"
BUILD = 3
# An image is rebuilt after this long even without changes (security updates).
MAX_AGE = 7 * 24 * 3600
# Packages whose updates force a new image.
SECURITY = re.compile(r"^(chromium.*|libx264.*|gstreamer.*|libgstreamer.*|mesa.*|libgl1-mesa.*|libegl-mesa.*|libglx-mesa.*|libgbm.*|libdrm.*|libva.*|intel-media-va-driver.*)$")

spec = importlib.util.spec_from_file_location("release_kind", REPO / "scripts/release-kind.py")
release_kind = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release_kind)


# A connection that breaks off in the middle of a file: the server closed it
# early, reset it or stopped sending.
BROKEN = (http.client.IncompleteRead, ConnectionError, TimeoutError)
# How often a download that broke off in the middle starts again (B-11).
BODY_ATTEMPTS = 3


def check_length(response, got):
    """Raises IncompleteRead when fewer bytes came than the server announced:
    read(n) of http.client returns an early end of the connection as the end
    of the file instead of raising."""
    announced = response.headers.get("Content-Length")
    if announced and announced.isdigit() and got < int(announced):
        raise http.client.IncompleteRead(b"", int(announced) - got)


def broke_off(url, attempt, error, pause):
    """Raises on the last attempt, else waits before the next one."""
    if attempt == BODY_ATTEMPTS:
        raise error
    print(f"{url}: download broke off ({error!r}), again in {pause * attempt} s", flush=True)
    time.sleep(pause * attempt)


def fetch(url, limit, pause=10):
    # Through release_kind.open_release: tried again, then the GitHub API.
    # A body that breaks off is read again from the start.
    for attempt in range(1, BODY_ATTEMPTS + 1):
        try:
            with release_kind.open_release(url, pause=pause) as response:
                data = response.read(limit + 1)
                if len(data) <= limit:
                    check_length(response, len(data))
            break
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return None
            raise
        except BROKEN as error:
            broke_off(url, attempt, error, pause)
    if len(data) > limit:
        raise ValueError(f"{url} is larger than expected")
    return data


def signed(data, signature, keys):
    """Whether data carries a valid minisign signature of one of keys."""
    with tempfile.TemporaryDirectory() as tmp:
        message, sig = Path(tmp) / "message", Path(tmp) / "message.minisig"
        message.write_bytes(data)
        sig.write_bytes(signature)
        for key in sorted(Path(keys).glob("*.pub")):
            if subprocess.run(["minisign", "-V", "-q", "-p", str(key), "-m", str(message), "-x", str(sig)],
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
                return True
    return False


def previous(version, tags, manifest_of):
    """The newest earlier release whose verified manifest has a viewer image."""
    own = release_kind.key(version)
    earlier = sorted((t for t in tags if release_kind.key(t[1:]) and release_kind.key(t[1:]) < own),
                     key=lambda t: release_kind.key(t[1:]), reverse=True)
    for tag in earlier:
        manifest = manifest_of(tag)
        if manifest and isinstance(manifest.get("viewer"), dict):
            return tag, manifest
    return None, None


def decide(version, tags, manifest_of, changed_since, modules_differ=None):
    """Returns (tag, manifest) to reuse, or (None, reason).

    modules_differ(tag) says why the Go modules of the agent differ from
    those at tag ("" when they are the same); without it, any change to
    go.mod or go.sum builds the image.
    """
    tag, manifest = previous(version, tags, manifest_of)
    if not tag:
        return None, "no earlier release with a viewer image"
    paths = changed_since(tag)
    changed = [p for p in paths if p.startswith(INPUTS)]
    if changed:
        return None, f"changed since {tag}: " + ", ".join(changed[:10])
    modules = [p for p in paths if p in MODULE_FILES]
    if modules:
        reason = modules_differ(tag) if modules_differ else "not compared"
        if reason:
            return None, f"changed since {tag}: " + ", ".join(modules) + f" ({reason})"
    return tag, manifest


def agent_modules(backend, run=subprocess.run):
    """The Go version and the modules the agent of the image is built from."""
    result = run(["go", "list", "-deps", "-f",
                  "{{with .Module}}{{.Path}} {{.Version}}{{with .Replace}} => {{.Path}} {{.Version}}{{end}}{{end}}", AGENT],
                 cwd=backend, capture_output=True, text=True, timeout=600)
    if result.returncode != 0:
        raise RuntimeError(f"go list failed: {result.stderr.strip()[-300:]}")
    modules = {line for line in result.stdout.splitlines() if line.strip()}
    # The module of TitanOS itself has no version; its Go version counts.
    version = run(["go", "list", "-m", "-f", "{{.GoVersion}}"], cwd=backend, capture_output=True, text=True, timeout=120)
    if version.returncode != 0:
        raise RuntimeError(f"go list -m failed: {version.stderr.strip()[-300:]}")
    return {"go " + version.stdout.strip()} | modules


def modules_differ(tag, run=subprocess.run):
    """Why the agent's modules differ from those at tag, or "".

    The tree of the tag is checked out beside this one. Whatever cannot be
    compared counts as a difference: the image is built rather than reused
    by mistake.
    """
    try:
        now = agent_modules(REPO / "backend", run)
        with tempfile.TemporaryDirectory() as tmp:
            tree = Path(tmp) / "tree"
            release_kind.git("worktree", "add", "--detach", str(tree), tag)
            try:
                then = agent_modules(tree / "backend", run)
            finally:
                release_kind.git("worktree", "remove", "--force", str(tree))
    except (RuntimeError, OSError, subprocess.SubprocessError) as error:
        return f"modules of the agent cannot be compared: {error}"
    if now == then:
        return ""
    return "modules of the agent: " + ", ".join(sorted(now ^ then)[:6])


def pinned_base():
    """The base image of viewer/Dockerfile (name@sha256:…)."""
    for line in (REPO / "viewer/Dockerfile").read_text().splitlines():
        m = re.fullmatch(r"FROM\s+(\S+@sha256:[0-9a-f]{64})\s*", line)
        if m:
            return m.group(1)
    raise ValueError("viewer/Dockerfile has no pinned FROM")


def parse_packages(text):
    """name → version of a packages.txt (name version source [source-version])."""
    out = {}
    for line in text.splitlines():
        fields = line.split()
        if len(fields) >= 2:
            out[fields[0]] = fields[1]
    return out


def parse_policy(text):
    """name → candidate version from `apt-cache policy` output."""
    out, name = {}, None
    for line in text.splitlines():
        if line and not line[0].isspace() and line.endswith(":"):
            name = line[:-1]
        elif name and line.strip().startswith("Candidate:"):
            out[name] = line.split(":", 1)[1].strip()
    return out


def candidate_versions(names, run=subprocess.run):
    """Candidate versions in Debian (with trixie-security, as the image build
    sees it) via the pinned base image."""
    if not names:
        return {}
    for name in names:
        if not re.fullmatch(r"[a-z0-9][a-z0-9.+-]*", name):
            raise ValueError(f"odd package name {name!r}")
    result = run(["docker", "run", "--rm", pinned_base(), "sh", "-c",
                  "apt-get update -qq >/dev/null && apt-cache policy " + " ".join(sorted(names))],
                 capture_output=True, text=True, timeout=600)
    if result.returncode != 0:
        raise RuntimeError(f"apt-cache policy failed: {result.stderr.strip()[-300:]}")
    return parse_policy(result.stdout)


def image_built_at(tag, tags, manifest_of, tag_time):
    """Unix time of the oldest release that carries the image of tag (a
    reused image keeps its ID, so the chain goes back to its build)."""
    image = manifest_of(tag)["viewer"]["imageId"]
    oldest = tag
    earlier = sorted((t for t in tags if release_kind.key(t[1:]) and release_kind.key(t[1:]) < release_kind.key(tag[1:])),
                     key=lambda t: release_kind.key(t[1:]), reverse=True)
    for t in earlier:
        m = manifest_of(t)
        if m is None:
            continue  # a tag never published (0.67.0, 0.68.0) carries no image
        if not isinstance(m.get("viewer"), dict) or m["viewer"].get("imageId") != image:
            break
        oldest = t
    return tag_time(oldest)


def security_reason(packages_text, candidates, built_at, now, max_age=MAX_AGE):
    """Why the previous image must not be reused (None: it may)."""
    if now - built_at > max_age:
        return f"the image is {int((now - built_at) / 86400)} days old (more than {max_age // 86400})"
    old = {n: v for n, v in parse_packages(packages_text).items() if SECURITY.match(n)}
    if not old or "chromium" not in old:
        return "no chromium in the previous package list"
    changed = [f"{n} {v} → {candidates.get(n, 'unknown')}" for n, v in sorted(old.items()) if candidates.get(n) != v]
    if changed:
        return "security-relevant updates: " + ", ".join(changed[:10])
    return None


def previous_packages(tag, source, keys):
    """packages.txt of tag from its licenses archive (checked against the
    signed SHA256SUMS)."""
    base = f"{source}/releases/download/{tag}/"
    old_version = tag[1:]
    # The licenses archive is covered by the signed SHA256SUMS.
    sums, sums_sig = fetch(base + "SHA256SUMS", 1 << 20), fetch(base + "SHA256SUMS.minisig", 4096)
    if not sums or not sums_sig or not signed(sums, sums_sig, keys):
        raise ValueError(f"SHA256SUMS of {tag} is not signed by a trusted key")
    checksums = dict(reversed(line.split("  ", 1)) for line in sums.decode().splitlines() if "  " in line)
    licenses_name = f"titanos-{old_version}-licenses.tar.gz"
    licenses = fetch(base + licenses_name, 512 << 20)
    if not licenses or hashlib.sha256(licenses).hexdigest() != checksums.get(licenses_name):
        raise ValueError(f"{licenses_name} does not match SHA256SUMS")
    with tarfile.open(fileobj=io.BytesIO(licenses)) as archive:
        return archive.extractfile("viewer/packages.txt").read()


def reuse(version, out, tag, manifest, source, keys, packages=None, pause=10):
    base = f"{source}/releases/download/{tag}/"
    viewer = manifest["viewer"]
    old_version = tag[1:]
    if viewer.get("asset") != f"titanos-viewer-{old_version}.tar.zst" or not re.fullmatch(r"sha256:[0-9a-f]{64}", str(viewer.get("imageId", ""))):
        raise ValueError(f"odd viewer entry in {tag}")
    if packages is None:
        packages = previous_packages(tag, source, keys)
    out.mkdir(parents=True, exist_ok=True)
    target = out / f"titanos-viewer-{version}.tar.zst"
    # Tried again on a server error (PLAN 76, F-13-13): one 5xx from GitHub
    # on this large file must not end the release run. A download that
    # breaks off in the middle starts again from the beginning, the partial
    # file removed first (B-11).
    for attempt in range(1, BODY_ATTEMPTS + 1):
        digest, size = hashlib.sha256(), 0
        try:
            with release_kind.open_release(base + viewer["asset"], timeout=600, pause=pause) as response, target.open("wb") as f:
                for block in iter(lambda: response.read(1 << 20), b""):
                    size += len(block)
                    if size > viewer["size"]:
                        raise ValueError("viewer archive larger than its manifest says")
                    digest.update(block)
                    f.write(block)
                check_length(response, size)
            break
        except BROKEN as error:
            target.unlink(missing_ok=True)
            broke_off(base + viewer["asset"], attempt, error, pause)
    if size != viewer["size"] or digest.hexdigest() != viewer["sha256"]:
        target.unlink()
        raise ValueError(f"viewer archive of {tag} does not match its signed manifest")
    (out / f"titanos-viewer-{version}.packages.txt").write_bytes(packages)
    (out / f"titanos-viewer-{version}.image-id").write_text(viewer["imageId"] + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("version")
    parser.add_argument("out", type=Path)
    parser.add_argument("--keys", default=str(REPO / "build/mkosi.extra/usr/share/titanos/update-keys"))
    parser.add_argument("--source", default="https://github.com/ra5on/TitanOS")
    args = parser.parse_args()
    source = args.source.rstrip("/")

    def manifest_of(tag):
        data = fetch(f"{source}/releases/download/{tag}/manifest.json", 1 << 20)
        sig = fetch(f"{source}/releases/download/{tag}/manifest.json.minisig", 4096)
        if not data or not sig or not signed(data, sig, args.keys):
            return None
        manifest = json.loads(data)
        return manifest if manifest.get("tag") == tag else None

    tags = release_kind.git("tag", "-l", "v*").split()
    tag, manifest = decide(args.version, tags, manifest_of,
                           release_kind.changed_files, modules_differ)
    if not tag:
        print(f"building the viewer image: {manifest}", file=sys.stderr)
        sys.exit(BUILD)
    packages = previous_packages(tag, source, args.keys)
    built = image_built_at(tag, tags, manifest_of,
                           lambda t: int(release_kind.git("for-each-ref", "--format=%(creatordate:unix)", f"refs/tags/{t}").strip()))
    try:
        names = [n for n in parse_packages(packages.decode()) if SECURITY.match(n)]
        reason = security_reason(packages.decode(), candidate_versions(names), built, time.time())
    except (RuntimeError, ValueError, OSError, subprocess.TimeoutExpired) as error:
        reason = f"cannot check Debian updates ({error})"
    if reason:
        print(f"building the viewer image: {reason}", file=sys.stderr)
        sys.exit(BUILD)
    reuse(args.version, args.out, tag, manifest, source, args.keys, packages)
    print(f"viewer image of {tag} reused ({manifest['viewer']['imageId']})", file=sys.stderr)


if __name__ == "__main__":
    main()
