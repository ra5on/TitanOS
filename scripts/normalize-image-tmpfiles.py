#!/usr/bin/env python3
"""Remove duplicate creation rules for /run/lock and /run/utmp in an image.

Preserve tmpfiles' basename ordering and directory precedence, including the
first rule's Debian permissions. Do not change other paths or operation types.
"""
import argparse
from pathlib import Path

PATHS = {"/run/lock", "/run/utmp"}


def normalize(root):
    root = Path(root)
    effective = {}
    for directory in ("usr/lib/tmpfiles.d", "usr/local/lib/tmpfiles.d", "run/tmpfiles.d", "etc/tmpfiles.d"):
        for path in sorted((root / directory).glob("*.conf")):
            effective[path.name] = path
    seen = {}
    removed = 0
    for name, path in sorted(effective.items()):
        # Masking symlinks and vendor symlinks must never be followed on the host.
        if path.is_symlink() or not path.is_file():
            continue
        original = path.read_text()
        lines = []
        for line in original.splitlines(keepends=True):
            fields = line.split()
            if len(fields) >= 2 and fields[0] in ("d", "f", "F") and fields[1] in PATHS:
                key = (fields[0], fields[1])
                if key in seen:
                    lines.append(f"# TitanOS: {fields[1]} already provided by {seen[key]}.\n")
                    removed += 1
                    continue
                seen[key] = name
            lines.append(line)
        updated = "".join(lines)
        if updated != original:
            path.write_text(updated)
    return removed


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    args = parser.parse_args()
    print(f"Removed {normalize(args.root)} duplicate tmpfiles creation rules")
