#!/usr/bin/env python3
"""Make a TitanOS stick start on boards with a legacy BIOS (no UEFI).

Writes GRUB's boot code into the MBR (the partition table stays) and GRUB
itself into the free sectors between the MBR and the first partition. The
FAT32 partition with the stick contents is not touched; the menu and the A/B
rules come from /boot/grub/grub.cfg on it.

Usage (Linux, as root, with the stick NOT mounted):
    python3 stick-bios.py /dev/sdX [boot directory]
    python3 stick-bios.py stick.img [boot directory]

The boot directory holds i386-pc/boot.img and i386-pc/core.img; by default
the boot/grub folder of the stick (next to this script) or out/stick.

Only MBR (DOS) partition tables whose first partition starts at 1 MiB or
later are supported; GPT and sticks without a partition table are refused.
"""
import os
import struct
import sys
from pathlib import Path

SECTOR = 512
# Offsets in GRUB's boot.img (grub-core/boot/i386/pc/boot.S).
KERNEL_SECTOR = 0x5C
DRIVE_CHECK = 0x66
CODE_END = 0x1B8  # disk signature and partition table follow
# Offsets in the first sector of core.img (diskboot.img): the block list.
BLOCKLIST_START = 0x1F4


class Refused(Exception):
    pass


def partitions(mbr):
    """The used primary partitions as (index, status, type, first LBA, sectors)."""
    found = []
    for i in range(4):
        entry = mbr[0x1BE + 16 * i:0x1CE + 16 * i]
        status, kind = entry[0], entry[4]
        first, count = struct.unpack_from("<II", entry, 8)
        if kind != 0:
            found.append((i, status, kind, first, count))
    return found


def plan(mbr, gap, boot, core):
    """Checks the disk and returns the new MBR; raises Refused if unsafe."""
    if len(mbr) != SECTOR or mbr[510:512] != b"\x55\xaa":
        raise Refused("no MBR partition table (sticks without one are not supported)")
    if mbr[0x0B:0x0D] == b"\x00\x02" and mbr[0x52:0x57] == b"FAT32":
        raise Refused("the stick has no partition table, only a FAT file system")
    parts = partitions(mbr)
    if not parts:
        raise Refused("the partition table is empty")
    if any(kind == 0xEE for _, _, kind, _, _ in parts):
        raise Refused("GPT partition table: use an MBR (DOS) partition table")
    first = min(start for _, _, _, start, _ in parts)
    sectors = (len(core) + SECTOR - 1) // SECTOR
    if first < 1 + sectors:
        raise Refused(f"the first partition starts at sector {first}; GRUB needs {1 + sectors} sectors before it")
    used = gap[: sectors * SECTOR]
    if any(used) and not used.startswith(core[:BLOCKLIST_START]):
        raise Refused("the sectors before the first partition are in use by something else")
    if len(boot) != SECTOR or len(core) < SECTOR:
        raise Refused("boot.img or core.img is damaged")
    new = bytearray(mbr)
    new[:CODE_END] = boot[:CODE_END]
    # core.img lies right behind the MBR (the default of boot.img and of
    # its block list); a hard disk check as grub-bios-setup does it for
    # BIOSes that hand over a wrong drive number.
    if struct.unpack_from("<Q", new, KERNEL_SECTOR)[0] != 1:
        raise Refused("unexpected boot.img")
    new[DRIVE_CHECK:DRIVE_CHECK + 2] = b"\x90\x90"
    # Some BIOSes only start a USB stick with an active partition.
    if not any(status == 0x80 for _, status, _, _, _ in parts):
        index = min(parts, key=lambda p: p[3])[0]
        new[0x1BE + 16 * index] = 0x80
    if struct.unpack_from("<Q", core, BLOCKLIST_START)[0] != 2:
        raise Refused("unexpected core.img")
    return bytes(new)


def install(target, directory):
    boot = (directory / "i386-pc/boot.img").read_bytes()
    core = (directory / "i386-pc/core.img").read_bytes()
    with open(target, "r+b") as disk:
        head = disk.read(SECTOR + len(core) + SECTOR)
        mbr = head[:SECTOR]
        new = plan(mbr, head[SECTOR:], boot, core)
        disk.seek(SECTOR)
        disk.write(core)
        disk.seek(0)
        disk.write(new)
        disk.flush()
        os.fsync(disk.fileno())


def main(argv):
    if len(argv) not in (2, 3):
        print(__doc__, file=sys.stderr)
        return 2
    here = Path(__file__).resolve().parent
    if len(argv) == 3:
        directory = Path(argv[2])
    elif (here / "i386-pc/core.img").exists():
        directory = here
    else:
        directory = here.parent / "out/stick/boot/grub"
    target = argv[1]
    if target.startswith("/dev/"):
        with open("/proc/mounts") as mounts:
            if any(line.split()[0].startswith(target) for line in mounts):
                print(f"{target} is mounted; unmount it first", file=sys.stderr)
                return 1
    try:
        install(target, directory)
    except Refused as error:
        print(f"not changed: {error}", file=sys.stderr)
        return 1
    print(f"{target} now also starts with a legacy BIOS")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
