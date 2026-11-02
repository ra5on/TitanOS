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
    r"BUG:|Oops|Call Trace|segfault|Kernel panic|detected stalls?|hard LOCKUP|"
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
# Only where QEMU emulates the CPU (no KVM, as on GitHub's runners): a
# virtual CPU can wait tens of seconds for its turn. On KVM or real
# hardware these lines stay problems.
EMULATED = [
    (r"clocksource: Watchdog remote CPU \d+ read timed out", "a late emulated CPU"),
    (r"watchdog: BUG: soft lockup - CPU#\d+ stuck for \d+s", "a late emulated CPU"),
    (r"rcu: INFO: rcu_\w+ (self-)?detected stall", "a late emulated CPU"),
    (r"^Call Trace:$", "printed with the lines above; an Oops or BUG of its own still counts"),
]
KNOWN_RE = [(re.compile(pattern), reason) for pattern, reason in KNOWN]
EMULATED_RE = [(re.compile(pattern), reason) for pattern, reason in EMULATED]
ANSI = re.compile(r"\x1b\[[0-9;:?]*[A-Za-z]")
STAMP = re.compile(r"^\[\s*\d+\.\d+\]\s*")


def problems(text, emulated=False):
    """The problem lines of a serial log, each once, in order of appearance.
    emulated: QEMU ran without KVM."""
    known = KNOWN_RE + (EMULATED_RE if emulated else [])
    found = []
    for raw in text.splitlines():
        line = STAMP.sub("", ANSI.sub("", raw)).strip()
        if not line or not PROBLEM.search(line):
            continue
        if any(pattern.search(line) for pattern, _ in known):
            continue
        if line not in found:
            found.append(line)
    return found


def check(path, emulated=False):
    """Raises with the problem lines of the serial log at path."""
    text = path.read_bytes().decode("utf-8", errors="replace")
    lines = problems(text, emulated)
    if lines:
        raise AssertionError("boot log shows problems (fix them or explain them in scripts/boot_log.py):\n  " + "\n  ".join(lines[:40]))
