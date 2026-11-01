"""Finds problems in the serial console log of a test boot.

The smoke tests boot the real image in QEMU with the console on the serial
port: kernel messages and systemd's unit status land in the log. A line
that looks like a problem (missing firmware, a driver error, a timeout, a
unit that failed) fails the test unless KNOWN explains it. New hardware
support, packages or waits must not bring new messages unnoticed.
"""
import re

PROBLEM = re.compile(
    r"Direct firmware load for \S+ failed|firmware: failed to load|\*ERROR\*|"
    r"\[FAILED\]|Failed to start|Failed with result|"
    r"timed out|timeout|didn't bind|"
    r"BUG:|Oops|Call Trace|segfault|Kernel panic|"
    r"I/O error|probe of \S+ failed|failed with error|Unknown Bus Offset",
    re.IGNORECASE,
)

# Lines that match PROBLEM but are fine in QEMU, with the reason.
KNOWN = [
    (r"systemd(-shutdown)?\[1\]: .*[Ww]atchdog running with a hardware timeout", "systemd arms the hardware watchdog"),
    (r"Failed unmounting run-live-medium\.mount", "live-boot keeps the stick (read-only) mounted until power-off"),
    (r"watchdog: watchdog0: watchdog did not stop", "QEMU's iTCO watchdog at shutdown"),
    (r"RAS: Correctable Errors collector initialized", "not an error"),
    (r"hung_task_timeout_secs", "a kernel setting, not a hang"),
    (r"x-systemd\.device-timeout", "an fstab option in a message, not a timeout"),
]
KNOWN_RE = [(re.compile(pattern), reason) for pattern, reason in KNOWN]
ANSI = re.compile(r"\x1b\[[0-9;:?]*[A-Za-z]")
STAMP = re.compile(r"^\[\s*\d+\.\d+\]\s*")


def problems(text):
    """The problem lines of a serial log, each once, in order of appearance."""
    found = []
    for raw in text.splitlines():
        line = STAMP.sub("", ANSI.sub("", raw)).strip()
        if not line or not PROBLEM.search(line):
            continue
        if any(pattern.search(line) for pattern, _ in KNOWN_RE):
            continue
        if line not in found:
            found.append(line)
    return found


def check(path):
    """Raises with the problem lines of the serial log at path."""
    text = path.read_bytes().decode("utf-8", errors="replace")
    lines = problems(text)
    if lines:
        raise AssertionError("boot log shows problems (fix them or explain them in scripts/boot_log.py):\n  " + "\n  ".join(lines[:40]))
