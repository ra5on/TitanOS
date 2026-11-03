"""The nightly comparison with Debian."""
import importlib.util
import shutil
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("debian_security", REPO / "scripts/debian-security.py")
security = importlib.util.module_from_spec(spec)
spec.loader.exec_module(security)


@unittest.skipUnless(shutil.which("dpkg"), "dpkg compares Debian versions")
class DebianSecurityTest(unittest.TestCase):
    def test_newer_packages_in_trixie_security(self):
        sources = """TitanOS 0.8.0: exact third-party package sources.

Debian: openssl 3.5.1-1
  https://snapshot.debian.org/package/openssl/3.5.1-1/
Debian: bash 5.2.37-2+b5
  https://snapshot.debian.org/package/bash/5.2.37-2/
Debian: libssl3t64 3.5.1-1
  https://snapshot.debian.org/package/openssl/3.5.1-1/
Go: golang.org/x/sys v0.30.0
  https://proxy.golang.org/golang.org/x/sys/@v/v0.30.0.zip
"""
        index = """Package: openssl
Version: 3.5.1-1+deb13u1

Package: libssl3t64
Version: 3.5.1-1+deb13u1

Package: bash
Version: 5.2.37-2

Package: unrelated
Version: 9
"""
        installed = security.image_packages(sources)
        self.assertEqual(installed, {"openssl": "3.5.1-1", "bash": "5.2.37-2+b5", "libssl3t64": "3.5.1-1"})
        found = security.updates(installed, security.security_packages(index))
        self.assertEqual(found, [("libssl3t64", "3.5.1-1", "3.5.1-1+deb13u1"), ("openssl", "3.5.1-1", "3.5.1-1+deb13u1")])

    def test_the_image_with_the_newer_kernel_is_its_own_section(self):
        # Shaped like titanos-0.45.1-sources.txt: the backports kernel must
        # not overwrite the version of the default image.
        sources = """Debian: linux-image-amd64 6.12.113-1
  https://snapshot.debian.org/package/linux-signed-amd64/6.12.113-1/
Debian: openssl 3.5.1-1
  https://snapshot.debian.org/package/openssl/3.5.1-1/
Plugin: nvidia 580
  https://example

Image with the new kernel (titanos-0.45.1-new-*), additionally:
Debian: linux-image-amd64 7.2.11-1~bpo13+1
  https://snapshot.debian.org/package/linux-signed-amd64/7.2.11-1~bpo13+1/
"""
        base, variant = security.image_sections(sources)
        self.assertEqual(base, {"linux-image-amd64": "6.12.113-1", "openssl": "3.5.1-1"})
        self.assertEqual(variant, {"linux-image-amd64": "7.2.11-1~bpo13+1"})
        self.assertEqual(security.image_packages(sources), base)
        # A point release in trixie brings the kernel of the default image;
        # trixie-backports the one of the image with the newer kernel.
        trixie = {"linux-image-amd64": "6.12.115-2", "openssl": "3.5.1-1"}
        backports = {"linux-image-amd64": "7.2.13-1~bpo13+1"}
        self.assertEqual(security.updates(base, security.merge(trixie, {"openssl": "3.5.0-1"})),
                         [("linux-image-amd64", "6.12.113-1", "6.12.115-2")])
        self.assertEqual(security.updates(variant, security.merge(trixie, backports)),
                         [("linux-image-amd64", "7.2.11-1~bpo13+1", "7.2.13-1~bpo13+1")])

    def test_suites_merge_newest_over_all(self):
        urls = []

        def load(url):
            urls.append(url)
            return {"bash": "5.2.37-2+deb13u1"} if "security" in url else {"bash": "5.2.37-2"}
        self.assertEqual(security.suites(security.BASE_SUITES, load), {"bash": "5.2.37-2+deb13u1"})
        self.assertIn("https://deb.debian.org/debian/dists/trixie-updates/main/binary-amd64/Packages.xz", urls)
        self.assertIn("https://security.debian.org/debian-security/dists/trixie-security/non-free-firmware/binary-amd64/Packages.xz", urls)
        self.assertFalse(any("backports" in url for url in urls))
        self.assertTrue(any("trixie-backports" in url for url in [t.format(suite=s, component="main") for t, s in security.VARIANT_SUITES]))


if __name__ == "__main__":
    unittest.main()
