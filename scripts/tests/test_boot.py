"""GRUB for legacy BIOS and 32-bit UEFI: MBR installation and the stick image."""
import importlib.util
import os
import shutil
import struct
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, REPO / "scripts" / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


bios = load("stick_bios", "stick-bios.py")
grubenv = load("grubenv", "grubenv.py")


def fake_grub(directory):
    """boot.img and core.img with the fields GRUB's own images have."""
    boot = bytearray(b"\xeb\x63\x90" + b"\x11" * 509)
    struct.pack_into("<Q", boot, bios.KERNEL_SECTOR, 1)
    boot[bios.DRIVE_CHECK:bios.DRIVE_CHECK + 2] = b"\xeb\x05"
    core = bytearray(b"\x22" * (3 * 512))
    struct.pack_into("<Q", core, bios.BLOCKLIST_START, 2)
    (directory / "i386-pc").mkdir(parents=True)
    (directory / "i386-pc/boot.img").write_bytes(bytes(boot))
    (directory / "i386-pc/core.img").write_bytes(bytes(core))
    return bytes(boot), bytes(core)


def disk(path, start=2048, kind=0x0C, size=4 << 20):
    mbr = bytearray(512)
    mbr[0x1B8:0x1BC] = b"\xaa\xbb\xcc\xdd"
    entry = bytearray(16)
    entry[4] = kind
    struct.pack_into("<II", entry, 8, start, size // 512 - start)
    mbr[0x1BE:0x1CE] = entry
    mbr[510:512] = b"\x55\xaa"
    with open(path, "wb") as f:
        f.write(mbr)
        f.truncate(size)


class StickBiosTest(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp)
        self.boot, self.core = fake_grub(self.tmp / "grub")

    def test_installs_behind_the_mbr_and_keeps_the_partition_table(self):
        image = self.tmp / "stick.img"
        disk(image)
        before = image.read_bytes()[:512]
        bios.install(str(image), self.tmp / "grub")
        data = image.read_bytes()
        self.assertEqual(data[:0x66], self.boot[:0x66])
        self.assertEqual(data[0x66:0x68], b"\x90\x90")
        self.assertEqual(data[0x68:0x1B8], self.boot[0x68:0x1B8])
        self.assertEqual(data[0x1B8:0x1BE], before[0x1B8:0x1BE])  # disk signature
        self.assertEqual(data[0x1BE], 0x80)  # made active
        self.assertEqual(data[0x1BF:512], before[0x1BF:512])
        self.assertEqual(data[512:512 + len(self.core)], self.core)
        # Installing again (a newer stick image or a repeat) is fine.
        bios.install(str(image), self.tmp / "grub")

    def test_refuses_unsafe_disks(self):
        cases = {
            "gpt": dict(kind=0xEE, start=1),
            "too close": dict(start=2),
        }
        for name, args in cases.items():
            image = self.tmp / f"{name}.img"
            disk(image, **args)
            before = image.read_bytes()
            with self.assertRaises(bios.Refused, msg=name):
                bios.install(str(image), self.tmp / "grub")
            self.assertEqual(image.read_bytes(), before, name)
        used = self.tmp / "used.img"
        disk(used)
        with open(used, "r+b") as f:
            f.seek(1024)
            f.write(b"something else")
        with self.assertRaises(bios.Refused):
            bios.install(str(used), self.tmp / "grub")
        floppy = self.tmp / "floppy.img"
        vbr = bytearray(512)
        vbr[0x0B:0x0D] = b"\x00\x02"
        vbr[0x52:0x57] = b"FAT32"
        vbr[510:512] = b"\x55\xaa"
        floppy.write_bytes(bytes(vbr) + bytes(4 << 20))
        with self.assertRaises(bios.Refused):
            bios.install(str(floppy), self.tmp / "grub")


class GrubEnvTest(unittest.TestCase):
    def test_block(self):
        data = grubenv.block({"titanos_prefer": "a"})
        self.assertEqual(len(data), 1024)
        self.assertTrue(data.startswith(b"# GRUB Environment Block\ntitanos_prefer=a\n#"))
        with self.assertRaises(ValueError):
            grubenv.block({"x": "a\nb"})


class GrubConfigTest(unittest.TestCase):
    def test_template_matches_the_updater(self):
        text = (REPO / "boot/grub.cfg.in").read_text()
        update = (REPO / "backend/internal/update/update.go").read_text()
        # Three tries, the names systemd-boot gives them on the way.
        self.assertIn("tries = 3", update)
        for name in ["+3.conf", "+2-1.conf", "+1-2.conf"]:
            self.assertIn(f'"/loader/entries/titanos-$1{name}"', text)
        self.assertIn("@OPTIONS@", text)
        self.assertIn("save_env --file /boot/grub/grubenv titanos_trial titanos_count titanos_prefer", text)
        entry = (REPO / "boot/entry.conf.in").read_text()
        self.assertIn("live-media-path=/titanos/@SLOT@", entry)


@unittest.skipUnless(shutil.which("mformat") and (shutil.which("sfdisk") or Path("/usr/sbin/sfdisk").exists()), "mtools/sfdisk missing")
class StickImageTest(unittest.TestCase):
    def test_image_holds_the_stick_and_starts_with_a_bios(self):
        tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, tmp)
        stick = tmp / "stick"
        (stick / "titanos/a").mkdir(parents=True)
        (stick / "titanos/a/version").write_text("1.0.0\n")
        (stick / "config").mkdir()
        _, core = fake_grub(stick / "boot/grub")
        image = tmp / "stick.img"
        subprocess.run(["python3", str(REPO / "scripts/make-stick-image.py"), str(stick), str(image), "64"], check=True)
        data = image.read_bytes()
        self.assertEqual(data[512:512 + len(core)], core)
        self.assertEqual(data[0x1BE], 0x80)
        listing = subprocess.run(["mtype", "-i", f"{image}@@1M", "::/titanos/a/version"], capture_output=True, text=True, check=True)
        self.assertEqual(listing.stdout, "1.0.0\n")
        label = subprocess.run(["mlabel", "-s", "-i", f"{image}@@1M", "::"], capture_output=True, text=True, check=True)
        self.assertIn("TITANOS", label.stdout)
        (stick / "config/accounts.json").write_text("{}")
        with self.assertRaises(subprocess.CalledProcessError):
            subprocess.run(["python3", str(REPO / "scripts/make-stick-image.py"), str(stick), str(image), "64"], check=True, capture_output=True)


if __name__ == "__main__":
    unittest.main()
