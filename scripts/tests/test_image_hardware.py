"""The release gate inspects image contents, not the builder's hardware."""
import gzip
import importlib.util
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("image_hardware", ROOT / "scripts/check-image-hardware.py")
hardware = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hardware)


def archive(entries):
    output = bytearray()
    for name, content in [*entries, ("TRAILER!!!", b"")]:
        encoded = name.encode() + b"\0"
        fields = [0, 0o100644, 0, 0, 1, 0, len(content), 0, 0, 0, 0, len(encoded), 0]
        output += b"070701" + b"".join(f"{field:08x}".encode() for field in fields)
        output += encoded
        output += b"\0" * (-len(output) % 4)
        output += content
        output += b"\0" * (-len(output) % 4)
    output += b"\0" * (-len(output) % 512)
    return bytes(output)


class ImageHardwareTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.kernel = "test-kernel"
        self.initrd = self.root / "boot" / ("initrd.img-" + self.kernel)
        self.initrd.parent.mkdir()
        early = b"".join(archive([(name, b"test microcode")]) for name in hardware.MICROCODE)
        self.initrd.write_bytes(early + gzip.compress(archive([("init", b"test init")])) )
        for pattern in hardware.FIRMWARE:
            path = self.root / "usr/lib/firmware" / pattern.replace("*", "test")
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b"test firmware")
        self.modules = self.root / "usr/lib/modules" / self.kernel
        self.modules.mkdir(parents=True)
        # Built-in drivers are just as usable as .ko files.
        # Of each group (ntfs3 or ntfs) the first is there.
        names = list(hardware.MODULES) + [group[0] for group in hardware.ANY_OF]
        (self.modules / "modules.builtin").write_text("\n".join("kernel/" + name.replace("_", "-") + ".ko" for name in names))

    def test_both_vendors_before_the_compressed_root(self):
        hardware.check_image(self.root, self.kernel)
        for missing in hardware.MICROCODE:
            entries = [(name, b"microcode") for name in hardware.MICROCODE if name != missing]
            # The late root must never satisfy the early microcode requirement.
            self.initrd.write_bytes(archive(entries) + gzip.compress(archive([(missing, b"late microcode")])))
            with self.assertRaisesRegex(ValueError, "missing early microcode"):
                hardware.check_microcode(self.initrd)

    def test_one_driver_of_a_group_is_enough(self):
        # Linux 7 calls its NTFS driver ntfs instead of ntfs3.
        builtin = self.modules / "modules.builtin"
        builtin.write_text(builtin.read_text().replace("kernel/ntfs3.ko", "kernel/ntfs.ko"))
        hardware.check_image(self.root, self.kernel)
        builtin.write_text(builtin.read_text().replace("kernel/ntfs.ko\n", "").replace("\nkernel/ntfs.ko", ""))
        with self.assertRaisesRegex(ValueError, "ntfs3/ntfs"):
            hardware.check_image(self.root, self.kernel)

    def test_empty_and_truncated_microcode_are_rejected(self):
        self.initrd.write_bytes(archive([(name, b"") for name in hardware.MICROCODE]))
        with self.assertRaisesRegex(ValueError, "missing early microcode"):
            hardware.check_microcode(self.initrd)
        self.initrd.write_bytes(archive([(hardware.MICROCODE[0], b"data")])[:100])
        with self.assertRaisesRegex(ValueError, "truncated"):
            hardware.check_microcode(self.initrd)

    def test_missing_firmware_or_module_fails_the_release_gate(self):
        path = next((self.root / "usr/lib/firmware").glob(hardware.FIRMWARE[0]))
        path.unlink()
        with self.assertRaisesRegex(ValueError, "firmware missing"):
            hardware.check_image(self.root, self.kernel)
        path.write_bytes(b"restored")
        (self.modules / "modules.builtin").write_text("")
        with self.assertRaisesRegex(ValueError, "kernel modules missing"):
            hardware.check_image(self.root, self.kernel)


if __name__ == "__main__":
    unittest.main()
