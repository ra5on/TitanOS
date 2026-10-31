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
OFFICIAL = {"nvidia", "nvidia-legacy", "intel-gpu-tools", "coral-usb", "tailscale", "netbird", "wangxun-nic", "intel-qat",
            "it87-extended", "r8125", "r8126"}


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

    def test_nvidia_legacy_rules_cover_maxwell_to_volta_only(self):
        legacy = [rule["modalias"] for rule in json.loads((CATALOG / "nvidia-legacy.json").read_text())["hardware"]]
        open_rules = [rule["modalias"] for rule in json.loads((CATALOG / "nvidia.json").read_text())["hardware"]]

        def matches(rules, device, klass="03"):
            alias = f"pci:v000010DEd0000{device}sv00001043sd00008700bc{klass}sc00i00"
            return any(fnmatch(rule, alias) for rule in rules)
        for device in ("1380", "13C2", "1401", "17C2", "1B80", "1B06", "1C82", "1D01", "1D81", "1DB4"):
            self.assertTrue(matches(legacy, device), device)    # GM107 … GV100
        for device in ("1180", "1004", "1E84", "2504", "2684"):
            self.assertFalse(matches(legacy, device), device)   # Kepler, Turing and newer
        self.assertFalse(matches(legacy, "1B80", "04"))          # the card's audio function
        # Every NVIDIA display ID belongs to at most one of the two plugins.
        for value in range(0x1000, 0x3000):
            device = f"{value:04X}"
            self.assertFalse(matches(legacy, device) and matches(open_rules, device), device)

    def test_realtek_vendor_drivers_are_never_suggested_over_r8169(self):
        # r8169 binds these chips at boot; "unbound" rules show the devices on
        # the plugin page without recommending the plugin to every owner.
        for name, ids in (("r8125", {"8125", "3000"}), ("r8126", {"8126"})):
            rules = json.loads((CATALOG / f"{name}.json").read_text())["hardware"]
            self.assertTrue(all(rule["unbound"] for rule in rules))
            self.assertEqual({rule["modalias"] for rule in rules},
                             {f"pci:v000010ECd0000{i}sv*sd*bc*sc*i*" for i in ids})


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
