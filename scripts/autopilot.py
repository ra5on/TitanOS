#!/usr/bin/env python3
"""Security releases without the owner (docs/AUTOPILOT.md).

plan     For the nightly autopilot: is a security release possible now, and
         under which tag? It is cut from the latest published release, as
         the next free patch version. Prints action=tag|wait and, for tag,
         tag=v<x.y.z> and from=v<latest>; reason= says why.
guard    For the release workflow: may this tag be published without the
         owner? Only when its commit is a direct child of a published
         release tag, the version is a later patch of that release and the
         commit changes build/debian-security.txt alone. Prints
         autopilot=true|false. The tag message plays no part.
verify   For the release workflow, after the build: every update listed in
         build/debian-security.txt is in the built image, at that version or
         newer. Otherwise the next night would find it again and cut the
         same release once more.
pending  For the plugin signing: the autopilot tag that waits for its
         release, if any. Prints pending=<tag> or pending=.

Usage:
  autopilot.py plan --latest <version> [--source URL]
  autopilot.py guard <version> [--source URL]
  autopilot.py pending [--source URL]
  autopilot.py verify <list> <components.json> [<components.json of the image with the newer kernel>]
"""
import argparse
import json
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request

STABLE = re.compile(r"v?([0-9]+)\.([0-9]+)\.([0-9]+)")
ANY = re.compile(r"v([0-9]+)\.([0-9]+)\.([0-9]+)(?:-[a-z0-9.]+)?")
# The subject of every tag the autopilot cuts; it opens the changelog too.
MARKER = "(Autopilot)"
MARKER_FILE = "build/debian-security.txt"
# A tag of the owner newer than the latest release is a release on its way
# (approval, plugin sets). The autopilot waits for it this long.
OWNER_GRACE = 3 * 24 * 3600


def stable(version):
    match = STABLE.fullmatch(version)
    return tuple(int(p) for p in match.groups()) if match else None


def numbers(tag):
    match = ANY.fullmatch(tag)
    return tuple(int(p) for p in match.groups()) if match else None


def git(*args):
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout


def published(source, tag):
    """Whether a release of this tag has a manifest, i.e. was published."""
    url = f"{source}/releases/download/{tag}/manifest.json"
    try:
        with urllib.request.urlopen(urllib.request.Request(url, headers={"User-Agent": "TitanOS autopilot"}), timeout=60) as response:
            json.load(response)
            return True
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return False
        raise


def local_tags():
    """[(tag, created unix time, subject)] of all v* tags."""
    out = git("for-each-ref", "refs/tags/v*", "--format=%(refname:short)%09%(creatordate:unix)%09%(contents:subject)")
    tags = []
    for line in out.splitlines():
        name, created, subject = (line.split("\t") + ["", ""])[:3]
        tags.append((name, int(created or 0), subject))
    return tags


def plan(latest, tags, is_published, now):
    """Returns (action, tag, reason)."""
    own = stable(latest)
    if own is None:
        return "wait", "", f"the latest release {latest} is not a stable version"
    later = [(name, created, subject) for name, created, subject in tags
             if numbers(name) and numbers(name) > own and not is_published(name)]
    for name, created, subject in sorted(later, key=lambda t: numbers(t[0])):
        if MARKER in subject:
            # Building, waiting for its plugin set, or failed (an issue says so).
            return "wait", "", f"{name} of the autopilot is not published yet"
        if now - created < OWNER_GRACE:
            return "wait", "", f"{name} of the owner is on its way"
    taken = {numbers(name)[:3] for name, _, _ in tags if numbers(name)}
    major, minor, patch = own
    patch += 1
    while (major, minor, patch) in taken:
        patch += 1
    return "tag", f"v{major}.{minor}.{patch}", f"next free patch after {latest}"


def pending(tags, is_published):
    """The newest autopilot tag without a published release, or ""."""
    ours = [name for name, _, subject in tags if MARKER in subject and stable(name)]
    for name in sorted(ours, key=stable, reverse=True):
        return "" if is_published(name) else name
    return ""


LISTED = re.compile(r"([a-z0-9][a-z0-9.+-]*): (\S+) -> (\S+)")


def listed(text):
    """The updates of build/debian-security.txt: (default, newer kernel),
    each [(name, version)]."""
    base, variant = [], []
    current = base
    for line in text.splitlines():
        if line.startswith("Image with the newer kernel"):
            current = variant
            continue
        match = LISTED.fullmatch(line.strip())
        if match:
            current.append((match[1], match[3]))
    return base, variant


def missing(updates, components, at_least):
    """Updates the image does not have: [(name, wanted, found or None)]."""
    have = {c["name"]: c["version"] for c in components if c.get("ecosystem") == "Debian"}
    return [(name, version, have.get(name)) for name, version in updates
            if name not in have or not at_least(have[name], version)]


def dpkg_at_least(a, b):
    return subprocess.run(["dpkg", "--compare-versions", a, "ge", b]).returncode == 0


def guard(version, head_parents, parent_tags, changed, is_published):
    """Returns (allowed, reason). parent_tags are the tags on the parent commit."""
    own = stable(version)
    if own is None:
        return False, "not a stable version"
    if len(head_parents) != 1:
        return False, "the commit is not a plain child of one release"
    releases = [t for t in parent_tags if stable(t) and stable(t)[:2] == own[:2] and stable(t) < own]
    release = next((t for t in sorted(releases, key=stable, reverse=True) if is_published(t)), None)
    if release is None:
        return False, "the parent commit is no published release of the same minor version"
    if changed != [MARKER_FILE]:
        return False, f"changed since {release}: " + ", ".join(changed[:10] or ["nothing"])
    return True, f"only {MARKER_FILE} changed since {release}"


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    p = sub.add_parser("plan")
    p.add_argument("--latest", required=True)
    p.add_argument("--source", default="https://github.com/ra5on/TitanOS")
    g = sub.add_parser("guard")
    g.add_argument("version")
    g.add_argument("--source", default="https://github.com/ra5on/TitanOS")
    w = sub.add_parser("pending")
    w.add_argument("--source", default="https://github.com/ra5on/TitanOS")
    v = sub.add_parser("verify")
    v.add_argument("list")
    v.add_argument("components")
    v.add_argument("components_new", nargs="?")
    args = parser.parse_args()
    if args.command == "verify":
        base, variant = listed(open(args.list, encoding="utf-8").read())
        if variant and not args.components_new:
            raise SystemExit("updates for the image with the newer kernel, but no such image")
        wrong = missing(base, json.load(open(args.components, encoding="utf-8")), dpkg_at_least)
        if variant:
            wrong += missing(variant, json.load(open(args.components_new, encoding="utf-8")), dpkg_at_least)
        for name, wanted, found in wrong:
            print(f"{name}: wanted {wanted}, the image has {found or 'nothing'}", file=sys.stderr)
        if wrong:
            raise SystemExit("the image lacks listed updates; publishing it would repeat the release every night")
        print(f"all {len(base) + len(variant)} listed updates are in the image")
        return
    source = args.source.rstrip("/")
    check = lambda tag: published(source, tag)  # noqa: E731
    if args.command == "plan":
        action, tag, reason = plan(args.latest, local_tags(), check, time.time())
        print(f"{action}: {reason}", file=sys.stderr)
        print(f"action={action}")
        print(f"tag={tag}")
        print(f"from=v{args.latest}")
        print(f"reason={reason}")
        return
    if args.command == "pending":
        print(f"pending={pending(local_tags(), check)}")
        return
    parents = git("rev-list", "--parents", "-n", "1", "HEAD").split()[1:]
    parent_tags = git("tag", "--points-at", parents[0]).split() if len(parents) == 1 else []
    changed = git("diff", "--name-only", "HEAD^", "HEAD").split() if len(parents) == 1 else []
    allowed, reason = guard(args.version, parents, parent_tags, changed, check)
    print(f"autopilot {'allowed' if allowed else 'not allowed'}: {reason}", file=sys.stderr)
    print(f"autopilot={'true' if allowed else 'false'}")


if __name__ == "__main__":
    main()
