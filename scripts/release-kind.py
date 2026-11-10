#!/usr/bin/env python3
"""Decide whether a tag becomes an app release or a full system release.

An app release replaces titand alone (see docs/APP-UPDATES.md). It is made
on the system image of the newest full release before it (the base), and
only if nothing but titand and its web UI changed since that release: the
code under backend/ and web/, documentation, browser tests, the example
agent and the image of the remote view (viewer/), which are images of
their own. Everything else, and a tag message
with "[system]", makes a full release.

Prints kind=app|system and base=<version> as lines for $GITHUB_OUTPUT.

Usage: release-kind.py <version> [--tag-message FILE] [--source URL]
"""
import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

VERSION = re.compile(r"([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([a-z0-9.]+))?")

# What an app release may change; anything else needs a new system image.
ALLOWED = (
    "backend/",
    "web/",
    "docs/",
    "scripts/test-ui-",
    "scripts/test-files-ui.mjs",
    # QEMU test of the key plugin route; runs in CI, not part of the image.
    # This decision and its tests run only in CI.
    "scripts/release-kind.py",
    "scripts/tests/test_release_kind.py",
    # The release workflow runs only in CI and is not in the image; an app
    # release repacks the base image anyway (decision of 4.11.2026). Changes
    # meant for the image itself need "[system]" in the tag message.
    ".github/workflows/release.yml",
    # Signs and publishes plugin sets in CI (PLAN 75: newest draft per tag);
    # nothing of it is in the image.
    ".github/workflows/plugin-sign.yml",
    # The image of the remote view (PLAN 63) is a release asset of its own,
    # built for every release and loaded by titand; it is not in the system
    # squashfs. make-release.sh and third-party-licenses.py only pack it.
    "viewer/",
    "scripts/build-viewer-image.sh",
    # Release tooling that runs only in CI (manifest, license inventory of the
    # release archive, release checks, the build workflow); an app release
    # uses it to repack the base image anyway (decision of 7.11.2026, PLAN 63).
    # THIRD_PARTY_LICENSES is the overview in the repository; the copy in the
    # image follows with the next full release, the release archive carries
    # the complete inventory of every release.
    "scripts/make-release.sh",
    # repack-release.py makes the app release itself in CI and is never in
    # an image (9 Nov 2026: retries for GitHub's 504 on release downloads).
    "scripts/repack-release.py",
    "scripts/third-party-licenses.py",
    "scripts/verify-release.py",
    "scripts/tests/test_release.py",
    ".github/workflows/build.yml",
    "THIRD_PARTY_LICENSES",
    "scripts/viewer-image-reuse.py",
    "scripts/tests/test_viewer_image_reuse.py",
    # The web apps (PLAN 66) are release assets fetched and checked in CI,
    # installed by titand onto a data volume; nothing of them is in the image.
    "webapps/",
    "scripts/fetch-webapps.py",
    "scripts/tests/test_fetch_webapps.py",
    # The example agent is published as its own image, not in the system.
    "examples/",
    ".github/workflows/agent.yml",
    "README.md",
    # Former name of CLAUDE.md; removing it is no system change.
    "AGENTS.md",
    "CLAUDE.md",
    "CONTRIBUTING.md",
    "SECURITY.md",
    # Only git reads it; nothing of it reaches the image (PLAN 72: the
    # cleanup of 8.11.2026 made 0.67.0 and 0.68.0 full releases by mistake).
    ".gitignore",
    # A Python wheel checked in by accident and removed on 8.11.2026; it was
    # never part of the image.
    "shellcheck_py-0.11.0.1-",
    # Checks of the QEMU storage test (smoke-storage.py) that run in CI against
    # a full image; they are not in the image, and an app release does not run
    # them (PR #289 fixed only the trusted certificate there).
    "scripts/workflows023_smoke.py",
    # Takes the README pictures against vite dev with test data; nothing of it
    # is in the image (v0.70.0 became a full release by mistake, 8.11.2026).
    "scripts/capture-readme-screenshots.mjs",
    # PLAN 76 (F-13-4): tools that run only in CI or on the developer's
    # machine and are in no image – the QEMU smoke tests, every test of the
    # scripts, the nightly check for Debian updates, the autopilot and the
    # plugin sets. What the image build itself uses (make-zip.sh,
    # make-grub.sh, stick-bios.py, grubenv.py, check-image-hardware.py,
    # normalize-image-tmpfiles.py, fetch-pci-ids.sh) stays outside this list.
    "scripts/smoke-",
    "scripts/vm_snapshot_smoke.py",
    "scripts/tests/",
    "scripts/autopilot.py",
    "scripts/debian-security.py",
    "scripts/plugin-set.py",
    ".github/workflows/autopilot.yml",
)


def key(version):
    match = VERSION.fullmatch(version)
    if not match:
        return None
    major, minor, patch, pre = match.groups()
    # A release sorts after every pre-release of the same version.
    parts = [(0, int(p)) if p.isdigit() else (1, p) for p in (pre or "").split(".")] if pre else []
    return (int(major), int(minor), int(patch), 0 if pre else 1, parts)


def git(*args):
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


GITHUB_DOWNLOAD = re.compile(r"^https://github\.com/([^/]+/[^/]+)/releases/download/([^/]+)/([^/?#]+)$")


def api_headers(accept):
    headers = {"User-Agent": "TitanOS release", "Accept": accept}
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if token:
        headers["Authorization"] = "Bearer " + token
    return headers


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def api_asset(url, timeout):
    """The file of a github.com release download through the API, or None
    when the address is no such download or the release has no such file."""
    match = GITHUB_DOWNLOAD.match(url)
    if not match:
        return None
    repo, tag, name = match.groups()
    request = urllib.request.Request(f"https://api.github.com/repos/{repo}/releases/tags/{tag}", headers=api_headers("application/vnd.github+json"))
    with urllib.request.urlopen(request, timeout=timeout) as response:
        release = json.load(response)
    asset = next((a for a in release.get("assets", []) if a.get("name") == urllib.parse.unquote(name)), None)
    if not asset:
        return None
    # The API sends the file from storage elsewhere: that address gets no token.
    request = urllib.request.Request(asset["url"], headers=api_headers("application/octet-stream"))
    try:
        return urllib.request.build_opener(_NoRedirect).open(request, timeout=timeout)
    except urllib.error.HTTPError as error:
        if error.code not in (301, 302, 303, 307, 308):
            raise
        location = error.headers["Location"]
    if not location.startswith("https://"):
        raise SystemExit("insecure redirect: " + url)
    return urllib.request.urlopen(urllib.request.Request(location, headers={"User-Agent": "TitanOS release"}), timeout=timeout)


def open_release(url, timeout=120, attempts=4, pause=10):
    """Opens a release download. Server errors and broken connections are
    tried again; then a github.com file comes through the API. GitHub
    answered the download address of single files with 504 for hours while
    the API delivered them (9 Nov 2026, releases of 0.70.2 and 0.71.0).
    A 404 raises at once, as from urlopen."""
    failure = None
    for attempt in range(1, attempts + 1):
        try:
            return urllib.request.urlopen(urllib.request.Request(url, headers={"User-Agent": "TitanOS release"}), timeout=timeout)
        except urllib.error.HTTPError as error:
            if error.code < 500:
                raise
            failure = error
        except (urllib.error.URLError, TimeoutError, ConnectionError) as error:
            failure = error
        if attempt < attempts:
            print(f"{url}: try {attempt} failed ({failure}), again in {pause * attempt} s", flush=True)
            time.sleep(pause * attempt)
    response = api_asset(url, timeout)
    if response is None:
        raise failure
    print(f"{url}: through the GitHub API", flush=True)
    return response


def manifest(source, tag):
    url = f"{source}/releases/download/{tag}/manifest.json"
    try:
        with open_release(url, timeout=60) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return None  # a tag that was never published
        raise


def decide(version, message, source, changed_since=None, fetch=manifest, tags=None):
    """Returns (kind, base, reason)."""
    if "[system]" in message:
        return "system", version, "the tag asks for a full release"
    own = key(version)
    if own is None:
        raise ValueError(f"not a version: {version}")
    tags = tags if tags is not None else git("tag", "-l", "v*").split()
    earlier = sorted((t for t in tags if key(t[1:]) and key(t[1:]) < own), key=lambda t: key(t[1:]), reverse=True)
    previous = None
    for tag in earlier:
        previous = fetch(source, tag)
        if previous:
            break
    if not previous or not isinstance(previous.get("app"), dict):
        return "system", version, "no earlier release with a base version"
    base = previous["app"]["base"]
    if key(base) is None:
        raise ValueError(f"odd base version in {previous['tag']}: {base}")
    changed = changed_since(f"v{base}") if changed_since else git("diff", "--name-only", f"v{base}..HEAD").split()
    outside = [path for path in changed if not path.startswith(ALLOWED)]
    if outside:
        return "system", version, f"changed since v{base}: " + ", ".join(outside[:10])
    return "app", base, f"only titand changed since v{base}"


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("version")
    parser.add_argument("--tag-message", default="")
    parser.add_argument("--source", default="https://github.com/ra5on/TitanOS")
    args = parser.parse_args()
    message = open(args.tag_message, encoding="utf-8").read() if args.tag_message else ""
    kind, base, reason = decide(args.version, message, args.source.rstrip("/"))
    print(f"{kind} release on base {base}: {reason}", file=sys.stderr)
    print(f"kind={kind}")
    print(f"base={base}")


if __name__ == "__main__":
    main()
