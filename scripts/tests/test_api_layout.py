import subprocess
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]


class APILayout(unittest.TestCase):
    def test_overview_is_current(self):
        """A new file in backend/internal/api belongs into docs/API-LAYOUT.md."""
        run = subprocess.run([sys.executable, str(REPO / "scripts/api-layout.py"), "--check"], capture_output=True, text=True)
        self.assertEqual(run.returncode, 0, run.stderr)


if __name__ == "__main__":
    unittest.main()
