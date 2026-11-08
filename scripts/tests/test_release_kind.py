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

    def test_the_example_agent_is_not_part_of_the_image(self):
        result = self.decide("0.8.2", ["backend/x.go", "examples/agent/agent.py", ".github/workflows/agent.yml"])
        self.assertEqual(result[:2], ("app", "0.8.0"))

    def test_the_release_workflow_is_not_part_of_the_image(self):
        result = self.decide("0.8.2", ["web/src/app.css", ".github/workflows/release.yml"])
        self.assertEqual(result[:2], ("app", "0.8.0"))

    def test_the_viewer_image_is_not_part_of_the_system_image(self):
        result = self.decide("0.8.2", ["backend/internal/viewer/manager.go", "viewer/Dockerfile", "viewer/policies.json", "scripts/build-viewer-image.sh"])
        self.assertEqual(result[:2], ("app", "0.8.0"))
        # A build file of the image still makes a full release.
        self.assertEqual(self.decide("0.8.2", ["viewer/Dockerfile", "build/mkosi.conf"])[:2], ("system", "0.8.2"))

    def test_the_web_apps_are_not_part_of_the_system_image(self):
        changed = ["backend/internal/webapps/manager.go", "webapps/catalog.json", "scripts/fetch-webapps.py",
                   "scripts/tests/test_fetch_webapps.py", "scripts/make-release.sh", "THIRD_PARTY_LICENSES"]
        self.assertEqual(self.decide("0.8.2", changed)[:2], ("app", "0.8.0"))

    def test_the_cleanup_of_the_repository_is_no_system_change(self):
        changed = ["backend/x.go", ".gitignore",
                   "shellcheck_py-0.11.0.1-py2.py3-none-manylinux1_x86_64.manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_5_x86_64.whl"]
        self.assertEqual(self.decide("0.8.2", changed)[:2], ("app", "0.8.0"))
        # The checks of the QEMU storage test run in CI only.
        self.assertEqual(self.decide("0.8.2", ["scripts/workflows023_smoke.py"])[:2], ("app", "0.8.0"))
        # The README pictures are taken against vite dev, not in the image.
        self.assertEqual(self.decide("0.8.2", ["scripts/capture-readme-screenshots.mjs"])[:2], ("app", "0.8.0"))
        # Other files at the top stay a full release.
        self.assertEqual(self.decide("0.8.2", ["backend/x.go", "Makefile"])[:2], ("system", "0.8.2"))

    def test_changes_to_the_image_make_a_full_release(self):
        for path in ["build/mkosi.conf", "boot/entry.conf.in", "scripts/make-zip.sh", ".github/actions/image-tools/action.yml", "Makefile"]:
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
