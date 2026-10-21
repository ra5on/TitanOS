"""Which tags become app releases."""
import importlib.util
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("release_kind", REPO / "scripts/release-kind.py")
kind = importlib.util.module_from_spec(spec)
spec.loader.exec_module(kind)

RELEASES = {
    "v0.7.2": {"tag": "v0.7.2"},  # before app updates
    "v0.8.0": {"tag": "v0.8.0", "app": {"base": "0.8.0"}},
    "v0.8.1": {"tag": "v0.8.1", "app": {"base": "0.8.0"}},
}
TAGS = ["v0.7.2", "v0.8.0", "v0.8.1", "v0.8.3-beta.1"]


def fetch(_source, tag):
    return RELEASES.get(tag)


class ReleaseKindTest(unittest.TestCase):
    def decide(self, version, changed, message=""):
        return kind.decide(version, message, "https://example", changed_since=lambda tag: changed, fetch=fetch, tags=TAGS)

    def test_only_titand_changed_is_an_app_release_on_the_newest_base(self):
        result = self.decide("0.8.2", ["backend/internal/vm/vm.go", "web/src/routes/vms/+page.svelte", "docs/releases/v0.8.2.md", "README.md"])
        self.assertEqual(result[:2], ("app", "0.8.0"))

    def test_changes_to_the_image_make_a_full_release(self):
        for path in ["build/mkosi.conf", "boot/entry.conf.in", "scripts/make-zip.sh", ".github/workflows/release.yml", "Makefile"]:
            with self.subTest(path=path):
                result = self.decide("0.8.2", ["backend/main.go", path])
                self.assertEqual(result[:2], ("system", "0.8.2"))
                self.assertIn(path, result[2])

    def test_tag_message_can_ask_for_a_full_release(self):
        self.assertEqual(self.decide("0.8.2", ["backend/x.go"], "Debian security updates [system]")[:2], ("system", "0.8.2"))

    def test_first_release_with_app_updates_is_a_full_one(self):
        self.assertEqual(self.decide("0.8.0", ["backend/x.go"])[:2], ("system", "0.8.0"))

    def test_an_unpublished_tag_is_skipped(self):
        # v0.8.3-beta.1 was tagged but never published.
        self.assertEqual(self.decide("0.8.3", ["web/x.ts"])[:2], ("app", "0.8.0"))

    def test_versions_sort_like_semver(self):
        self.assertLess(kind.key("0.8.0-beta.2"), kind.key("0.8.0-beta.10"))
        self.assertLess(kind.key("0.8.0-beta.10"), kind.key("0.8.0"))
        self.assertLess(kind.key("0.8.0"), kind.key("0.8.1"))
        self.assertIsNone(kind.key("latest"))


if __name__ == "__main__":
    unittest.main()
