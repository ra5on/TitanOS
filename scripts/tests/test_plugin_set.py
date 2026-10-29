"""Fetching plugin sets of ra5on/TitanOS-Plugins and carrying them in a release."""
import functools
import hashlib
import http.server
import importlib.util
import json
import shutil
import subprocess
import tempfile
import threading
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("plugin_set", REPO / "scripts/plugin-set.py")
plugin_set = importlib.util.module_from_spec(spec)
spec.loader.exec_module(plugin_set)

KERNEL, PACKAGE = "6.12.113+deb13-amd64", "6.12.113-1"


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


@unittest.skipUnless(shutil.which("minisign"), "minisign not installed")
class PluginSetTest(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp)
        self.keys = self.tmp / "keys"
        self.keys.mkdir()
        self.key = self.tmp / "test.key"
        subprocess.run(["minisign", "-G", "-W", "-s", str(self.key), "-p", str(self.keys / "test.pub")], check=True, capture_output=True)
        self.releases = self.tmp / "web/releases/download"
        handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(self.tmp / "web"))
        handler.log_message = lambda *a: None
        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=self.server.serve_forever, daemon=True).start()
        self.addCleanup(self.server.shutdown)
        self.source = f"http://127.0.0.1:{self.server.server_port}"

    def sign(self, path):
        subprocess.run(["minisign", "-S", "-s", str(self.key), "-m", str(path)], check=True, capture_output=True)

    def publish(self, tag, kernel, package, plugins, extras=(), min_base="0.36.0"):
        directory = self.releases / tag
        directory.mkdir(parents=True)
        items = []
        for name, version in plugins.items():
            raw = directory / (name + ".raw")
            raw.write_bytes(f"{name} {version}".encode())
            manifest = directory / (name + ".plugin.json")
            manifest.write_text(json.dumps({"name": name, "version": version, "kernel": kernel, "sha256": sha(raw), "size": raw.stat().st_size}))
            self.sign(manifest)
            (directory / (name + "-licenses.tar.gz")).write_bytes(b"licenses")
            items.append({"name": name, "version": version, "manifest": sha(manifest), "sha256": sha(raw), "size": raw.stat().st_size})
        for name, content in extras:
            (directory / name).write_text(content)
        assets = [{"name": p.name, "sha256": sha(p), "size": p.stat().st_size}
                  for p in sorted(directory.iterdir()) if not p.name.endswith(".minisig")]
        index = directory / "index.json"
        index.write_text(json.dumps({"tag": tag, "debian": "trixie", "sysextLevel": "1", "kernel": kernel, "kernelPackage": package,
                                     "minBase": min_base, "plugins": items, "assets": assets}))
        self.sign(index)
        return {"tag": tag, "index": sha(index)} | ({"kernel": kernel, "kernelPackage": package} if kernel else {})

    def catalog(self, common, kernels):
        directory = self.releases / "catalog"
        directory.mkdir(parents=True, exist_ok=True)
        path = directory / "catalog.json"
        path.write_text(json.dumps({"serial": 1, "debian": "trixie", "sysextLevel": "1", "common": common, "kernels": kernels}))
        self.sign(path)

    def fetch(self, base="0.36.0"):
        args = type("Args", (), dict(keys=str(self.keys), base=base, source=self.source, dest=str(self.tmp / "sets"),
                                     kernel=KERNEL, kernel_package=PACKAGE))
        return plugin_set.fetch(args)

    def test_fetch_and_import_carry_sets_into_a_release(self):
        sdk = json.dumps({"asset": "nvidia-build-sdk.tar.zst", "kernel": KERNEL, "version": "kernel-6.12.113-1-1"})
        kernel = self.publish("kernel-6.12.113-1-1", KERNEL, PACKAGE, {"nvidia": "615.71.09-1"}, [("nvidia-build-sdk.json", sdk)])
        common = self.publish("common-2", "", "", {"tailscale": "1.92.1-2"})
        self.catalog(common, [kernel])
        self.assertEqual(self.fetch(), 0)
        out = self.tmp / "out/plugins"
        args = type("Args", (), dict(base="0.36.0", keys=str(self.keys), out=str(out),
                                     sets=[str(self.tmp / "sets/kernel"), str(self.tmp / "sets/common")]))
        plugin_set.import_sets(args)
        manifest = json.loads((out / "tailscale.plugin.json").read_text())
        self.assertEqual(manifest["version"], "1.92.1-2-titanos-0.36.0")
        self.assertEqual(sha(out / "tailscale.raw"), manifest["sha256"])
        self.assertFalse((out / "tailscale.plugin.json.minisig").exists(), "the release signs the changed manifest")
        self.assertEqual(json.loads((out / "nvidia-build-sdk.json").read_text())["version"], "0.36.0")

    def test_missing_kernel_set_and_newer_sets(self):
        self.assertEqual(self.fetch(), plugin_set.NOT_FOUND)  # no catalog at all
        other = self.publish("kernel-6.12.112-1-1", "6.12.112+deb13-amd64", "6.12.112-1", {"nvidia": "1-1"})
        self.catalog(None, [other])
        self.assertEqual(self.fetch(), plugin_set.NOT_FOUND)
        kernel = self.publish("kernel-6.12.113-1-1", KERNEL, PACKAGE, {"nvidia": "1-1"}, min_base="0.37.0")
        self.catalog(None, [other, kernel])
        with self.assertRaisesRegex(SystemExit, "meant for TitanOS 0.37.0"):
            self.fetch()

    def test_tampering_is_refused(self):
        kernel = self.publish("kernel-6.12.113-1-1", KERNEL, PACKAGE, {"nvidia": "1-1"})
        self.catalog(None, [kernel])
        (self.releases / "kernel-6.12.113-1-1/nvidia.raw").write_bytes(b"changed")
        with self.assertRaisesRegex(SystemExit, "does not match"):
            self.fetch()
        # A catalog signed by an unknown key.
        (self.keys / "test.pub").rename(self.tmp / "elsewhere.pub")
        with self.assertRaisesRegex(SystemExit, "untrusted"):
            self.fetch()


if __name__ == "__main__":
    unittest.main()
