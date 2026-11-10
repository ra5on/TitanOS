import importlib.util
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "smoke-system-update.py"
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

    def test_versions_of_the_test_releases(self):
        self.assertEqual(smoke.bump("0.75.0", 2), "0.75.2")
        self.assertEqual(smoke.bump("0.2.0-beta.4", 1), "0.2.0-beta.5")
        with self.assertRaises(ValueError):
            smoke.bump("0.75", 1)


if __name__ == "__main__":
    unittest.main()
