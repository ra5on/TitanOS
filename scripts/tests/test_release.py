"""Release regressions use tiny real squashfs images and real minisign signatures."""
import functools
import http.server
import importlib.util
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import threading
import unittest
import zipfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, REPO / "scripts" / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


licenses = load("licenses", "third-party-licenses.py")
verify = load("verify", "verify-release.py")
release_keys = load("release_keys", "check-release-keys.py")


class PackageInventoryTest(unittest.TestCase):
    def test_installed_source_versions_and_epochs(self):
        status = """Package: installed
Status: install ok installed
Version: 2:1.0-3+b2

Package: different
Status: install ok installed
Version: 9.2-1+b1
Source: actual (9.2-1)

Package: removed
Status: deinstall ok not-installed
Version: 1.0

Package: configured
Status: deinstall ok config-files
Version: 1.0
"""
        self.assertEqual(list(licenses.debian_packages(status)), [
            ("installed", "2:1.0-3+b2", "installed", "2:1.0-3"),
            ("different", "9.2-1+b1", "actual", "9.2-1")])

    def test_missing_or_outside_image_copyright_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            notices = root / "usr/share/titanos"
            notices.mkdir(parents=True)
            (notices / "components.json").write_text("[]")
            (root / "var/lib/dpkg").mkdir(parents=True)
            (root / "var/lib/dpkg/status").write_text("Package: test\nStatus: install ok installed\nVersion: 1\n")
            with self.assertRaisesRegex(ValueError, "missing in-image"):
                licenses.image(root)
            doc = root / "usr/share/doc/test"
            doc.mkdir(parents=True)
            (doc / "copyright").symlink_to("/etc/passwd")
            with self.assertRaisesRegex(ValueError, "missing in-image"):
                licenses.image(root)


@unittest.skipUnless(all(shutil.which(c) for c in ["mksquashfs", "unsquashfs", "minisign", "zip"]),
                     "release tools not installed (make test-release requires them)")
class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name)
        (self.repo / "scripts").mkdir()
        for name in ["make-release.sh", "sign-release.sh"]:
            shutil.copy2(REPO / "scripts" / name, self.repo / "scripts" / name)
        shutil.copytree(REPO / "boot", self.repo / "boot")
        shutil.copytree(REPO / "plugins/catalog", self.repo / "plugins/catalog")
        self.root = self.repo / "build/mkosi.output/rootfs"
        self.root.mkdir(parents=True)
        self.slot = self.repo / "out/stick/titanos/a"
        self.slot.mkdir(parents=True)
        (self.repo / "out/stick/config").mkdir()
        (self.root / "usr/lib").mkdir(parents=True)
        (self.root / "usr/lib/os-release").write_text('ID=titanos\nVERSION_ID=0.1.0-beta.1\n')
        (self.root / "var/lib/dpkg").mkdir(parents=True)
        (self.root / "var/lib/dpkg/status").write_text(
            "Package: linux-image-6.12-test\nStatus: install ok installed\nVersion: 6.12.0-1\n")
        (self.root / "usr/bin").mkdir(parents=True)
        (self.root / "usr/bin/titand").write_bytes(b"titand 0.1.0-beta.1")
        (self.root / "usr/share/titanos").mkdir(parents=True)
        (self.root / "usr/share/titanos/base-version").write_text("0.1.0-beta.1\n")
        (self.repo / "backend/bin").mkdir(parents=True)
        (self.repo / "backend/bin/titand").write_bytes(b"titand 0.1.0-beta.1")
        cli = self.repo / "backend/bin/titanosctl"
        cli.write_bytes(b"CLI fixture")
        cli.chmod(0o755)
        notices = self.root / "usr/share/titanos"
        (notices / "licenses").mkdir(parents=True, exist_ok=True)
        for name in ["LICENSE.md", "THIRD_PARTY_LICENSES", "licenses/NOTICE"]:
            (notices / name).write_text("Test notice\n")
        (notices / "components.json").write_text(json.dumps([
            dict(ecosystem="Debian", name="example", version="1", source="https://example.org/source", notices=["NOTICE"])]))
        (self.root.parent / "kernel-version").write_text("6.12-test\n")
        (self.slot / "version").write_text("0.1.0-beta.1\n")
        for name in ["vmlinuz", "initrd.img"]:
            (self.slot / name).write_bytes(b"test\n")
        plugins = self.repo / "out/plugins"
        plugins.mkdir()
        (plugins / "nvidia.raw").write_bytes(b"plugin image")
        (plugins / "nvidia.plugin.json").write_text(json.dumps(dict(
            name="nvidia", version="615.71.09-titanos-0.1.0-beta.1", kernel="6.12-test",
            sha256=hashlib.sha256(b"plugin image").hexdigest(), size=len(b"plugin image"))))
        for name in ["nvidia-kernel-sources.tar.gz", "nvidia-licenses.tar.gz"]:
            (plugins / name).write_bytes(b"archive fixture")
        (plugins / "nvidia-build-sdk.tar.zst").write_bytes(b"SDK fixture")
        (plugins / "nvidia-build-sdk-sources.txt").write_text("SDK sources fixture")
        (plugins / "nvidia-build-sdk.json").write_text(json.dumps(dict(asset="nvidia-build-sdk.tar.zst",version="0.1.0-beta.1",kernel="6.12-test",sha256=hashlib.sha256(b"SDK fixture").hexdigest(),size=len(b"SDK fixture"))))
        self.release = self.repo / "out/release"
        self.keys = self.repo / "keys"
        self.keys.mkdir()
        self.command("minisign", "-G", "-W", "-s", str(self.repo / "secret.key"), "-p", str(self.keys / "test.pub"))

    def command(self, *args, success=True):
        env = dict(os.environ)
        env.pop("ALLOW_DEV", None)
        env.pop("MINISIGN_PASSWORD", None)
        result = subprocess.run(args, cwd=self.repo, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        if success:
            self.assertEqual(result.returncode, 0, result.stdout)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout)
        return result.stdout

    def pack(self):
        self.command("mksquashfs", str(self.root), str(self.slot / "filesystem.squashfs"), "-noappend", "-quiet", "-processors", "1")
        self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta")

    def sign(self):
        self.command("scripts/sign-release.sh", str(self.release), str(self.repo / "secret.key"))

    def test_release_and_tamper_detection(self):
        self.pack()
        self.sign()
        self.assertEqual(verify.verify(self.release, self.keys), "0.1.0-beta.1")
        manifest = json.loads((self.release / "manifest.json").read_text())
        self.assertEqual(manifest["app"]["base"], "0.1.0-beta.1")
        self.assertEqual(manifest["kernelPackage"], "6.12.0-1")
        self.assertEqual(manifest["app"]["asset"], "titanos-0.1.0-beta.1-titand")
        self.assertEqual((self.release / "titanos-0.1.0-beta.1-titand").read_bytes(), b"titand 0.1.0-beta.1")
        for name in ["manifest.json", "titanos-0.1.0-beta.1.zip", "titanos-0.1.0-beta.1-vmlinuz", "titanos-0.1.0-beta.1-titand",
                     "titanos-0.1.0-beta.1-licenses.tar.gz", "titanos-0.1.0-beta.1-sources.txt", "titanos-0.1.0-beta.1-cli-linux-amd64",
                     "nvidia-build-sdk.tar.zst", "nvidia-build-sdk-sources.txt", "nvidia.raw", "nvidia.plugin.json", "nvidia.plugin.json.minisig", "nvidia-kernel-sources.tar.gz", "nvidia-licenses.tar.gz"]:
            with self.subTest(name=name):
                path = self.release / name
                original = path.read_bytes()
                if name.endswith(".minisig"):
                    lines = original.splitlines(); lines[1] = b"invalid signature"; path.write_bytes(b"\n".join(lines))
                else:
                    path.write_bytes(original + b" ")
                with self.assertRaises(ValueError):
                    verify.verify(self.release, self.keys)
                path.write_bytes(original)
        (self.release / "unexpected.txt").write_text("unexpected")
        with self.assertRaisesRegex(ValueError, "inventory"):
            verify.verify(self.release, self.keys)

    def test_multiple_plugins_signed_and_verified(self):
        plugins = self.repo / "out/plugins"
        for name in ["intel-gpu-tools", "coral-usb", "tailscale", "netbird"]:
            data = name.encode()
            (plugins / (name + ".raw")).write_bytes(data)
            (plugins / (name + ".plugin.json")).write_text(json.dumps(dict(
                name=name, version="0.1.0-beta.1", sha256=hashlib.sha256(data).hexdigest(), size=len(data))))
            (plugins / (name + "-licenses.tar.gz")).write_bytes(b"notices")
        self.pack()
        self.sign()
        self.assertEqual(verify.verify(self.release, self.keys), "0.1.0-beta.1")
        self.assertEqual(len(json.loads((self.release / "manifest.json").read_text())["plugins"]), 5)
        (self.release / "intel-gpu-tools.plugin.json.minisig").unlink()
        with self.assertRaisesRegex(ValueError, "trusted key"):
            verify.verify(self.release, self.keys)

    def test_driver_plugin_ships_sources(self):
        plugins = self.repo / "out/plugins"
        data = b"driver image"
        manifest = dict(name="wangxun-nic", version="0.1.0-beta.1", kernel="6.12-test", sha256=hashlib.sha256(data).hexdigest(),
                        size=len(data), modules=["ngbe", "txgbe"], sources=["https://snapshot.debian.org/package/linux/6.12-1/", "wangxun-nic-sources.tar.gz"])
        (plugins / "wangxun-nic.raw").write_bytes(data)
        (plugins / "wangxun-nic.plugin.json").write_text(json.dumps(manifest))
        (plugins / "wangxun-nic-licenses.tar.gz").write_bytes(b"notices")
        # Without the compiled sources the release is refused.
        self.command("mksquashfs", str(self.root), str(self.slot / "filesystem.squashfs"), "-noappend", "-quiet", "-processors", "1")
        self.assertIn("wangxun-nic", self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta", success=False))
        (plugins / "wangxun-nic-sources.tar.gz").write_bytes(b"sources")
        self.pack()
        self.assertTrue((self.release / "wangxun-nic-sources.tar.gz").is_file())
        listed = (self.release / "titanos-0.1.0-beta.1-sources.txt").read_text()
        self.assertIn("Plugin: wangxun-nic 0.1.0-beta.1\n  https://snapshot.debian.org/package/linux/6.12-1/\n  wangxun-nic-sources.tar.gz\n", listed)
        self.sign()
        self.assertEqual(verify.verify(self.release, self.keys), "0.1.0-beta.1")
        # A driver for another kernel is never packed.
        manifest["kernel"] = "6.13-test"
        (plugins / "wangxun-nic.plugin.json").write_text(json.dumps(manifest))
        self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta", success=False)

    def test_wrong_key_rejected(self):
        self.pack()
        self.sign()
        (self.keys / "test.pub").unlink()
        self.command("minisign", "-G", "-W", "-s", str(self.repo / "wrong.key"), "-p", str(self.keys / "wrong.pub"))
        with self.assertRaisesRegex(ValueError, "trusted key"):
            verify.verify(self.release, self.keys)

    def test_image_only_rehearsal_is_not_a_release(self):
        self.command("mksquashfs", str(self.root), str(self.slot / "filesystem.squashfs"), "-noappend", "-quiet", "-processors", "1")
        shutil.rmtree(self.repo / "out/plugins")
        self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta", success=False)
        self.command("env", "WITHOUT_PLUGINS=1", "scripts/make-release.sh", "0.1.0-beta.1", "beta")
        self.sign()
        manifest = json.loads((self.release / "manifest.json").read_text())
        self.assertEqual(manifest["plugins"], [])
        self.assertNotIn("nvidiaBuildSDK", manifest)
        with self.assertRaisesRegex(ValueError, "image-only"):
            verify.verify(self.release, self.keys)
        self.assertEqual(verify.verify(self.release, self.keys, without_plugins=True), "0.1.0-beta.1")

    def test_plugin_for_wrong_kernel_cannot_be_packed(self):
        self.pack()
        path = self.repo / "out/plugins/nvidia.plugin.json"
        plugin = json.loads(path.read_text())
        plugin["kernel"] = "other-kernel"
        path.write_text(json.dumps(plugin))
        output = self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta", success=False)
        self.assertIn("plugin kernel mismatch", output)

    def test_missing_plugin_signature_rejected(self):
        self.pack()
        self.sign()
        (self.release / "nvidia.plugin.json.minisig").unlink()
        with self.assertRaisesRegex(ValueError, "trusted key"):
            verify.verify(self.release, self.keys)

    def test_encrypted_key_and_ci_password_input(self):
        self.pack()
        key = self.repo / "encrypted.key"
        password = "disposable regression test passphrase"
        generated = subprocess.run(["minisign", "-G", "-s", str(key), "-p", str(self.keys / "encrypted.pub")],
                                   input=f"{password}\n{password}\n", text=True, capture_output=True)
        self.assertEqual(generated.returncode, 0, generated.stderr)
        env = dict(os.environ, MINISIGN_PASSWORD=password)
        signed = subprocess.run([str(self.repo / "scripts/sign-release.sh"), str(self.release), str(key)],
                                env=env, text=True, capture_output=True)
        self.assertEqual(signed.returncode, 0, signed.stderr)
        self.assertEqual(verify.verify(self.release, self.keys), "0.1.0-beta.1")

    def test_release_requires_distinct_recovery_key(self):
        with self.assertRaisesRegex(ValueError, "distinct"):
            release_keys.check(self.keys)
        shutil.copyfile(self.keys / "test.pub", self.keys / "duplicate.pub")
        with self.assertRaisesRegex(ValueError, "distinct"):
            release_keys.check(self.keys)
        self.command("minisign", "-G", "-W", "-s", str(self.repo / "recovery.key"), "-p", str(self.keys / "recovery.pub"))
        release_keys.check(self.keys)
        shutil.copyfile(self.keys / "test.pub", self.keys / "rehearsal.pub")
        with self.assertRaisesRegex(ValueError, "rehearsal"):
            release_keys.check(self.keys)

    def test_corrupt_image_rejected(self):
        (self.slot / "filesystem.squashfs").write_bytes(b"not a filesystem")
        self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta", success=False)
        self.assertFalse(self.release.exists())

    def test_dev_autologin_rejected(self):
        autologin = self.root / "etc/systemd/system/getty@tty1.service.d/autologin.conf"
        autologin.parent.mkdir(parents=True)
        autologin.write_text("development console")
        self.command("mksquashfs", str(self.root), str(self.slot / "filesystem.squashfs"), "-noappend", "-quiet", "-processors", "1")
        output = self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta", success=False)
        self.assertIn("development build", output)

    def test_user_configuration_rejected(self):
        self.pack()
        (self.repo / "out/stick/config/account.json").write_text("private")
        output = self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta", success=False)
        self.assertIn("user configuration", output)

    def test_titand_must_be_the_one_in_the_image(self):
        self.command("mksquashfs", str(self.root), str(self.slot / "filesystem.squashfs"), "-noappend", "-quiet", "-processors", "1")
        (self.repo / "backend/bin/titand").write_bytes(b"another titand")
        output = self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta", success=False)
        self.assertIn("not the titand inside the image", output)

    def test_image_must_name_its_base_version(self):
        (self.root / "usr/share/titanos/base-version").write_text("0.0.9\n")
        self.command("mksquashfs", str(self.root), str(self.slot / "filesystem.squashfs"), "-noappend", "-quiet", "-processors", "1")
        output = self.command("scripts/make-release.sh", "0.1.0-beta.1", "beta", success=False)
        self.assertIn("base version", output)

    def test_app_release_on_an_older_image(self):
        # The image of 0.1.0-beta.1 with a newer titand, as repack-release.py makes it.
        (self.root / "usr/bin/titand").write_bytes(b"titand 0.1.0-beta.2")
        (self.repo / "backend/bin/titand").write_bytes(b"titand 0.1.0-beta.2")
        (self.slot / "version").write_text("0.1.0-beta.2\n")
        self.command("mksquashfs", str(self.root), str(self.slot / "filesystem.squashfs"), "-noappend", "-quiet", "-processors", "1")
        self.command("env", "BASE_VERSION=0.1.0-beta.1", "scripts/make-release.sh", "0.1.0-beta.2", "beta")
        self.sign()
        manifest = json.loads((self.release / "manifest.json").read_text())
        self.assertEqual((manifest["version"], manifest["app"]["base"]), ("0.1.0-beta.2", "0.1.0-beta.1"))
        # The plugins and the SDK of the base come along unchanged.
        self.assertEqual(verify.verify(self.release, self.keys), "0.1.0-beta.2")

    def test_repack_an_app_release_from_the_published_base(self):
        # A driver plugin in the base brings its sources along (0.33.0 failed there).
        plugins = self.repo / "out/plugins"
        data = b"driver image"
        (plugins / "wangxun-nic.raw").write_bytes(data)
        (plugins / "wangxun-nic.plugin.json").write_text(json.dumps(dict(
            name="wangxun-nic", version="0.1.0-beta.1", kernel="6.12-test", sha256=hashlib.sha256(data).hexdigest(),
            size=len(data), modules=["ngbe"], sources=["wangxun-nic-sources.tar.gz"])))
        (plugins / "wangxun-nic-licenses.tar.gz").write_bytes(b"notices")
        (plugins / "wangxun-nic-sources.tar.gz").write_bytes(b"sources")
        self.pack()
        self.sign()
        # The base as GitHub serves it.
        published = self.repo / "published/releases/download/v0.1.0-beta.1"
        published.parent.mkdir(parents=True)
        shutil.copytree(self.release, published)
        handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(self.repo / "published"))
        handler.log_message = lambda *args: None
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        for name in ["repack-release.py", "verify-release.py", "nvidia-update-release.py"]:
            shutil.copy2(REPO / "scripts" / name, self.repo / "scripts" / name)
        # Notices without a Go toolchain: Go for the application, Debian from the image.
        stub = self.repo / "licenses-stub.py"
        stub.write_text("""import json, pathlib, sys
mode, path = sys.argv[1], pathlib.Path(sys.argv[2])
if mode == "application":
    (path / "licenses/go").mkdir(parents=True, exist_ok=True)
    (path / "licenses/go/NOTICE").write_text("go notice")
    (path / "components.json").write_text(json.dumps([dict(ecosystem="Go", name="example", version="2", source="https://example.org/go", notices=["go/NOTICE"])]))
else:
    notices = path / "usr/share/titanos"
    components = json.loads((notices / "components.json").read_text())
    components.append(dict(ecosystem="Debian", name="example", version="1", source="https://example.org/source", notices=["NOTICE"]))
    (notices / "components.json").write_text(json.dumps(components))
""")
        (self.repo / "backend/bin/titand").write_bytes(b"titand 0.1.0-beta.2")
        shutil.rmtree(self.repo / "out/stick")
        shutil.rmtree(self.repo / "out/plugins")
        self.command("env", "REPACK_NO_SUDO=1", f"REPACK_LICENSES={stub}", "python3", "scripts/repack-release.py",
                     "--base", "0.1.0-beta.1", "--version", "0.1.0-beta.2",
                     "--source", f"http://127.0.0.1:{server.server_address[1]}", "--keys", str(self.keys))
        self.command("env", "BASE_VERSION=0.1.0-beta.1", "scripts/make-release.sh", "0.1.0-beta.2", "beta")
        self.sign()
        self.assertEqual(verify.verify(self.release, self.keys), "0.1.0-beta.2")
        manifest = json.loads((self.release / "manifest.json").read_text())
        base = json.loads((published / "manifest.json").read_text())
        self.assertEqual(manifest["app"]["base"], "0.1.0-beta.1")
        # Kernel and initrd of the base, bit for bit; the image holds the new titand.
        for name in ["vmlinuz", "initrd.img"]:
            self.assertEqual(next(f["sha256"] for f in manifest["files"] if f["name"] == name),
                             next(f["sha256"] for f in base["files"] if f["name"] == name))
        image = self.release / "titanos-0.1.0-beta.2-filesystem.squashfs"
        cat = lambda path: subprocess.run(["unsquashfs", "-cat", str(image), path], capture_output=True).stdout
        self.assertEqual(cat("usr/bin/titand"), b"titand 0.1.0-beta.2")
        self.assertEqual(cat("usr/share/titanos/base-version"), b"0.1.0-beta.1\n")
        self.assertIn(b"https://example.org/go", cat("usr/share/titanos/components.json"))
        # The plugins of the base came along.
        self.assertEqual(sorted(p["name"] for p in manifest["plugins"]), ["nvidia", "wangxun-nic"])
        self.assertEqual((self.release / "wangxun-nic-sources.tar.gz").read_bytes(), b"sources")
        self.assertEqual(manifest["nvidiaBuildSDK"], base["nvidiaBuildSDK"])

    def test_repack_refuses_a_base_with_a_foreign_signature(self):
        self.pack()
        self.sign()
        published = self.repo / "published/releases/download/v0.1.0-beta.1"
        published.parent.mkdir(parents=True)
        shutil.copytree(self.release, published)
        handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(self.repo / "published"))
        handler.log_message = lambda *args: None
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        for name in ["repack-release.py", "verify-release.py", "nvidia-update-release.py"]:
            shutil.copy2(REPO / "scripts" / name, self.repo / "scripts" / name)
        other = self.repo / "other-keys"
        other.mkdir()
        self.command("minisign", "-G", "-W", "-s", str(self.repo / "other.key"), "-p", str(other / "other.pub"))
        output = self.command("env", "REPACK_NO_SUDO=1", "python3", "scripts/repack-release.py",
                              "--base", "0.1.0-beta.1", "--version", "0.1.0-beta.2",
                              "--source", f"http://127.0.0.1:{server.server_address[1]}", "--keys", str(other), success=False)
        self.assertIn("trusted key", output)

    def test_beta_cannot_be_published_as_stable(self):
        self.pack()
        output = self.command("scripts/make-release.sh", "0.1.0-beta.1", "stable", success=False)
        self.assertIn("channel disagree", output)


if __name__ == "__main__":
    unittest.main()
