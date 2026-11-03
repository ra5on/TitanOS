"""Which common plugin sets may be signed without the owner's approval."""
import copy
import importlib.util
import io
import json
import subprocess
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("plugin_guard", REPO / "scripts/plugin-guard.py")
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


def entry(name, version, digest):
    return {"name": name, "version": version, "manifest": "m" + digest, "sha256": "s" + digest, "size": 4096, "inputs": "i" + digest}


PUBLISHED = {
    "tag": "common-3", "debian": "trixie", "sysextLevel": "1", "kernel": "", "kernelPackage": "", "minBase": "0.45.1",
    "plugins": [entry("coral-usb", "1.0-1", "c"), entry("intel-gpu-tools", "2.0-1-1", "g"),
                entry("netbird", "0.81.5-1", "n"), entry("tailscale", "1.104.1-1", "t")],
}


def draft(change=None):
    index = copy.deepcopy(PUBLISHED)
    index["tag"], index["minBase"] = "common-4", "0.46.2"
    index["plugins"][3] = entry("tailscale", "1.104.5-4", "T")
    if change:
        change(index)
    return index


class CheckTest(unittest.TestCase):
    def refused(self, index, published=PUBLISHED):
        with self.assertRaises(guard.Refused):
            guard.check(index, published)

    def test_newer_tailscale_and_netbird_pass(self):
        self.assertEqual(guard.check(draft(), PUBLISHED), ["tailscale 1.104.1-1 -> 1.104.5-4"])

        def netbird(index):
            index["plugins"][2] = entry("netbird", "0.82.0-4", "N")
        self.assertEqual(len(guard.check(draft(netbird), PUBLISHED)), 2)

    def test_any_other_change_needs_the_owner(self):
        def coral(index):
            index["plugins"][0]["sha256"] = "other"
        def added(index):
            index["plugins"].append(entry("nvidia", "615.71.09-1", "x"))
        def removed(index):
            del index["plugins"][1]
        def doubled(index):
            index["plugins"].append(entry("tailscale", "1.104.6-4", "U"))
        def same_version(index):
            index["plugins"][3] = entry("tailscale", "1.104.1-4", "T")
        def older(index):
            index["plugins"][3] = entry("tailscale", "1.104.0-4", "T")
        def kernel(index):
            index["kernel"] = "6.12.115+deb13.1-amd64"
        def tag(index):
            index["tag"] = "common-3"
        def kernel_set(index):
            index["tag"] = "kernel-6.12.115-2-1"
        for change in (coral, added, removed, doubled, same_version, older, kernel, tag, kernel_set):
            with self.subTest(change.__name__):
                self.refused(draft(change))
        self.refused(copy.deepcopy(PUBLISHED) | {"tag": "common-4"})  # nothing changed


class SignatureTest(unittest.TestCase):
    """The published index must carry a signature of a key of the image."""

    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = Path(tmp.name)
        self.keys = self.dir / "keys"
        self.keys.mkdir()
        for name in ("trusted", "foreign"):
            subprocess.run(["minisign", "-G", "-W", "-p", str(self.dir / f"{name}.pub"), "-s", str(self.dir / f"{name}.key")],
                           check=True, stdout=subprocess.DEVNULL)
        (self.keys / "titanos-release-2.pub").write_bytes((self.dir / "trusted.pub").read_bytes())
        for name, index in (("draft", draft()), ("published", PUBLISHED)):
            (self.dir / name).mkdir()
            (self.dir / name / "index.json").write_text(json.dumps(index))

    def sign(self, key):
        subprocess.run(["minisign", "-S", "-s", str(self.dir / f"{key}.key"), "-m", str(self.dir / "published/index.json")],
                       check=True, stdout=subprocess.DEVNULL)

    def run_guard(self):
        out = io.StringIO()
        with redirect_stdout(out):
            code = guard.main(["common", str(self.dir / "draft"), str(self.dir / "published"), "--keys", str(self.keys)])
        return code, out.getvalue()

    def test_signed_by_the_image(self):
        self.sign("trusted")
        code, out = self.run_guard()
        self.assertEqual(code, 0, out)
        self.assertIn("tailscale 1.104.1-1 -> 1.104.5-4", out)

    def test_foreign_or_missing_signature(self):
        code, out = self.run_guard()
        self.assertEqual(code, 1)
        self.sign("foreign")
        code, out = self.run_guard()
        self.assertEqual(code, 1)
        self.assertIn("not signed", out)


if __name__ == "__main__":
    unittest.main()
