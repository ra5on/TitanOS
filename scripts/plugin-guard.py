#!/usr/bin/env python3
"""May a common plugin set be signed without the owner's approval?

  plugin-guard.py common <draft dir> <published dir> [--keys <dir>]

<draft dir> holds index.json of a draft common-<n> of ra5on/TitanOS-Plugins,
<published dir> index.json and index.json.minisig of the common set the
catalog names now. The published index must carry a signature of a key of
the image. Exits 0 when the draft differs from it only in newer upstream
versions of Tailscale or NetBird (scripts/upstream.py there, nightly), 1
otherwise: then the owner approves as always. NVIDIA is never in a common
set; kernel sets never pass here.

Run by .github/workflows/plugin-sign.yml with AUTOPILOT_PLUGINS=on, before
the key is near and again next to it.
"""
import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
KEYS = REPO / "build/mkosi.extra/usr/share/titanos/update-keys"
ALLOWED = {"tailscale", "netbird"}
COMMON = re.compile(r"common-([1-9][0-9]*)")
UPSTREAM = re.compile(r"([0-9]+)\.([0-9]+)\.([0-9]+)-[1-9][0-9]*")
SAME = ("debian", "sysextLevel", "kernel", "kernelPackage")
ENTRY = ("version", "manifest", "sha256", "size", "inputs")


class Refused(Exception):
    pass


def trusted(index, signature, keys):
    """The published index carries a signature of a key of the image."""
    for key in sorted(keys.glob("*.pub")):
        if key.name == "rehearsal.pub":
            continue
        if subprocess.run(["minisign", "-V", "-q", "-p", str(key), "-m", str(index), "-x", str(signature)],
                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
            return True
    return False


def upstream(version):
    match = UPSTREAM.fullmatch(version)
    if not match:
        raise Refused(f"odd plugin version {version!r}")
    return tuple(int(part) for part in match.groups())


def check(draft, published):
    """Returns what changed, or raises Refused."""
    new_tag, old_tag = COMMON.fullmatch(draft.get("tag", "")), COMMON.fullmatch(published.get("tag", ""))
    if not new_tag or not old_tag or int(new_tag[1]) <= int(old_tag[1]):
        raise Refused(f"not a later common set: {draft.get('tag')!r} after {published.get('tag')!r}")
    for field in SAME:
        if draft.get(field) != published.get(field):
            raise Refused(f"{field} changed")
    new = {p["name"]: p for p in draft.get("plugins", [])}
    old = {p["name"]: p for p in published.get("plugins", [])}
    if len(new) != len(draft.get("plugins", [])) or new.keys() != old.keys():
        raise Refused(f"other plugins: {sorted(new)} instead of {sorted(old)}")
    changed = []
    for name in sorted(new):
        if all(new[name].get(f) == old[name].get(f) for f in ENTRY):
            continue
        if name not in ALLOWED:
            raise Refused(f"{name} changed")
        if upstream(new[name]["version"]) <= upstream(old[name]["version"]):
            raise Refused(f"{name} changed without a newer upstream version")
        changed.append(f"{name} {old[name]['version']} -> {new[name]['version']}")
    if not changed:
        raise Refused("nothing changed")
    return changed


def main(argv):
    parser = argparse.ArgumentParser()
    parser.add_argument("kind", choices=["common"])
    parser.add_argument("draft", type=Path)
    parser.add_argument("published", type=Path)
    parser.add_argument("--keys", type=Path, default=KEYS)
    args = parser.parse_args(argv)
    try:
        if not trusted(args.published / "index.json", args.published / "index.json.minisig", args.keys):
            raise Refused("the published set is not signed by a key of the image")
        draft = json.loads((args.draft / "index.json").read_text())
        published = json.loads((args.published / "index.json").read_text())
        changed = check(draft, published)
    except (Refused, OSError, ValueError, KeyError, TypeError) as error:
        print(f"approval needed: {error}")
        return 1
    print(f"{draft['tag']}: only upstream versions: " + ", ".join(changed))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
