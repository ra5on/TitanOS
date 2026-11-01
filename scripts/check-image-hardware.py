#!/usr/bin/env python3
"""Check the actual image's early microcode, firmware and NAS kernel modules."""
import argparse
import fnmatch
import gzip
import lzma
import re
import shutil
import subprocess
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

# Every firmware file a module of these directories asks for must be in the
# image or named in EXPECTED_MISSING with its reason: network, storage,
# graphics, accelerators and USB host controllers decide whether a NAS
# works. Other directories (WLAN, Bluetooth, TV cards, sound, serial
# adapters) are only counted in the report.
GATED = (
    "kernel/drivers/net/ethernet/", "kernel/drivers/scsi/", "kernel/drivers/ata/",
    "kernel/drivers/nvme/", "kernel/drivers/md/", "kernel/drivers/gpu/drm/",
    "kernel/drivers/accel/", "kernel/drivers/usb/host/", "kernel/drivers/usb/storage/",
    "kernel/drivers/thunderbolt/", "kernel/drivers/misc/mei/", "kernel/net/wireless/",
)
EXPECTED_MISSING = {
    # amdgpu
    "amdgpu/*ip_discovery.bin": "optional, the GPU carries the table itself",
    "amdgpu/*_gpu_info.bin": "optional, only for chips without an IP discovery table",
    "amdgpu/*_cap.bin": "only for SR-IOV virtual functions in a hypervisor",
    "amdgpu/*_kicker.bin": "single RX 7900 revisions; newer linux-firmware (kernel choice Neu)",
    "amdgpu/gc_11_0_0_toc.bin": "single RX 7900 revisions; newer linux-firmware (kernel choice Neu)",
    "amdgpu/gc_11_0_3_mes.bin": "old name, gc_11_0_3_mes_2.bin replaces it",
    # not in linux-firmware at all (license) or for hardware from the 1990s
    "ast_dp501_fw.bin": "ASPEED DP501 bridge of a few old server boards; not in linux-firmware",
    "renesas_usb_fw.mem": "only Renesas uPD720201/2 cards without ROM; not redistributable",
    "aic94xx-seq.fw": "Adaptec AIC-94xx SAS (2005); not in linux-firmware",
    "wd719x-*.bin": "Western Digital WD7193/7197/7296 SCSI (1997); not in linux-firmware",
    "acenic/*": "Alteon AceNIC gigabit card (1998)",
    "adaptec/starfire_*.bin": "Adaptec Starfire network card (1999)",
    "sun/cassini.bin": "Sun Cassini network card (2003)",
    "ositech/*": "Ositech PCMCIA network card (1990s)",
    "netronome/nic_AMDA00*.nffw": "Agilio SmartNIC profiles beyond the ones in firmware-netronome",
    # accelerators no NAS carries yet
    "habanalabs/*": "Intel Gaudi data-center accelerators",
    "intel/vpu/vpu_50xx*": "NPU of Lunar Lake and later; newer linux-firmware (kernel choice Neu)",
}
# What every NAS asks for at boot.
REQUIRED_FILES = ("regulatory.db",)


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
    initrd = root / "boot" / ("initrd.img-" + kernel)
    check_microcode(initrd)
    check_firmware(root, kernel)
    check_initrd_without_graphics(initrd)


def in_image(root, path):
    """Whether path exists in the image at root. Links are followed inside
    the image: an absolute target (regulatory.db -> /etc/alternatives/...)
    means the image's file, never the build machine's."""
    root = Path(root)
    for _ in range(20):
        if path.is_symlink():
            target = Path(path.readlink())
            path = root / target.relative_to("/") if target.is_absolute() else path.parent / target
            continue
        return path.exists()
    return False


def firmware_present(firmware, name, root=None):
    root = root or firmware.parents[2]  # <root>/usr/lib/firmware
    if any(char in name for char in "*?["):
        return any(in_image(root, path) for path in firmware.glob(name))
    return any(in_image(root, firmware / (name + suffix)) for suffix in ("", ".zst", ".xz"))


def blacklisted(root):
    names = set()
    for directory in ("etc/modprobe.d", "usr/lib/modprobe.d", "lib/modprobe.d"):
        for conf in sorted((root / directory).glob("*.conf")):
            for line in conf.read_text(errors="replace").splitlines():
                fields = line.split()
                if len(fields) == 2 and fields[0] == "blacklist":
                    names.add(fields[1].replace("-", "_"))
    return names


def decompress(data):
    """The content of a module or initramfs: plain, gzip, xz or zstd."""
    if data[:2] == b"\x1f\x8b":
        return gzip.decompress(data)
    if data[:6] == b"\xfd7zXZ\0":
        return lzma.decompress(data)
    if data[:4] == b"\x28\xb5\x2f\xfd":
        try:
            from compression import zstd  # Python 3.14
            return zstd.decompress(data)
        except ImportError:
            tool = shutil.which("zstd")
            if not tool:
                raise ValueError("zstd is needed to read zstd-compressed files")
            return subprocess.run([tool, "-dcq"], input=data, capture_output=True, check=True).stdout
    return data


def module_firmware(path):
    """The firmware files a module names (modinfo -F firmware)."""
    data = decompress(path.read_bytes())
    return [match.decode() for match in re.findall(rb"(?<=\0)firmware=([^\0]+)", data)]


def check_firmware(root, kernel):
    """Fails on firmware a NAS-relevant module asks for that is neither in
    the image nor an expected gap; prints how many other modules miss some."""
    firmware = root / "usr/lib/firmware"
    for name in REQUIRED_FILES:
        if not firmware_present(firmware, name):
            raise ValueError("required firmware missing: " + name)
    directory = root / "usr/lib/modules" / kernel
    blocked = blacklisted(root)
    problems, others = [], 0
    for path in sorted(directory.rglob("*.ko*")):
        relative = path.relative_to(directory).as_posix()
        # A blacklisted module never asks for its firmware (nouveau: NVIDIA
        # cards get the NVIDIA plugin).
        if path.name.split(".ko")[0].replace("-", "_") in blocked:
            continue
        missing = [name for name in module_firmware(path) if not firmware_present(firmware, name)]
        if not missing:
            continue
        if not relative.startswith(GATED):
            others += 1
            continue
        unexplained = [name for name in missing if not any(fnmatch.fnmatch(name, pattern) for pattern in EXPECTED_MISSING)]
        if unexplained:
            files = sorted(set(unexplained))
            more = f" and {len(files) - 5} more" if len(files) > 5 else ""
            problems.append(f"{path.name.split('.ko')[0]}: {', '.join(files[:5])}{more}")
    if problems:
        raise ValueError("firmware missing for network, storage or graphics drivers (add it or explain it in EXPECTED_MISSING): " + "; ".join(problems))
    print(f"Firmware: all NAS drivers complete; {others} other modules (WLAN, Bluetooth, TV, sound, serial) miss some files")


def initrd_names(initrd):
    """The names in all archives of the initramfs: the uncompressed early
    ones and the compressed main one after them."""
    data = Path(initrd).read_bytes()
    names = newc_names(data)
    # The main archive starts where the early ones (and their padding) end.
    offset = 0
    while offset + 110 <= len(data):
        if data[offset:offset + 1] == b"\0":
            offset += 1
            continue
        if data[offset:offset + 6] not in (b"070701", b"070702"):
            break
        size = int(data[offset + 54:offset + 62], 16)
        name_size = int(data[offset + 94:offset + 102], 16)
        offset += 110 + name_size
        offset += -offset % 4
        offset += size
        offset += -offset % 4
    if offset < len(data):
        names += newc_names(decompress(data[offset:]))
    return names


def newc_names(data):
    """Names of all newc archives in data; several may follow each other,
    separated by zero padding."""
    names, offset = [], 0
    while offset + 110 <= len(data):
        if data[offset:offset + 1] == b"\0":
            offset += 1
            continue
        if data[offset:offset + 6] not in (b"070701", b"070702"):
            break
        size = int(data[offset + 54:offset + 62], 16)
        name_size = int(data[offset + 94:offset + 102], 16)
        name = data[offset + 110:offset + 110 + name_size - 1].decode(errors="replace")
        if name != "TRAILER!!!":
            names.append(name)
        offset += 110 + name_size
        offset += -offset % 4
        offset += size
        offset += -offset % 4
    return names


# Graphics drivers stay out of the initramfs (mkosi.postinst.chroot removes
# live-boot's kms hook): loaded there, i915 waits for MEI modules that only
# come with the system and gives up.
GRAPHICS_IN_INITRD = re.compile(r"/drivers/gpu/drm/(i915|xe|amd/amdgpu|radeon|nouveau)/[^/]+\.ko")


def check_initrd_without_graphics(initrd):
    found = [name for name in initrd_names(initrd) if GRAPHICS_IN_INITRD.search(name)]
    if found:
        raise ValueError("graphics drivers in the initramfs: " + ", ".join(found))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("kernel")
    args = parser.parse_args()
    try:
        check_image(args.root, args.kernel)
    except (ValueError, OSError) as error:
        raise SystemExit(str(error)) from error
    print("Hardware image checks passed: both early microcode vendors, GPU/NPU firmware, firmware of NAS drivers, regulatory.db, NAS kernel modules and an initramfs without graphics drivers")


if __name__ == "__main__":
    main()
