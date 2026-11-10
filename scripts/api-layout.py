#!/usr/bin/env python3
"""The overview of backend/internal/api: which files belong together.

  api-layout.py            prints docs/API-LAYOUT.md as it should be
  api-layout.py --write    writes it
  api-layout.py --check    exits 1 if the file is not current

The package is one flat folder; the first word of a file name (up to the
first "_") is its group. The overview names the files and the areas
(types "<x>Area", docs/rules/API.md) of each group, so that a new handler
finds its place. scripts/tests/test_api_layout.py keeps it current.
"""
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
API = REPO / "backend/internal/api"
DOC = REPO / "docs/API-LAYOUT.md"
AREA = re.compile(r"^type (\w+Area) struct", re.M)

HEAD = """# Aufbau von `backend/internal/api`

Erzeugt von `scripts/api-layout.py --write`; nicht von Hand ändern. Der Test
`scripts/tests/test_api_layout.py` schlägt fehl, wenn die Übersicht nicht
mehr stimmt.

Das Paket ist ein flacher Ordner. Was zusammengehört, zeigt der Dateiname:
Das erste Wort (bis zum ersten `_`) ist die Gruppe. Regeln dazu stehen in
`docs/rules/API.md` („Dateien im API-Paket“ und „Bereiche statt `s.opts`“).

| Gruppe | Dateien | Bereiche | Tests |
|---|---|---|---|
"""


def layout():
    groups = {}
    for path in sorted(API.glob("*.go")):
        name = path.name.removesuffix(".go")
        test = name.endswith("_test")
        group = name.removesuffix("_test").split("_")[0]
        entry = groups.setdefault(group, {"files": [], "areas": [], "tests": 0})
        if test:
            entry["tests"] += 1
            continue
        entry["files"].append(path.name)
        entry["areas"] += AREA.findall(path.read_text())
    rows = []
    for group in sorted(groups):
        entry = groups[group]
        if not entry["files"]:
            # Tests of something that lies in several groups.
            continue
        files = ", ".join(f"`{name}`" for name in entry["files"])
        areas = ", ".join(f"`{name}`" for name in sorted(set(entry["areas"]))) or "–"
        rows.append(f"| {group} | {files} | {areas} | {entry['tests']} |")
    return HEAD + "\n".join(rows) + "\n"


def main(args):
    text = layout()
    if args == ["--write"]:
        DOC.write_text(text)
    elif args == ["--check"]:
        if not DOC.is_file() or DOC.read_text() != text:
            print("docs/API-LAYOUT.md is not current: run scripts/api-layout.py --write", file=sys.stderr)
            return 1
    elif args:
        raise SystemExit(__doc__)
    else:
        sys.stdout.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
