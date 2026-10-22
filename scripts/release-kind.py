#!/usr/bin/env python3
"""Decide whether a tag becomes an app release or a full system release.

An app release replaces titand alone (see docs/APP-UPDATES.md). It is made
on the system image of the newest full release before it (the base), and
only if nothing but titand and its web UI changed since that release: the
code under backend/ and web/, documentation, browser tests and the example
agent, which is an image of its own. Everything else, and a tag message
with "[system]", makes a full release.

Prints kind=app|system and base=<version> as lines for $GITHUB_OUTPUT.

Usage: release-kind.py <version> [--tag-message FILE] [--source URL]
"""
import argparse
import json
import re
import subprocess
import sys
import urllib.error
import urllib.request

VERSION = re.compile(r"([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([a-z0-9.]+))?")

# What an app release may change; anything else needs a new system image.
ALLOWED = (
    "backend/",
    "web/",
    "docs/",
    "scripts/test-ui-",
    "scripts/test-files-ui.mjs",
    # The example agent is published as its own image, not in the system.
    "examples/",
    ".github/workflows/agent.yml",
    "README.md",
    "AGENTS.md",
    "CLAUDE.md",
    "CONTRIBUTING.md",
    "SECURITY.md",
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


def manifest(source, tag):
    url = f"{source}/releases/download/{tag}/manifest.json"
    try:
        with urllib.request.urlopen(urllib.request.Request(url, headers={"User-Agent": "TitanOS release"}), timeout=60) as response:
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
