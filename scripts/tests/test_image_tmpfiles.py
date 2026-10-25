"""Image-only fixes retain the first effective distro rule and other config."""
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("tmpfiles_normalizer", ROOT / "scripts/normalize-image-tmpfiles.py")
normalizer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(normalizer)


class TmpfilesTest(unittest.TestCase):
    def test_duplicate_rules_keep_debian_permissions_and_are_idempotent(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            directory = root / "usr/lib/tmpfiles.d"
            directory.mkdir(parents=True)
            (directory / "debian.conf").write_text("d /run/lock 1777 root root -\nf /run/utmp 0664 root utmp -\n")
            (directory / "legacy.conf").write_text("# vendor copyright\nd /run/lock 0755 root root -\nL /var/lock - - - - ../run/lock\n")
            (directory / "x11-common.conf").write_text("f /run/utmp 0664 root utmp -\nD /tmp/.X11-unix 1777 root root -\n")
            self.assertEqual(normalizer.normalize(root), 2)
            self.assertIn("d /run/lock 1777", (directory / "debian.conf").read_text())
            self.assertIn("# vendor copyright", (directory / "legacy.conf").read_text())
            self.assertIn("L /var/lock", (directory / "legacy.conf").read_text())
            self.assertIn("D /tmp/.X11-unix", (directory / "x11-common.conf").read_text())
            self.assertEqual(normalizer.normalize(root), 0)

    def test_override_and_mask_precedence(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            vendor = root / "usr/lib/tmpfiles.d"
            local = root / "etc/tmpfiles.d"
            vendor.mkdir(parents=True)
            local.mkdir(parents=True)
            original = "d /run/lock 0755 root root -\n"
            (vendor / "a.conf").write_text(original)
            (local / "a.conf").symlink_to("/dev/null")
            (vendor / "b.conf").write_text("d /run/lock 1777 root root -\n")
            self.assertEqual(normalizer.normalize(root), 0)
            self.assertEqual((vendor / "a.conf").read_text(), original)
