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
        for name in hardware.REQUIRED_FILES:
            (self.root / "usr/lib/firmware").mkdir(parents=True, exist_ok=True)
            (self.root / "usr/lib/firmware" / name).write_bytes(b"test")
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

    def module(self, relative, *firmware):
        """A module whose .modinfo names the firmware files, xz-compressed."""
        import lzma
        path = self.modules / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        info = b"".join(b"firmware=" + name.encode() + b"\0" for name in firmware)
        path.write_bytes(lzma.compress(b"\x7fELF\0license=GPL\0" + info))

    def test_firmware_of_nas_drivers_must_be_there_or_explained(self):
        self.module("kernel/drivers/net/ethernet/realtek/r8169.ko.xz", "rtl_nic/rtl8125b-2.fw")
        with self.assertRaisesRegex(ValueError, "r8169: rtl_nic/rtl8125b-2.fw"):
            hardware.check_image(self.root, self.kernel)
        firmware = self.root / "usr/lib/firmware/rtl_nic/rtl8125b-2.fw.zst"
        firmware.parent.mkdir(parents=True)
        firmware.write_bytes(b"compressed is fine")
        hardware.check_image(self.root, self.kernel)
        # Known gaps with a reason, and WLAN, are not the gate's business.
        self.module("kernel/drivers/gpu/drm/amd/amdgpu/amdgpu.ko.xz", "amdgpu/vega10_ip_discovery.bin")
        self.module("kernel/drivers/net/wireless/intel/iwlwifi/iwlwifi.ko.xz", "iwlwifi-100-5.ucode")
        hardware.check_image(self.root, self.kernel)

    def test_links_are_followed_inside_the_image(self):
        # Debian's alternatives: firmware/regulatory.db -> /etc/alternatives/... -> /lib/firmware/regulatory.db-debian,
        # all absolute. They must resolve in the image, not on the build machine.
        firmware = self.root / "usr/lib/firmware"
        (firmware / "regulatory.db").unlink()
        alternatives = self.root / "etc/alternatives"
        alternatives.mkdir(parents=True)
        (alternatives / "regulatory.db").symlink_to("/lib/firmware/regulatory.db-debian")
        (firmware / "regulatory.db").symlink_to("/etc/alternatives/regulatory.db")
        (self.root / "lib").symlink_to("usr/lib")
        with self.assertRaisesRegex(ValueError, "regulatory.db"):
            hardware.check_image(self.root, self.kernel)
        (firmware / "regulatory.db-debian").write_bytes(b"db")
        hardware.check_image(self.root, self.kernel)

    def test_a_chip_newer_than_the_firmware_is_reported_not_fatal(self):
        firmware = self.root / "usr/lib/firmware/amdgpu"
        firmware.mkdir(parents=True, exist_ok=True)
        (firmware / "gc_11_5_0_me.bin").write_bytes(b"x")
        # No file at all for gc 11.5.4 or the new NPU directory: newer than the packages.
        self.module("kernel/drivers/gpu/drm/amd/amdgpu/amdgpu.ko.xz", "amdgpu/gc_11_5_4_me.bin", "amdgpu/gc_11_5_4_mec.bin")
        self.module("kernel/drivers/accel/amdxdna/amdxdna.ko.xz", "amdnpu/17f0_20/npu.sbin")
        # On the default kernel that is a missing package.
        with self.assertRaisesRegex(ValueError, "gc_11_5_4_me.bin"):
            hardware.check_image(self.root, self.kernel)
        variant = self.root / "usr/share/titanos/kernel-variant"
        variant.parent.mkdir(parents=True)
        variant.write_text("new\n")
        hardware.check_image(self.root, self.kernel)
        # A chip with some files but not all: a packaging error.
        self.module("kernel/drivers/gpu/drm/amd/amdgpu/amdgpu.ko.xz", "amdgpu/gc_11_5_0_me.bin", "amdgpu/gc_11_5_0_mec.bin")
        with self.assertRaisesRegex(ValueError, "gc_11_5_0_mec.bin"):
            hardware.check_image(self.root, self.kernel)

    def test_chip_of(self):
        self.assertEqual(hardware.chip_of("amdgpu/gc_11_5_4_mes1.bin"), "amdgpu/gc_11_5_4")
        self.assertEqual(hardware.chip_of("amdgpu/dcn_4_2_1_dmcub.bin"), "amdgpu/dcn_4_2_1")
        self.assertEqual(hardware.chip_of("intel/vpu/vpu_60xx_v1.bin"), "intel/vpu/vpu_60xx")
        self.assertEqual(hardware.chip_of("rtl_nic/rtl8125b-2.fw"), "rtl_nic/rtl8125b-2")

    def test_regulatory_db_is_required(self):
        (self.root / "usr/lib/firmware/regulatory.db").unlink()
        with self.assertRaisesRegex(ValueError, "regulatory.db"):
            hardware.check_image(self.root, self.kernel)

    def test_no_graphics_driver_in_the_initramfs(self):
        early = b"".join(archive([(name, b"test microcode")]) for name in hardware.MICROCODE)
        main = archive([("init", b"x")]) + archive([("usr/lib/modules/k/kernel/drivers/gpu/drm/i915/i915.ko.xz", b"x")])
        self.initrd.write_bytes(early + gzip.compress(main))
        with self.assertRaisesRegex(ValueError, "graphics drivers in the initramfs.*i915"):
            hardware.check_image(self.root, self.kernel)
        # Two archives one after the other are both read; xhci is fine.
        main = archive([("init", b"x")]) + archive([("usr/lib/modules/k/kernel/drivers/usb/host/xhci-pci.ko.xz", b"x")])
        self.initrd.write_bytes(early + gzip.compress(main))
        hardware.check_image(self.root, self.kernel)


if __name__ == "__main__":
    unittest.main()
