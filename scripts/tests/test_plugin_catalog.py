"""Catalog descriptions of the official plugins.

The plugins are built in ra5on/TitanOS-Plugins; its tests check the driver
pins against these entries and how make-plugin.sh merges them."""
import json
import re
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
CATALOG = REPO / "plugins/catalog"
CATEGORIES = {"network", "graphics", "sensors", "storage", "tools", "remote"}
OFFICIAL = {"nvidia", "intel-gpu-tools", "coral-usb", "tailscale", "netbird", "wangxun-nic", "intel-qat", "it87-extended"}


class PluginCatalogTest(unittest.TestCase):
    def test_every_official_plugin_is_described_in_both_languages(self):
        names = {path.stem for path in CATALOG.glob("*.json")}
        self.assertEqual(names, OFFICIAL)
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
