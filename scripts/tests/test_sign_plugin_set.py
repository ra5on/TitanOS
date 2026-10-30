"""Signing drafts of ra5on/TitanOS-Plugins with code of this repository only."""
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
spec = importlib.util.spec_from_file_location("sign_plugin_set", REPO / "scripts/sign-plugin-set.py")
signer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(signer)
spec = importlib.util.spec_from_file_location("plugin_set", REPO / "scripts/plugin-set.py")
plugin_set = importlib.util.module_from_spec(spec)
spec.loader.exec_module(plugin_set)

KERNEL, PACKAGE = "6.12.113+deb13-amd64", "6.12.113-1"


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


@unittest.skipUnless(shutil.which("minisign"), "minisign not installed")
class SignPluginSetTest(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp)
        self.keys = self.tmp / "keys"
        self.keys.mkdir()
        self.key = self.tmp / "test.key"
        subprocess.run(["minisign", "-G", "-W", "-s", str(self.key), "-p", str(self.keys / "test.pub")],
                       check=True, capture_output=True)

    def draft(self, tag, kernel, package, plugins):
        """A set as the plugin repository leaves it: unsigned."""
        directory = self.tmp / tag
        directory.mkdir()
        items = []
        for name, version in plugins.items():
            raw = directory / (name + ".raw")
            raw.write_bytes(f"{name} {version}".encode())
            manifest = directory / (name + ".plugin.json")
            manifest.write_text(json.dumps({"name": name, "version": version, "kernel": kernel,
                                            "sha256": sha(raw), "size": raw.stat().st_size}))
            (directory / (name + "-licenses.tar.gz")).write_bytes(b"licenses")
            items.append({"name": name, "version": version, "manifest": sha(manifest), "sha256": sha(raw),
                          "size": raw.stat().st_size})
        assets = [{"name": p.name, "sha256": sha(p), "size": p.stat().st_size} for p in sorted(directory.iterdir())]
        (directory / "index.json").write_text(json.dumps({
            "tag": tag, "debian": "trixie", "sysextLevel": "1", "kernel": kernel, "kernelPackage": package,
            "minBase": "0.36.0", "plugins": items, "assets": assets}))
        return directory

    def test_sets_and_pointer_are_what_titanos_accepts(self):
        kernel = self.draft("kernel-6.12.113-1-1", KERNEL, PACKAGE, {"nvidia": "1-1"})
        common = self.draft("common-1", "", "", {"tailscale": "1-1"})
        signer.sign_set(kernel, self.key, self.keys)
        signer.sign_set(common, self.key, self.keys)
        comment = (common / "tailscale.plugin.json.minisig").read_text().splitlines()[2]
        self.assertEqual(comment, "trusted comment: TitanOS plugins: tailscale.plugin.json")
        # The same checks as a release that fetches the sets.
        plugin_set.check_index(kernel, self.keys)
        plugin_set.check_index(common, self.keys)

        signer.pointer(kernel, "-", self.tmp / "catalog1", self.key, self.keys)
        signer.pointer(common, self.tmp / "catalog1", self.tmp / "catalog2", self.key, self.keys)
        catalog = json.loads((self.tmp / "catalog2/catalog.json").read_text())
        self.assertEqual(catalog["serial"], 2)
        self.assertEqual(catalog["common"], {"tag": "common-1", "index": sha(common / "index.json")})
        self.assertEqual(catalog["kernels"], [{"kernel": KERNEL, "kernelPackage": PACKAGE, "tag": "kernel-6.12.113-1-1",
                                               "index": sha(kernel / "index.json")}])
        self.assertTrue(signer.verified(self.tmp / "catalog2/catalog.json", self.keys))

    def test_tampered_draft_and_foreign_catalog_are_refused(self):
        common = self.draft("common-1", "", "", {"tailscale": "1-1"})
        (common / "tailscale.raw").write_bytes(b"changed")
        with self.assertRaises(SystemExit):
            signer.sign_set(common, self.key, self.keys)
        other = self.tmp / "other"
        other.mkdir()
        (other / "catalog.json").write_text("{}")
        good = self.draft("common-2", "", "", {"tailscale": "1-2"})
        signer.sign_set(good, self.key, self.keys)
        with self.assertRaisesRegex(SystemExit, "untrusted"):
            signer.pointer(good, other, self.tmp / "out", self.key, self.keys)

    def test_a_key_the_image_does_not_know_is_refused(self):
        common = self.draft("common-1", "", "", {"tailscale": "1-1"})
        stranger = self.tmp / "stranger.key"
        subprocess.run(["minisign", "-G", "-W", "-s", str(stranger), "-p", str(self.tmp / "stranger.pub")],
                       check=True, capture_output=True)
        with self.assertRaisesRegex(SystemExit, "untrusted"):
            signer.sign_set(common, stranger, self.keys)

    def test_nvidia_update_extends_the_published_channel(self):
        web = self.tmp / "web"
        published = web / "releases/download/kernel-6.12.113-1-1"
        published.mkdir(parents=True)
        handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(web))
        handler.log_message = lambda *a: None
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.shutdown)
        source = f"http://127.0.0.1:{server.server_port}"

        def draft(revision, driver):
            directory = self.tmp / f"pending-{revision}"
            directory.mkdir()
            raw = directory / "image"
            raw.write_bytes(f"nvidia {revision}".encode())
            digest = sha(raw)
            raw.rename(directory / f"nvidia-update-{digest}.raw")
            archives = {}
            for name in ("licenses", "kernel-sources"):
                path = directory / f"nvidia-update-{digest}-{name}.tar.gz"
                path.write_bytes(name.encode())
                archives[path.name] = {"sha256": sha(path), "size": path.stat().st_size}
            (directory / f"nvidia-update-{digest}.plugin.json").write_text(json.dumps({
                "name": "nvidia", "version": f"{driver}-{revision}", "kernel": KERNEL, "sha256": digest,
                "size": len(f"nvidia {revision}"), "revision": revision, "driver": driver, "toolkit": "1.17.0",
                "archives": archives}))
            (directory / "target.json").write_text(json.dumps({"tag": "kernel-6.12.113-1-1", "kernel": KERNEL,
                                                               "kernelPackage": PACKAGE}))
            return directory

        first = draft(1, "580.1")
        signer.sign_nvidia(first, self.key, self.keys, source)
        channel = json.loads((first / "nvidia-updates.json").read_text())
        self.assertEqual([v["revision"] for v in channel["versions"]], [1])
        for name in ("nvidia-updates.json", "nvidia-updates.json.minisig"):
            shutil.copy(first / name, published / name)
        second = draft(2, "590.1")
        signer.sign_nvidia(second, self.key, self.keys, source)
        channel = json.loads((second / "nvidia-updates.json").read_text())
        self.assertEqual([v["revision"] for v in channel["versions"]], [2, 1])
        self.assertEqual(channel["recommended"], channel["versions"][0]["sha256"])
        with self.assertRaisesRegex(SystemExit, "revision must increase"):
            signer.sign_nvidia(first, self.key, self.keys, source)


if __name__ == "__main__":
    unittest.main()
