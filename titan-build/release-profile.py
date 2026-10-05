#!/usr/bin/env python3
"""Select truthful canonical or one-time stable bridge build metadata."""
import argparse
import copy
import json
from pathlib import Path
import re
import subprocess

BRIDGE_VERSION = "2.0.0-titan.3"


def metadata_for(canonical, profile):
    if (not re.fullmatch(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", canonical.get("version", ""))
            or canonical.get("osVersion") != canonical["version"] or canonical.get("stage") != "stable"):
        raise ValueError("The committed release identity must be a canonical stable Titan version")
    result = copy.deepcopy(canonical)
    if profile == "legacy-bridge":
        if canonical["version"] != "2.0.0":
            raise ValueError("The legacy bridge belongs only to the initial 2.0.0 stable transition")
        result.update(version=BRIDGE_VERSION, osVersion=BRIDGE_VERSION, legacyUpdateBridgeTo="2.0.0")
    elif profile != "canonical":
        raise ValueError("Unsupported Titan release profile")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("profile", choices=("canonical", "legacy-bridge"))
    args = parser.parse_args()
    # Always restore from the committed identity, not the previous build's
    # working-tree metadata. Never commit a temporary bridge identity to main.
    canonical = json.loads(subprocess.check_output(["git", "-C", str(args.root), "show", "HEAD:.titan/release.json"], text=True))
    result = metadata_for(canonical, args.profile)
    (args.root / ".titan/release.json").write_text(json.dumps(result, indent=2) + "\n")
    print(f"Selected {args.profile}: TitanOS {result['osVersion']} ({result['stage']})")


if __name__ == "__main__":
    main()
