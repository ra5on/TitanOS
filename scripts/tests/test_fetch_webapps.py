"""The web apps of a release: catalog, download check and archive rules."""
import hashlib
import importlib.util
import json
import stat
import tempfile
import unittest
import zipfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("fetch_webapps", REPO / "scripts/fetch-webapps.py")
fw = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fw)


def make_zip(path, entries):
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
        for name, body, mode in entries:
            info = zipfile.ZipInfo(name)
            info.compress_type = zipfile.ZIP_DEFLATED
            if mode is None:
                mode = (stat.S_IFDIR | 0o755) if name.endswith("/") else (stat.S_IFREG | 0o644)
            info.external_attr = mode << 16
            z.writestr(info, body)


GOOD = [
    ("demo-web-1.0.0/", b"", None),
    ("demo-web-1.0.0/index.html", b"<canvas id=demo_canvas></canvas>", None),
    ("demo-web-1.0.0/demo-1.js", b"export {}", None),
    ("demo-web-1.0.0/LICENSE-MIT", b"MIT", None),
    ("demo-web-1.0.0/LICENSE-APACHE", b"Apache", None),
    ("demo-web-1.0.0/OFL-font.txt", b"OFL", None),
    ("demo-web-1.0.0/snippets/", b"", None),
    ("demo-web-1.0.0/snippets/inline0.js", b"x", None),
]


class FetchWebappsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.source = self.tmp / "source"
        self.source.mkdir()

    def catalog(self, archive):
        data = archive.read_bytes()
        path = self.tmp / "catalog.json"
        path.write_text(json.dumps({"apps": [{
            "id": "demo", "name": "Demo", "repo": "example/demo", "version": "1.0.0",
            "upstreamAsset": "demo-web-1.0.0.zip", "sha256": hashlib.sha256(data).hexdigest(), "size": len(data),
            "license": "MIT OR Apache-2.0", "bridge": "host", "types": [], "default": False}]}))
        return path

    def test_repository_catalog(self):
        apps = fw.load_catalog(REPO / "webapps/catalog.json")
        self.assertEqual(len(apps), 14)
        self.assertEqual(sorted(a["id"] for a in apps if a["default"]), ["gridcraft", "pdfcraft", "wordcraft"])
        # titand embeds a copy; both must be the same.
        self.assertEqual((REPO / "webapps/catalog.json").read_bytes(),
                         (REPO / "backend/internal/webapps/catalog.json").read_bytes())

    def test_fetch_copies_the_archive_and_its_licenses(self):
        archive = self.source / "demo-web-1.0.0.zip"
        make_zip(archive, GOOD)
        out = self.tmp / "out"
        entries = fw.fetch(self.catalog(archive), out, source=self.source)
        self.assertEqual(entries[0]["asset"], "titanos-webapp-demo-1.0.0.zip")
        self.assertEqual((out / "titanos-webapp-demo-1.0.0.zip").read_bytes(), archive.read_bytes())
        self.assertEqual(json.loads((out / "webapps.json").read_text()), entries)
        self.assertEqual(sorted(p.name for p in (out / "licenses/demo").iterdir()), ["LICENSE-APACHE", "LICENSE-MIT", "OFL-font.txt"])

    def test_checksum_mismatch_is_refused(self):
        archive = self.source / "demo-web-1.0.0.zip"
        make_zip(archive, GOOD)
        catalog = self.catalog(archive)
        make_zip(archive, GOOD + [("demo-web-1.0.0/extra.js", b"evil", None)])
        with self.assertRaisesRegex(ValueError, "SHA-256"):
            fw.fetch(catalog, self.tmp / "out", source=self.source)

    def test_unsafe_archives_are_refused(self):
        cases = {
            "traversal": GOOD + [("demo-web-1.0.0/../evil.js", b"x", None)],
            "outside": GOOD + [("other/x.js", b"x", None)],
            "absolute": GOOD + [("/demo-web-1.0.0/x.js", b"x", None)],
            "backslash": GOOD + [("demo-web-1.0.0/a\\b.js", b"x", None)],
            "symlink": GOOD + [("demo-web-1.0.0/link", b"/etc/passwd", stat.S_IFLNK | 0o777)],
            "duplicate": GOOD + [("demo-web-1.0.0/DEMO-1.js", b"x", None)],
            "no index": [e for e in GOOD if not e[0].endswith("index.html")],
        }
        for name, entries in cases.items():
            with self.subTest(name):
                path = self.tmp / (name.replace(" ", "_") + ".zip")
                make_zip(path, entries)
                with self.assertRaises(ValueError):
                    fw.validate_archive(path, "demo", "1.0.0")

    def test_limits(self):
        path = self.tmp / "bomb.zip"
        make_zip(path, GOOD + [("demo-web-1.0.0/big.bin", b"\0" * (2 << 20), None)])
        old = fw.MAX_UNPACKED
        fw.MAX_UNPACKED = 1 << 20
        try:
            with self.assertRaisesRegex(ValueError, "unpacked"):
                fw.validate_archive(path, "demo", "1.0.0")
        finally:
            fw.MAX_UNPACKED = old
        many = self.tmp / "many.zip"
        make_zip(many, GOOD + [(f"demo-web-1.0.0/f{i}.js", b"", None) for i in range(fw.MAX_FILES)])
        with self.assertRaisesRegex(ValueError, "files"):
            fw.validate_archive(many, "demo", "1.0.0")


if __name__ == "__main__":
    unittest.main()
