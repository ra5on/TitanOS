"""Catalog descriptions of the official plugins and their merge into manifests."""
import json
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
CATALOG = REPO / "plugins/catalog"
CATEGORIES = {"network", "graphics", "sensors", "storage", "tools", "remote"}
OFFICIAL = {"nvidia", "intel-gpu-tools", "coral-usb", "tailscale", "netbird"}


class PluginCatalogTest(unittest.TestCase):
    def setUp(self):
        self.drivers = json.loads((REPO / "plugins/drivers/drivers.json").read_text())

    def test_every_official_plugin_is_described_in_both_languages(self):
        names = {path.stem for path in CATALOG.glob("*.json")}
        self.assertEqual(names, OFFICIAL | set(self.drivers["plugins"]))
        for path in CATALOG.glob("*.json"):
            with self.subTest(plugin=path.stem):
                entry = json.loads(path.read_text())
                self.assertLessEqual(len(path.read_bytes()), 64 * 1024)
                self.assertIn(entry["category"], CATEGORIES)
                self.assertTrue(entry.get("title") and entry.get("license"))
                self.assertTrue(entry["upstream"]["url"].startswith("https://"))
                for language in ("de", "en"):
                    details = entry["details"][language]
                    for field in ("when", "changes", "footprint"):
                        self.assertTrue(details[field].strip(), field)
                    self.assertTrue(details["hardware"] and details["limits"])
                for rule in entry.get("hardware", []):
                    self.assertRegex(rule["modalias"], r"^(pci|usb|dmi\*?):")
                    self.assertLessEqual(len(rule["modalias"]), 512)

    def test_driver_plugins_have_hardware_rules_and_pins(self):
        for name, plugin in self.drivers["plugins"].items():
            with self.subTest(plugin=name):
                entry = json.loads((CATALOG / f"{name}.json").read_text())
                self.assertTrue(entry["hardware"])
                self.assertTrue(set(plugin["modules"]) <= set(plugin["built"]))
                for path in plugin.get("firmware", []) + plugin.get("notices", []):
                    self.assertRegex(self.drivers["firmware"]["files"][path]["sha256"], r"^[0-9a-f]{64}$")
                if "external" in plugin:
                    external = self.drivers["external"][plugin["external"]]
                    self.assertRegex(external["commit"], r"^[0-9a-f]{40}$")
                    self.assertRegex(external["sha256"], r"^[0-9a-f]{64}$")
                    self.assertIn(external["commit"], external["url"])
                if name != "it87-extended":
                    # Drivers: recommended only while no driver has the device.
                    self.assertTrue(all(rule.get("unbound") for rule in entry["hardware"]))

    def test_nvidia_rules_follow_turing_and_newer(self):
        rules = [rule["modalias"] for rule in json.loads((CATALOG / "nvidia.json").read_text())["hardware"]]

        def matches(device):
            alias = f"pci:v000010DEd0000{device}sv00001043sd00008700bc03sc00i00"
            return any(fnmatch(rule, alias) for rule in rules)
        self.assertTrue(matches("1E84"))   # TU104, Turing
        self.assertTrue(matches("2504"))   # GA106, Ampere
        self.assertTrue(matches("2684"))   # AD102, Ada
        self.assertFalse(matches("1B80"))  # GP104, Pascal
        self.assertFalse(matches("1DB4"))  # GV100, Volta

    def test_make_plugin_merges_catalog_under_build_values(self):
        with tempfile.TemporaryDirectory() as tmp:
            stage = Path(tmp) / "stage"
            (stage / "usr/share").mkdir(parents=True)
            (stage / "plugin.json").write_text(json.dumps({"upstream": {"version": "1.2.3"}, "modules": [], "services": []}))
            subprocess.run([str(REPO / "scripts/make-plugin.sh"), "tailscale", "0.1.0", str(stage), tmp],
                           check=True, capture_output=True)
            manifest = json.loads((Path(tmp) / "tailscale.plugin.json").read_text())
            self.assertEqual(manifest["category"], "remote")
            self.assertEqual(manifest["upstream"], {"name": "Tailscale", "url": "https://github.com/tailscale/tailscale", "version": "1.2.3"})
            self.assertEqual(manifest["details"]["en"]["hardware"], ["No special hardware"])
            self.assertEqual(manifest["version"], "0.1.0")


def fnmatch(pattern, name):
    """fnmatch(3) without flags, the way the kernel's modalias patterns are meant."""
    expression = ""
    i = 0
    while i < len(pattern):
        c = pattern[i]
        if c == "*":
            expression += ".*"
        elif c == "?":
            expression += "."
        elif c == "[":
            end = pattern.index("]", i + 2)
            body = pattern[i + 1:end]
            expression += "[" + ("^" + body[1:] if body[:1] in "!^" else body) + "]"
            i = end
        else:
            expression += re.escape(c)
        i += 1
    return re.fullmatch(expression, name, re.S) is not None


if __name__ == "__main__":
    unittest.main()
