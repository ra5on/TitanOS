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
import urllib.error
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


PACKAGES = """chromium 156.0.8078.79-1~deb13u1 chromium 156.0.8078.79-1~deb13u1
chromium-common 156.0.8078.79-1~deb13u1 chromium 156.0.8078.79-1~deb13u1
libx264-164 2:0.164.3108+git31e19f9-2+b1 x264 2:0.164.3108+git31e19f9-2
gstreamer1.0-plugins-bad 1.26.2-2 gst-plugins-bad1.0 1.26.2-2
mesa-libgallium 25.0.7-2+deb13u1 mesa 25.0.7-2+deb13u1
coreutils 9.7-3 coreutils 9.7-3
"""
POLICY = """chromium:
  Installed: (none)
  Candidate: 156.0.8078.79-1~deb13u1
  Version table:
     156.0.8078.79-1~deb13u1 500
chromium-common:
  Installed: (none)
  Candidate: 156.0.8078.79-1~deb13u1
libx264-164:
  Installed: (none)
  Candidate: 2:0.164.3108+git31e19f9-2+b1
gstreamer1.0-plugins-bad:
  Installed: (none)
  Candidate: 1.26.2-2
mesa-libgallium:
  Installed: (none)
  Candidate: 25.0.7-2+deb13u1
"""
DAY = 86400


class SecurityTest(unittest.TestCase):
    def candidates(self, **change):
        c = reuse.parse_policy(POLICY)
        c.update(change)
        return c

    def test_parse(self):
        self.assertEqual(reuse.parse_policy(POLICY)["libx264-164"], "2:0.164.3108+git31e19f9-2+b1")
        self.assertEqual(reuse.parse_packages(PACKAGES)["coreutils"], "9.7-3")
        names = [n for n in reuse.parse_packages(PACKAGES) if reuse.SECURITY.match(n)]
        self.assertEqual(sorted(names), ["chromium", "chromium-common", "gstreamer1.0-plugins-bad", "libx264-164", "mesa-libgallium"])

    def test_fresh_and_unchanged_is_reused(self):
        self.assertIsNone(reuse.security_reason(PACKAGES, self.candidates(), 1000 * DAY, 1003 * DAY))

    def test_old_image_is_rebuilt(self):
        reason = reuse.security_reason(PACKAGES, self.candidates(), 1000 * DAY, 1007 * DAY + 1)
        self.assertIn("days old", reason)

    def test_security_update_is_rebuilt(self):
        for name in ["chromium", "libx264-164", "gstreamer1.0-plugins-bad", "mesa-libgallium"]:
            with self.subTest(name=name):
                reason = reuse.security_reason(PACKAGES, self.candidates(**{name: "99.0-1~deb13u1"}), 1000 * DAY, 1001 * DAY)
                self.assertIn(name, reason)

    def test_unknown_candidate_is_rebuilt(self):
        c = self.candidates()
        del c["chromium"]
        self.assertIn("unknown", reuse.security_reason(PACKAGES, c, 1000 * DAY, 1001 * DAY))

    def test_other_package_updates_do_not_matter(self):
        self.assertIsNone(reuse.security_reason(PACKAGES.replace("9.7-3", "9.8-1"), self.candidates(), 1000 * DAY, 1001 * DAY))

    def test_without_chromium_it_is_rebuilt(self):
        self.assertIsNotNone(reuse.security_reason("coreutils 9.7-3 coreutils 9.7-3\n", {}, 1000 * DAY, 1001 * DAY))

    def test_candidate_versions_asks_the_pinned_base(self):
        calls = []

        class Result:
            returncode, stdout, stderr = 0, POLICY, ""

        def run(cmd, **kw):
            calls.append(cmd)
            return Result()

        got = reuse.candidate_versions(["chromium", "libx264-164"], run=run)
        self.assertEqual(got["chromium"], "156.0.8078.79-1~deb13u1")
        self.assertIn("debian:trixie-slim@sha256:", calls[0][3])
        self.assertIn("apt-cache policy chromium libx264-164", calls[0][-1])
        with self.assertRaises(ValueError):
            reuse.candidate_versions(["x; rm -rf /"], run=run)

        class Failed:
            returncode, stdout, stderr = 100, "", "no network"
        with self.assertRaises(RuntimeError):
            reuse.candidate_versions(["chromium"], run=lambda *a, **k: Failed())

    def test_image_age_follows_reuse_chain(self):
        manifests = {
            "v0.60.0": {"viewer": {"imageId": "sha256:" + "a" * 64}},
            "v0.61.0": {"viewer": {"imageId": "sha256:" + "b" * 64}},
            "v0.62.0": {"viewer": {"imageId": "sha256:" + "b" * 64}},
            "v0.63.0": {"viewer": {"imageId": "sha256:" + "b" * 64}},
        }
        times = {"v0.60.0": 1, "v0.61.0": 2, "v0.62.0": 3, "v0.63.0": 4}
        built = reuse.image_built_at("v0.63.0", list(manifests), manifests.get, times.get)
        self.assertEqual(built, 2)  # built for 0.61.0, reused by 0.62.0 and 0.63.0


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


class FetchRetries(unittest.TestCase):
    """GitHub answered release downloads with 504 for minutes (9 Nov 2026)."""

    def serve(self, answers):
        calls = []

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                code = answers[min(len(calls), len(answers) - 1)]
                calls.append(code)
                self.send_response(code)
                self.end_headers()
                if code == 200:
                    self.wfile.write(b"manifest")

            def log_message(self, *args):
                pass

        server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        return f"http://127.0.0.1:{server.server_address[1]}/m", calls

    def test_server_errors_are_tried_again(self):
        url, calls = self.serve([504, 504, 200])
        self.assertEqual(reuse.fetch(url, 100, pause=0), b"manifest")
        self.assertEqual(calls, [504, 504, 200])

    def test_missing_is_not_tried_again(self):
        url, calls = self.serve([404])
        self.assertIsNone(reuse.fetch(url, 100, pause=0))
        self.assertEqual(calls, [404])

    def test_gives_up(self):
        # Not a github.com download: no way through the API either.
        url, calls = self.serve([503])
        with self.assertRaises(urllib.error.HTTPError):
            reuse.fetch(url, 100, pause=0)
        self.assertEqual(len(calls), 4)

    def test_api_fallback_only_for_github_downloads(self):
        kind = reuse.release_kind
        self.assertIsNone(kind.api_asset("https://example.org/releases/download/v1/x", 5))
        self.assertEqual(
            kind.GITHUB_DOWNLOAD.match("https://github.com/ra5on/TitanOS/releases/download/v0.70.2/manifest.json").groups(),
            ("ra5on/TitanOS", "v0.70.2", "manifest.json"),
        )
