#!/usr/bin/env python3
"""Writes a GRUB environment block: exactly 1024 bytes (or a multiple), the
form GRUB's load_env reads and its save_env overwrites in place.

Usage: grubenv.py <file> [name=value ...]
"""
import sys

HEADER = "# GRUB Environment Block\n"
BLOCK = 1024


def block(values):
    body = HEADER
    for name, value in values.items():
        if not name.replace("_", "").isalnum() or any(c in value for c in "\n\\"):
            raise ValueError(f"cannot store {name!r} in a GRUB environment block")
        body += f"{name}={value}\n"
    data = body.encode()
    size = max(BLOCK, -(-len(data) // BLOCK) * BLOCK)
    return data + b"#" * (size - len(data))


def main(argv):
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    values = dict(arg.split("=", 1) for arg in argv[2:])
    with open(argv[1], "wb") as f:
        f.write(block(values))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
