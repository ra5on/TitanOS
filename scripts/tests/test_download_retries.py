"""Downloads of the release tools that break off are tried again (B-11).

A connection that drops in the middle of a file (the server sends less than
its Content-Length and closes) starts the download again from the
beginning: in repack-release.py (the base image), viewer-image-reuse.py (the
viewer image and the files around it) and autopilot.py (the manifest that
says whether a release is published).
"""
import hashlib
import http.client
import http.server
import importlib.util
import tempfile
import threading
import unittest
import urllib.error
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, REPO / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


repack = load("repack_release", "scripts/repack-release.py")
viewer = load("viewer_reuse_retries", "scripts/viewer-image-reuse.py")
autopilot = load("autopilot_retries", "scripts/autopilot.py")


class Server:
    """Answers GET with the next of answers: "cut" sends half of the body
    with the full Content-Length and closes, a number is that status, "ok"
    the whole body. The last answer repeats."""

    def __init__(self, test, answers, body):
        self.calls = []
        calls = self.calls

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                answer = answers[min(len(calls), len(answers) - 1)]
                calls.append(answer)
                if isinstance(answer, int):
                    self.send_response(answer)
                    self.send_header("Content-Length", "0")
                    self.end_headers()
                    return
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body if answer == "ok" else body[: len(body) // 2])
                self.wfile.flush()
                self.close_connection = True

            def log_message(self, *args):
                pass

        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        server.daemon_threads = True
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        test.addCleanup(server.server_close)
        test.addCleanup(thread.join)
        test.addCleanup(server.shutdown)
        self.url = f"http://127.0.0.1:{server.server_port}"


BODY = b"0123456789abcdef" * 4096


class RepackDownloadTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.target = Path(tmp.name) / "image.zip"

    def test_a_download_that_breaks_off_starts_again(self):
        server = Server(self, ["cut", "ok"], BODY)
        repack.download(server.url + "/f", self.target, pause=0)
        self.assertEqual(self.target.read_bytes(), BODY)
        self.assertEqual(server.calls, ["cut", "ok"])

    def test_gives_up_and_leaves_no_partial_file(self):
        server = Server(self, ["cut"], BODY)
        with self.assertRaises(http.client.IncompleteRead):
            repack.download(server.url + "/f", self.target, pause=0)
        self.assertEqual(server.calls, ["cut"] * 3)
        self.assertFalse(self.target.exists())

    def test_missing_is_not_tried_again(self):
        server = Server(self, [404], BODY)
        with self.assertRaises(urllib.error.HTTPError):
            repack.download(server.url + "/f", self.target, pause=0)
        self.assertEqual(server.calls, [404])


class ViewerDownloadTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.out = Path(tmp.name) / "out"

    def manifest(self):
        return {"tag": "v0.60.0", "viewer": {"asset": "titanos-viewer-0.60.0.tar.zst", "imageId": "sha256:" + "c" * 64,
                                             "sha256": hashlib.sha256(BODY).hexdigest(), "size": len(BODY)}}

    def test_the_image_starts_again_when_it_breaks_off(self):
        server = Server(self, ["cut", "ok"], BODY)
        viewer.reuse("0.61.0", self.out, "v0.60.0", self.manifest(), server.url, None, packages=b"chromium\n", pause=0)
        self.assertEqual((self.out / "titanos-viewer-0.61.0.tar.zst").read_bytes(), BODY)
        self.assertEqual(server.calls, ["cut", "ok"])

    def test_the_image_gives_up_without_a_partial_file(self):
        server = Server(self, ["cut"], BODY)
        with self.assertRaises(http.client.IncompleteRead):
            viewer.reuse("0.61.0", self.out, "v0.60.0", self.manifest(), server.url, None, packages=b"x\n", pause=0)
        self.assertEqual(len(server.calls), 3)
        self.assertFalse((self.out / "titanos-viewer-0.61.0.tar.zst").exists())

    def test_a_small_file_starts_again_when_it_breaks_off(self):
        server = Server(self, ["cut", "ok"], BODY)
        self.assertEqual(viewer.fetch(server.url + "/SHA256SUMS", len(BODY), pause=0), BODY)
        self.assertEqual(server.calls, ["cut", "ok"])


class AutopilotPublishedTest(unittest.TestCase):
    MANIFEST = b'{"tag": "v1.2.0"}'

    def test_server_errors_are_tried_again(self):
        server = Server(self, [503, 502, "ok"], self.MANIFEST)
        self.assertTrue(autopilot.published(server.url, "v1.2.0", pause=0))
        self.assertEqual(server.calls, [503, 502, "ok"])

    def test_a_manifest_that_breaks_off_is_read_again(self):
        server = Server(self, ["cut", "ok"], self.MANIFEST)
        self.assertTrue(autopilot.published(server.url, "v1.2.0", pause=0))
        self.assertEqual(server.calls, ["cut", "ok"])

    def test_missing_is_unpublished_at_once(self):
        server = Server(self, [404], self.MANIFEST)
        self.assertFalse(autopilot.published(server.url, "v1.2.0", pause=0))
        self.assertEqual(server.calls, [404])

    def test_other_client_errors_are_not_tried_again(self):
        server = Server(self, [403], self.MANIFEST)
        with self.assertRaises(urllib.error.HTTPError):
            autopilot.published(server.url, "v1.2.0", pause=0)
        self.assertEqual(server.calls, [403])

    def test_gives_up(self):
        server = Server(self, [503], self.MANIFEST)
        with self.assertRaises(urllib.error.HTTPError):
            autopilot.published(server.url, "v1.2.0", pause=0)
        self.assertEqual(len(server.calls), 4)


if __name__ == "__main__":
    unittest.main()
