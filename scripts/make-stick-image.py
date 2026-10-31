#!/usr/bin/env python3
"""Packs a stick folder (out/stick) into a disk image for writing with
balenaEtcher, Rufus or dd: an MBR partition table with one FAT32 partition
labelled TITANOS at 1 MiB, and GRUB in the MBR for legacy BIOS boards.
UEFI boards start it like a stick made from the ZIP.

Usage: make-stick-image.py <stick folder> <image> [size in MiB, default 4096]

Needs sfdisk (util-linux) and mtools; no root.
"""
import importlib.util
import os
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main(argv):
    if len(argv) not in (3, 4):
        print(__doc__, file=sys.stderr)
        return 2
    stick, image = Path(argv[1]), Path(argv[2])
    size = int(argv[3]) if len(argv) == 4 else 4096
    if not (stick / "boot/grub/i386-pc/core.img").exists():
        raise SystemExit(f"{stick} has no GRUB for legacy BIOS")
    if any((stick / "config").glob("*")):
        raise SystemExit("refusing to pack a stick with user configuration")
    os.environ["PATH"] += ":/usr/sbin:/sbin"
    image.unlink(missing_ok=True)
    with image.open("wb") as f:
        f.truncate(size << 20)
    subprocess.run(["sfdisk", "--quiet", "--label", "dos", str(image)],
                   input=b"start=1MiB, type=c, bootable\n", check=True)
    partition = f"{image}@@1M"
    subprocess.run(["mformat", "-i", partition, "-F", "-v", "TITANOS", "::"], check=True)
    subprocess.run(["mcopy", "-i", partition, "-s", "-Q", *map(str, sorted(stick.iterdir())), "::"], check=True)
    load("stick_bios", "stick-bios.py").install(str(image), stick / "boot/grub")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
