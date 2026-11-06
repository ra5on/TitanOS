"""Reusing the viewer image of the previous release."""
import functools
import hashlib
import http.server
import importlib.util
import io
import json
import shutil
import subprocess
import tarfile
import tempfile
import threading
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("viewer_reuse", REPO / "scripts/viewer-image-reuse.py")
reuse = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reuse)

IMAGE_ID = "sha256:" + "c" * 64
MANIFESTS = {
    "v0.59.0": {"tag": "v0.59.0"},  # before the remote view
    "v0.60.0": {"tag": "v0.60.0", "viewer": {"asset": "titanos-viewer-0.60.0.tar.zst", "imageId": IMAGE_ID}},
}
TAGS = ["v0.59.0", "v0.60.0", "v0.61.0-beta.1"]


class DecideTest(unittest.TestCase):
    def decide(self, version, changed):
        return reuse.decide(version, TAGS, MANIFESTS.get, lambda tag: changed)

    def test_unchanged_image_is_reused(self):
        tag, manifest = self.decide("0.61.0", ["backend/internal/api/viewer.go", "web/src/x.ts", "docs/FERNANSICHT.md"])
        self.assertEqual(tag, "v0.60.0")
        self.assertEqual(manifest["viewer"]["imageId"], IMAGE_ID)

    def test_changes_to_the_image_build_it(self):
        for path in ["viewer/Dockerfile", "backend/cmd/titanos-viewer/agent.go", "backend/internal/viewer/proto/proto.go",
                     "scripts/build-viewer-image.sh", "backend/go.sum"]:
            with self.subTest(path=path):
                tag, reason = self.decide("0.61.0", [path])
                self.assertIsNone(tag)
                self.assertIn(path, reason)

    def test_without_an_earlier_image_it_is_built(self):
        self.assertIsNone(self.decide("0.60.0", [])[0])


@unittest.skipUnless(shutil.which("minisign"), "minisign not installed")
class ReuseTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)
        self.keys = self.dir / "keys"
        self.keys.mkdir()
        subprocess.run(["minisign", "-G", "-W", "-s", str(self.dir / "secret.key"), "-p", str(self.keys / "test.pub")],
                       check=True, capture_output=True)
        root = self.dir / "web/releases/download/v0.60.0"
        root.mkdir(parents=True)
        self.root = root
        self.image = b"zstd image bytes"
        (root / "titanos-viewer-0.60.0.tar.zst").write_bytes(self.image)
        licenses = io.BytesIO()
        with tarfile.open(fileobj=licenses, mode="w:gz") as archive:
            data = b"chromium 156.0-1 chromium 156.0-1\n"
            info = tarfile.TarInfo("viewer/packages.txt")
            info.size = len(data)
            archive.addfile(info, io.BytesIO(data))
        (root / "titanos-0.60.0-licenses.tar.gz").write_bytes(licenses.getvalue())
        sums = "".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in sorted(root.iterdir()))
        (root / "SHA256SUMS").write_text(sums)
        self.sign("SHA256SUMS")
        handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(self.dir / "web"))
        handler.log_message = lambda *a: None
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        self.source = f"http://127.0.0.1:{server.server_port}"

    def sign(self, name):
        subprocess.run(["minisign", "-S", "-W", "-s", str(self.dir / "secret.key"), "-m", str(self.root / name)], check=True, capture_output=True)

    def manifest(self, sha=None):
        return {"tag": "v0.60.0", "viewer": {"asset": "titanos-viewer-0.60.0.tar.zst", "imageId": IMAGE_ID,
                                             "sha256": sha or hashlib.sha256(self.image).hexdigest(), "size": len(self.image)}}

    def test_reused_under_the_new_name_with_the_same_id(self):
        out = self.dir / "out"
        reuse.reuse("0.61.0", out, "v0.60.0", self.manifest(), self.source, self.keys)
        self.assertEqual((out / "titanos-viewer-0.61.0.tar.zst").read_bytes(), self.image)
        self.assertEqual((out / "titanos-viewer-0.61.0.image-id").read_text().strip(), IMAGE_ID)
        self.assertIn("chromium", (out / "titanos-viewer-0.61.0.packages.txt").read_text())

    def test_archive_must_match_the_signed_manifest(self):
        with self.assertRaisesRegex(ValueError, "does not match"):
            reuse.reuse("0.61.0", self.dir / "out", "v0.60.0", self.manifest(sha="0" * 64), self.source, self.keys)
        self.assertFalse((self.dir / "out/titanos-viewer-0.61.0.tar.zst").exists())

    def test_checksums_must_be_signed(self):
        (self.root / "SHA256SUMS.minisig").write_text("forged")
        with self.assertRaisesRegex(ValueError, "signed"):
            reuse.reuse("0.61.0", self.dir / "out", "v0.60.0", self.manifest(), self.source, self.keys)

    def test_signed_checks_the_signature(self):
        data = (self.root / "SHA256SUMS").read_bytes()
        sig = (self.root / "SHA256SUMS.minisig").read_bytes()
        self.assertTrue(reuse.signed(data, sig, self.keys))
        self.assertFalse(reuse.signed(data + b"x", sig, self.keys))


if __name__ == "__main__":
    unittest.main()
