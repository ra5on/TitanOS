"""The boot log check of the smoke tests."""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import boot_log  # noqa: E402


class BootLogTest(unittest.TestCase):
    def test_problems_are_found_once_and_known_lines_pass(self):
        log = "\n".join([
            "[    1.0] platform regulatory.0: Direct firmware load for regulatory.db failed with error -2",
            "[    1.1] platform regulatory.0: Direct firmware load for regulatory.db failed with error -2",
            "[   20.9] i915 0000:00:02.0: [drm] *ERROR* GT1: GSC proxy component didn't bind within the expected timeout",
            "\x1b[0;1;31m[FAILED]\x1b[0m Failed to start \x1b[0;1;39mdocker.service\x1b[0m.",
            "[    2.0] RAS: Correctable Errors collector initialized.",
            "[  100.0] watchdog: watchdog0: watchdog did not stop!",
            "[    0.5] vmd 0000:00:0e.0: Unknown Bus Offset Setting (3)",
            "[    3.0] usb 1-1: new high-speed USB device number 2 using xhci_hcd",
        ])
        found = boot_log.problems(log)
        self.assertEqual(found, [
            "platform regulatory.0: Direct firmware load for regulatory.db failed with error -2",
            "i915 0000:00:02.0: [drm] *ERROR* GT1: GSC proxy component didn't bind within the expected timeout",
            "[FAILED] Failed to start docker.service.",
            "vmd 0000:00:0e.0: Unknown Bus Offset Setting (3)",
        ])

    def test_late_emulated_cpus_count_only_without_kvm(self):
        log = "\n".join([
            "[   30.1] watchdog: BUG: soft lockup - CPU#1 stuck for 23s! [systemd:1]",
            "[   30.1] Call Trace:",
            "[    0.9] clocksource: Watchdog remote CPU 1 read timed out",
            "[   40.0] rcu: INFO: rcu_preempt self-detected stall on CPU",
        ])
        self.assertEqual(len(boot_log.problems(log)), 4)
        self.assertEqual(boot_log.problems(log, emulated=True), [])
        # A real Oops still counts under emulation.
        self.assertEqual(boot_log.problems(log + "\n[ 50.0] Oops: general protection fault", emulated=True),
                         ["Oops: general protection fault"])

    def test_check_raises_with_the_lines(self):
        import tempfile
        with tempfile.NamedTemporaryFile("wb", suffix=".log") as file:
            file.write(b"[ 1.0] cfg80211: failed to load regulatory.db\n[ 1.1] firmware: failed to load regulatory.db (-2)\n")
            file.flush()
            with self.assertRaisesRegex(AssertionError, "regulatory.db"):
                boot_log.check(Path(file.name))
            file.seek(0)
            file.truncate()
            file.write(b"[ 1.0] Linux version 6.12\n")
            file.flush()
            boot_log.check(Path(file.name))


if __name__ == "__main__":
    unittest.main()
