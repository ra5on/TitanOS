"""The weekly comparison with trixie-security."""
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


if __name__ == "__main__":
    unittest.main()
