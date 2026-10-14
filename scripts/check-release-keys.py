#!/usr/bin/env python3
"""Require distinct, structurally valid release and recovery Minisign public keys."""
import base64
import binascii
import sys
from pathlib import Path


def check(directory):
    keys = set()
    for path in directory.glob("*.pub"):
        if path.name == "rehearsal.pub":
            raise ValueError("a disposable rehearsal key must never be published")
        lines = path.read_text().splitlines()
        try:
            if len(lines) != 2 or not lines[0].startswith("untrusted comment: "):
                raise ValueError()
            decoded = base64.b64decode(lines[1], validate=True)
            if len(decoded) != 42 or decoded[:2] != b"Ed":
                raise ValueError()
        except (ValueError, binascii.Error) as error:
            raise ValueError(f"invalid Minisign public key: {path.name}") from error
        keys.add(decoded[10:])  # actual Ed25519 public key, not just filename or key ID
    if len(keys) < 2:
        raise ValueError("a release needs distinct release and offline recovery public keys")


if __name__ == "__main__":
    try:
        check(Path(sys.argv[1]))
    except (IndexError, ValueError, OSError) as error:
        sys.exit(str(error) or "usage: check-release-keys.py <public keys directory>")
