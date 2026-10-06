"""Real TAR restoration preserves timestamps without touching substituted paths."""
import io
import os
from pathlib import Path
import stat
import tarfile
import unittest
from unittest.mock import patch

from tests import test_backups as fixtures
from titan.backups import archive_mtime_ns, digest_file, restore_mtime
from titan.core import Error, atomic_json


class BackupTimestampTests(unittest.TestCase):
    setUp = fixtures.BackupTests.setUp
    tearDown = fixtures.BackupTests.tearDown

    def replace_archive(self, backup, entries):
        directory = self.backups.namespace() / backup["id"]
        archive_path = directory / "archive.tar.gz"
        total = 0
        with tarfile.open(archive_path, "w:gz", format=tarfile.PAX_FORMAT) as archive:
            for name, content, modified in entries:
                member = tarfile.TarInfo("shares/data/" + name if name else "shares/data")
                member.pax_headers = {"mtime": modified}
                if content is None:
                    member.type = tarfile.DIRTYPE
                    archive.addfile(member)
                else:
                    member.size = len(content)
                    total += len(content)
                    archive.addfile(member, io.BytesIO(content))
        backup.update(entries=len(entries), unpacked_bytes=total,
                      bytes=archive_path.stat().st_size, sha256=digest_file(archive_path))
        atomic_json(directory / "manifest.json", backup)

    def test_real_backup_restores_file_and_parent_directory_times_after_children(self):
        source = self.host.share_root / "data"
        expected = {"hello.txt": 1650000001, "nested/other.txt": 1650000002,
                    "nested": 1650000003, ".": 1650000004}
        for name, seconds in expected.items():
            os.utime(source / name, ns=(seconds * 1000000000, seconds * 1000000000))
        backup = self.backups.create(include_config=False)
        result = self.backups.restore(backup["id"], "data", "times")
        restored = Path(result["path"]) / "data"
        self.assertEqual((restored / "hello.txt").read_text(), "valuable file")
        self.assertEqual((restored / "nested/other.txt").read_text(), "nested value")
        for name, seconds in expected.items():
            with self.subTest(name=name):
                self.assertEqual((restored / name).stat().st_mtime_ns, seconds * 1000000000)

    def test_pax_nanoseconds_preserved_for_file_and_directory(self):
        backup = self.backups.create(include_config=False)
        self.replace_archive(backup, [("", None, "1650000000.123456789"),
                                      ("nested", None, "1650000001.987654321"),
                                      ("nested/item.txt", b"pax contents", "1650000002.000000001")])
        result = self.backups.restore(backup["id"], "data", "pax-times")
        restored = Path(result["path"]) / "data"
        self.assertEqual(restored.stat().st_mtime_ns, 1650000000123456789)
        self.assertEqual((restored / "nested").stat().st_mtime_ns, 1650000001987654321)
        self.assertEqual((restored / "nested/item.txt").stat().st_mtime_ns, 1650000002000000001)
        self.assertEqual((restored / "nested/item.txt").read_bytes(), b"pax contents")

    def test_selected_subtree_preserves_directory_time(self):
        source = self.host.share_root / "data/nested"
        os.utime(source, ns=(1650000003000000000, 1650000003000000000))
        backup = self.backups.create(include_config=False)
        result = self.backups.restore_selection(backup["id"], ["data/nested"], "data", "selected-times")
        restored = Path(result["path"]) / "data/nested"
        self.assertEqual(restored.stat().st_mtime_ns, 1650000003000000000)
        self.assertEqual((restored / "other.txt").read_text(), "nested value")

    def test_invalid_and_overflow_pax_times_rejected_before_output(self):
        backup = self.backups.create(include_config=False)
        for index, modified in enumerate(("nan", "inf", "-inf", "broken", "9223372037",
                                          "-9223372037", "1e100", "9" * 100)):
            with self.subTest(modified=modified):
                self.replace_archive(backup, [("item.txt", b"contents", modified)])
                name = "invalid-time-" + str(index)
                with self.assertRaises(Error):
                    self.backups.restore(backup["id"], "data", name)
                self.assertFalse((self.host.share_root / "data" / name).exists())

    def test_nan_inf_bool_and_unbounded_numeric_times_denied(self):
        for value in (float("nan"), float("inf"), float("-inf"), True, 10 ** 100,
                      -(10 ** 100), 9223372037, -9223372037):
            with self.subTest(value=repr(value)), self.assertRaises(Error):
                archive_mtime_ns(value)

    def test_filesystem_timestamp_overflow_is_a_controlled_error(self):
        path = self.root / "timestamp-overflow"
        path.write_bytes(b"contents")
        with path.open("rb") as stream, patch("titan.backups.os.utime", side_effect=OverflowError):
            with self.assertRaisesRegex(Error, "sicher wiederhergestellt"):
                restore_mtime(stream.fileno(), 1650000000000000000)
        self.assertEqual(path.read_bytes(), b"contents")

    def test_directory_symlink_replacement_does_not_change_external_timestamp(self):
        self.assert_replaced_directory_refused(symlink=True)

    def test_directory_inode_replacement_does_not_change_replacement_timestamp(self):
        self.assert_replaced_directory_refused(symlink=False)

    def assert_replaced_directory_refused(self, *, symlink):
        backup = self.backups.create(include_config=False)
        destination = self.host.share_root / "data/replaced-time/data/nested"
        outside = self.root / "outside"
        outside.mkdir()
        expected = 1660000000000000000
        os.utime(outside, ns=(expected, expected))
        changed = False

        def apply_with_replacement(fd, modified):
            nonlocal changed
            restore_mtime(fd, modified)
            if not changed and stat.S_ISREG(os.fstat(fd).st_mode) and Path(os.readlink(f"/proc/self/fd/{fd}")) == destination / "other.txt":
                destination.rename(destination.with_name("kept-nested"))
                if symlink:
                    destination.symlink_to(outside, target_is_directory=True)
                else:
                    destination.mkdir()
                    os.utime(destination, ns=(expected, expected))
                changed = True

        with patch("titan.backups.restore_mtime", side_effect=apply_with_replacement):
            with self.assertRaisesRegex(Error, "Verzeichnis.*(Pfadänderung|ersetzt)"):
                self.backups.restore(backup["id"], "data", "replaced-time")
        self.assertTrue(changed)
        self.assertEqual(outside.stat().st_mtime_ns, expected)
        if not symlink:
            self.assertEqual(destination.stat().st_mtime_ns, expected)
        self.assertEqual((destination.with_name("kept-nested") / "other.txt").read_text(), "nested value")


if __name__ == "__main__":
    unittest.main()
