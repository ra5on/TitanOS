#!/usr/bin/env python3
"""Turns lspci reports of linux-hardware.org into the hwcheck test corpus.

The reports come from github.com/linuxhw/LsPCI (CC-BY-4.0, hardware probes
uploaded by Linux users). Each is the output of `lspci -vvnn` with the
kernel driver and modules of that machine. This keeps per device only what
matching needs: slot, class, IDs and the driver/modules that machine used,
and writes one gzip-compressed JSON file.

Usage: scripts/linuxhw-corpus.py <checkout of LsPCI> <list of report paths> <output.json.gz> <commit>

The checkout may be sparse: only the listed reports need to be there.
"""
import gzip
import json
import re
import sys
from pathlib import Path

HEADER = re.compile(
    r"^(?P<slot>[0-9a-f]{2,4}:[0-9a-f]{2}\.[0-7]|[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]) "
    r".*?\[(?P<class>[0-9a-f]{4})\]: .*\[(?P<vendor>[0-9a-f]{4}):(?P<device>[0-9a-f]{4})\]"
    r"(?: \(rev (?P<rev>[0-9a-f]{2})\))?(?: \(prog-if (?P<progif>[0-9a-f]{2}))?"
)
SUBSYSTEM = re.compile(r"^\tSubsystem: .*\[(?P<sv>[0-9a-f]{4}):(?P<sd>[0-9a-f]{4})\]")


def parse(text):
    devices, current = [], None
    for line in text.splitlines():
        match = HEADER.match(line)
        if match:
            current = {
                "class": match["class"] + (match["progif"] or "00"),
                "vendor": match["vendor"],
                "device": match["device"],
                "sv": "", "sd": "", "driver": "", "modules": [],
            }
            devices.append(current)
            continue
        if current is None:
            continue
        if sub := SUBSYSTEM.match(line):
            current["sv"], current["sd"] = sub["sv"], sub["sd"]
        elif line.startswith("\tKernel driver in use: "):
            current["driver"] = line.split(": ", 1)[1].strip()
        elif line.startswith("\tKernel modules: "):
            current["modules"] = [name.strip() for name in line.split(": ", 1)[1].split(",") if name.strip()]
    return devices


def main():
    checkout, listing, output, commit = Path(sys.argv[1]), Path(sys.argv[2]), Path(sys.argv[3]), sys.argv[4]
    machines = []
    for path in listing.read_text().splitlines():
        report = checkout / path
        if not path or not report.is_file():
            continue
        parts = path.split("/")
        devices = parse(report.read_text(errors="replace"))
        if devices:
            # Type, vendor, model, kernel: no probe or machine IDs.
            machines.append({"type": parts[0], "vendor": parts[1], "model": parts[2], "kernel": parts[-3].lower(),
                             "arch": parts[-2].lower(), "devices": devices})
    corpus = {
        "source": "https://github.com/linuxhw/LsPCI",
        "commit": commit,
        "license": "CC-BY-4.0, linux-hardware.org (linuxhw)",
        "machines": machines,
    }
    with gzip.open(output, "wt") as file:
        json.dump(corpus, file, separators=(",", ":"), sort_keys=True)
    print(f"{len(machines)} machines, {sum(len(m['devices']) for m in machines)} devices -> {output}")


if __name__ == "__main__":
    main()
