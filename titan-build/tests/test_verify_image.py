"""Exercise actual dosfstools FAT layouts used by Rugix's EFI image builder."""

import importlib.util
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import unittest


HELPER = Path(__file__).resolve().parents[1] / "verify-image.py"
SPEC = importlib.util.spec_from_file_location("titan_image_verifier", HELPER)
VERIFIER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VERIFIER)
MKFS = shutil.which("mkfs.vfat") or shutil.which("mkfs.fat")
if MKFS is None:
    MKFS = next((str(path) for path in (Path("/usr/sbin/mkfs.vfat"), Path("/sbin/mkfs.vfat")) if path.is_file()), None)


@unittest.skipUnless(MKFS, "dosfstools is required for real FAT filesystem regression tests")
class EfiFilesystemTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="titan-efi-regression-")
        self.directory = Path(self.temporary.name)
        self.addCleanup(self.temporary.cleanup)

    def create_filesystem(self, fat_bits, loader=True):
        # Reproduce Bakery's unqualified mkfs.vfat on its 256 MiB ESP.
        # Explicit FAT32 on 128 MiB covers the other valid EFI layout.
        size = (256 if fat_bits == 16 else 128) * 1024**2
        image = self.directory / f"fat{fat_bits}.img"
        with image.open("wb") as stream:
            stream.truncate(size)
        options = [] if fat_bits == 16 else ["-F", "32"]
        subprocess.run([MKFS, *options, str(image)], check=True, capture_output=True)
        with image.open("r+b") as stream:
            boot = stream.read(512)
            sectors_per_cluster = boot[13]
            reserved = struct.unpack_from("<H", boot, 14)[0]
            fats = boot[16]
            root_entries = struct.unpack_from("<H", boot, 17)[0]
            fat_size_16 = struct.unpack_from("<H", boot, 22)[0]
            fat_size = fat_size_16 or struct.unpack_from("<I", boot, 36)[0]
            root_sector = reserved + fats * fat_size
            root_sectors = (root_entries * 32 + 511) // 512
            data_sector = root_sector + root_sectors
            root_cluster = struct.unpack_from("<I", boot, 44)[0] if fat_bits == 32 else None
            self.assertEqual(fat_size_16 == 0, fat_bits == 32)
            self.assertEqual(root_entries == 0, fat_bits == 32)
            first_directory_cluster = 3 if fat_bits == 32 else 2
            efi_cluster = first_directory_cluster
            boot_cluster = efi_cluster + 1
            file_cluster = efi_cluster + 2
            cluster_bytes = sectors_per_cluster * 512

            def cluster_offset(cluster):
                return (data_sector + (cluster - 2) * sectors_per_cluster) * 512

            def entry(name, cluster, is_directory, file_size=0):
                record = bytearray(32)
                record[:11] = name
                record[11] = 0x10 if is_directory else 0x20
                struct.pack_into("<H", record, 26, cluster & 0xFFFF)
                if fat_bits == 32:
                    struct.pack_into("<H", record, 20, cluster >> 16)
                struct.pack_into("<I", record, 28, file_size)
                return record

            if loader:
                root_offset = root_sector * 512 if fat_bits == 16 else cluster_offset(root_cluster)
                for offset, record in (
                    (root_offset, entry(b"EFI        ", efi_cluster, True)),
                    (cluster_offset(efi_cluster), entry(b"BOOT       ", boot_cluster, True)),
                    (cluster_offset(boot_cluster), entry(b"BOOTX64 EFI", file_cluster, False, 1024)),
                ):
                    stream.seek(offset)
                    stream.write(record + bytes(32))
                stream.seek(cluster_offset(file_cluster))
                stream.write(b"MZ" + bytes(1022))
                used_clusters = [efi_cluster, boot_cluster, file_cluster]
                if root_cluster is not None:
                    used_clusters.append(root_cluster)
                for fat_index in range(fats):
                    for cluster in used_clusters:
                        stream.seek((reserved + fat_index * fat_size) * 512 + cluster * (fat_bits // 8))
                        stream.write(struct.pack("<I" if fat_bits == 32 else "<H", 0x0FFFFFFF if fat_bits == 32 else 0xFFFF))
        return image, {"firstLba": 0, "sizeBytes": size}, cluster_offset(file_cluster)

    def test_bakery_default_256_mib_fat16_contains_uefi_loader(self):
        image, partition, _ = self.create_filesystem(16)
        with image.open("rb") as stream:
            result = VERIFIER.verify_efi_loader(stream, partition)
        self.assertEqual(result, {"path": "EFI/BOOT/BOOTX64.EFI", "sizeBytes": 1024, "filesystem": "FAT16"})

    def test_explicit_fat32_contains_uefi_loader(self):
        image, partition, _ = self.create_filesystem(32)
        with image.open("rb") as stream:
            result = VERIFIER.verify_efi_loader(stream, partition)
        self.assertEqual(result["filesystem"], "FAT32")
        self.assertEqual(result["path"], "EFI/BOOT/BOOTX64.EFI")

    def test_missing_efi_directory_is_rejected_for_both_fat_types(self):
        for fat_bits in (16, 32):
            with self.subTest(fat_bits=fat_bits):
                image, partition, _ = self.create_filesystem(fat_bits, loader=False)
                with image.open("rb") as stream, self.assertRaisesRegex(ValueError, "path component missing: EFI"):
                    VERIFIER.verify_efi_loader(stream, partition)

    def test_non_executable_loader_is_rejected_for_both_fat_types(self):
        for fat_bits in (16, 32):
            with self.subTest(fat_bits=fat_bits):
                image, partition, file_offset = self.create_filesystem(fat_bits)
                with image.open("r+b") as stream:
                    stream.seek(file_offset)
                    stream.write(b"NO")
                with image.open("rb") as stream, self.assertRaisesRegex(ValueError, "not a PE executable"):
                    VERIFIER.verify_efi_loader(stream, partition)

    def test_filesystem_outside_partition_is_rejected(self):
        for fat_bits in (16, 32):
            with self.subTest(fat_bits=fat_bits):
                image, partition, _ = self.create_filesystem(fat_bits)
                with image.open("r+b") as stream:
                    stream.seek(19)
                    stream.write(bytes(2))
                    stream.seek(32)
                    stream.write(struct.pack("<I", partition["sizeBytes"] // 512 + 1))
                with image.open("rb") as stream, self.assertRaisesRegex(ValueError, "outside its partition"):
                    VERIFIER.verify_efi_loader(stream, partition)

    def test_mismatched_bpb_geometry_is_rejected(self):
        image, partition, _ = self.create_filesystem(32)
        with image.open("r+b") as stream:
            stream.seek(17)
            stream.write(struct.pack("<H", 512))
        with image.open("rb") as stream, self.assertRaisesRegex(ValueError, "type and BPB geometry disagree"):
            VERIFIER.verify_efi_loader(stream, partition)


class InstalledReleaseTests(unittest.TestCase):
    def test_exact_titan_release_is_accepted(self):
        release = {"version": "2.0.0-titan.1", "name": "TitanOS 2.0"}
        self.assertEqual(VERIFIER.check_system_version({"result": {"data": release}}, *release.values()), release)

    def test_wrong_version_or_upstream_branding_is_rejected(self):
        for version, name in (("2.0.0", "TitanOS 2.0"), ("2.0.0-titan.1", "umbrelOS 2.0")):
            with self.subTest(version=version, name=name), self.assertRaises(VERIFIER.BrandingMismatch):
                VERIFIER.check_system_version({"result": {"data": {"version": version, "name": name}}}, "2.0.0-titan.1", "TitanOS 2.0")


class FirstBootCapacityTests(unittest.TestCase):
    def test_download_capacity_covers_both_system_slots_and_data(self):
        VERIFIER.verify_first_boot_capacity(32 * 1024**3, {"firstLba": 2048 * 1257, "sizeBytes": 7 * 1024**3})

    def test_compact_template_is_accepted_only_with_sufficient_target_disk(self):
        partition = {"firstLba": 2048 * 1257, "sizeBytes": 6 * 1024**3}
        VERIFIER.verify_first_boot_capacity(8 * 1024**3, partition, 32 * 1024**3)
        with self.assertRaisesRegex(ValueError, "at least 32 GiB"):
            VERIFIER.verify_first_boot_capacity(8 * 1024**3, partition, 8 * 1024**3)

    def test_enlarged_system_filesystem_and_missing_data_capacity_are_rejected(self):
        with self.assertRaisesRegex(ValueError, "exceeds the 10 GiB"):
            VERIFIER.verify_first_boot_capacity(32 * 1024**3, {"firstLba": 2048 * 1257, "sizeBytes": 11 * 1024**3})
        with self.assertRaisesRegex(ValueError, "too little space"):
            VERIFIER.verify_first_boot_capacity(32 * 1024**3, {"firstLba": 30 * 1024**3 // 512, "sizeBytes": 1024**3})


if __name__ == "__main__":
    unittest.main()
