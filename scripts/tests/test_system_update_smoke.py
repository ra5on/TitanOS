import importlib.util
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / "smoke-system-update.py"
sys.path.insert(0, str(SCRIPT.parent))
spec = importlib.util.spec_from_file_location("smoke_system_update", SCRIPT)
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class SystemUpdateSmokeTest(unittest.TestCase):
    def test_entry_names_tell_the_tries_left(self):
        self.assertEqual(smoke.tries_left("titanos-a.conf"), ("a", None))
        self.assertEqual(smoke.tries_left("titanos-b+3.conf"), ("b", 3))
        self.assertEqual(smoke.tries_left("titanos-b+2-1.conf"), ("b", 2))
        self.assertEqual(smoke.tries_left("titanos-a+0-3.conf"), ("a", 0))
        self.assertIsNone(smoke.tries_left("titanos-c.conf"))
        self.assertIsNone(smoke.tries_left("other.conf"))

    def test_grub_counts_the_starts_of_its_trial_in_grubenv(self):
        # GRUB cannot rename on FAT: the name stays, the count rises.
        self.assertEqual(smoke.tries_left("titanos-b+3.conf", "titanos-b+3.conf", 2), ("b", 1))
        self.assertEqual(smoke.tries_left("titanos-b+3.conf", "titanos-b+3.conf", 3), ("b", 0))
        self.assertEqual(smoke.tries_left("titanos-b+2-1.conf", "titanos-b+2-1.conf", 5), ("b", 0))
        # A count for another entry (an earlier trial) does not apply.
        self.assertEqual(smoke.tries_left("titanos-a+3.conf", "titanos-b+3.conf", 2), ("a", 3))

    def test_timed_out_creation_checks_state_without_formatting_twice(self):
        posts = []
        def request(path, body=None, *, timeout=None):
            if body is not None:
                posts.append(path)
                raise TimeoutError("reply lost after applying change")
            return {
                "disks": [{"name": "vda", "serial": "NORDLICHT-DATA", "identity": "disk1"}],
                "volumes": [{"name": "testdaten", "mounted": True}],
            }
        def wait(_label, check, _patience):
            self.assertTrue(check())
        with patch("interrupted_system_smoke.check_share", return_value=True) as read_share:
            smoke.create_share(request, wait, Path("/unused"), 1, "test")
        self.assertEqual(posts, ["/api/v1/storage/volumes", "/api/v1/shares"])
        read_share.assert_called_once()

    def test_versions_of_the_test_releases(self):
        self.assertEqual(smoke.bump("0.75.0", 2), "0.75.2")
        self.assertEqual(smoke.bump("0.2.0-beta.4", 1), "0.2.0-beta.5")
        with self.assertRaises(ValueError):
            smoke.bump("0.75", 1)


if __name__ == "__main__":
    unittest.main()
