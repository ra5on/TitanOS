#!/usr/bin/env python3
"""Check the actual image's early microcode, firmware and NAS kernel modules."""
import argparse
from pathlib import Path

MICROCODE = ("kernel/x86/microcode/GenuineIntel.bin", "kernel/x86/microcode/AuthenticAMD.bin")
FIRMWARE = ("i915/*guc*.bin*", "i915/*huc*.bin*", "i915/*dmc*.bin*", "xe/*guc*.bin*", "intel/vpu/*.bin*", "amdgpu/*.bin*")
# The exact GuC/HuC files i915 of Linux 6.12 asks for on Gen9 to Tiger Lake,
# where Settings > Hardware can turn HuC loading on (intel_uc_fw.c).
FIRMWARE += tuple(
    f"i915/{prefix}_{kind}.bin*"
    for prefix, versions in (
        ("skl", ("guc_70.1.1", "huc_2.0.0")), ("bxt", ("guc_70.1.1", "huc_2.0.0")),
        ("kbl", ("guc_70.1.1", "huc_4.0.0")), ("cml", ("guc_70.1.1", "huc_4.0.0")),
        ("glk", ("guc_70.1.1", "huc_4.0.0")), ("icl", ("guc_70.1.1", "huc_9.0.0")),
        ("ehl", ("guc_70.1.1", "huc_9.0.0")), ("tgl", ("guc_70.1.1", "huc_7.9.3")),
    )
    for kind in versions
)
MODULES = (
    "i915", "xe", "intel_vpu", "amdgpu", "coretemp", "k10temp",
    "e1000e", "igb", "igc", "ixgbe", "i40e", "ice", "r8169", "tg3", "bnx2", "bnx2x",
    "qla2xxx", "mpt3sas", "megaraid_sas", "aacraid", "hpsa",
    "nvme", "nvme_core", "vmd", "ahci", "ata_piix", "sata_mv", "sata_sil24",
    "sd_mod", "scsi_mod", "usb_storage", "uas", "xhci_pci",
    "fuse", "exfat", "vfat", "ext4", "xfs", "btrfs", "dm_crypt",
    "virtio_pci", "virtio_blk", "virtio_net", "vfio_pci", "kvm_intel", "kvm_amd",
)
# One of each group is enough: Linux 7 replaced ntfs3 with a new driver
# named ntfs (kernel choice "Neu", docs/KERNEL.md).
ANY_OF = (("ntfs3", "ntfs"),)


def check_microcode(initrd):
    """Inspect leading uncompressed newc archives, never the compressed main root.

    Multiple early archives and their zero padding are legal. Requiring nonempty
    payloads here prevents a microcode file in the late root from passing.
    """
    found = set()
    with Path(initrd).open("rb") as stream:
        while stream.tell() < 256 * 1024 * 1024:
            first = stream.read(1)
            if not first:
                break
            if first == b"\0":
                continue
            header = first + stream.read(109)
            if header[:6] not in (b"070701", b"070702"):
                break
            if len(header) != 110:
                raise ValueError("truncated early initramfs header")
            try:
                size = int(header[54:62], 16)
                name_size = int(header[94:102], 16)
            except ValueError as error:
                raise ValueError("invalid early initramfs header") from error
            if not 1 <= name_size <= 4096 or size > 128 * 1024 * 1024:
                raise ValueError("invalid early initramfs entry size")
            name = stream.read(name_size)
            if len(name) != name_size or name[-1:] != b"\0":
                raise ValueError("truncated early initramfs name")
            stream.seek((-stream.tell()) % 4, 1)
            stream.seek(size, 1)
            if stream.tell() > Path(initrd).stat().st_size:
                raise ValueError("truncated early initramfs payload")
            stream.seek((-stream.tell()) % 4, 1)
            path = name[:-1].decode("ascii").removeprefix("./")
            if path in MICROCODE and size > 0:
                found.add(path)
    missing = set(MICROCODE) - found
    if missing:
        raise ValueError("missing early microcode: " + ", ".join(sorted(missing)))


def check_image(root, kernel):
    root = Path(root)
    firmware = root / "usr/lib/firmware"
    for pattern in FIRMWARE:
        if not any(path.is_file() and path.stat().st_size > 0 for path in firmware.glob(pattern)):
            raise ValueError("required GPU/NPU firmware missing: " + pattern)
    directory = root / "usr/lib/modules" / kernel
    names = {path.name.split(".ko")[0].replace("-", "_") for path in directory.rglob("*.ko*") if path.is_file()}
    builtin = directory / "modules.builtin"
    if builtin.exists():
        names.update(Path(line).name.removesuffix(".ko").replace("-", "_") for line in builtin.read_text().splitlines())
    missing = set(MODULES) - names
    missing.update("/".join(group) for group in ANY_OF if not names.intersection(group))
    if missing:
        raise ValueError("required NAS kernel modules missing: " + ", ".join(sorted(missing)))
    check_microcode(root / "boot" / ("initrd.img-" + kernel))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("kernel")
    args = parser.parse_args()
    try:
        check_image(args.root, args.kernel)
    except (ValueError, OSError) as error:
        raise SystemExit(str(error)) from error
    print("Hardware image checks passed: both early microcode vendors, GPU/NPU firmware and NAS kernel modules")


if __name__ == "__main__":
    main()
