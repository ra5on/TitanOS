"""plugin-sign.yml: loading the current catalog before a set goes out (B-8).

Only a catalog that does not exist yet may start at serial 0; any other
failure to load it stops the run before the set is published. The shell
function load_catalog is taken from the workflow and run against a fake gh.
"""
import os
import re
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
WORKFLOW = REPO / ".github/workflows/plugin-sign.yml"

# A gh that answers from FAKE_* variables: the release view (or an error
# line with its HTTP status) and the files a download gives.
FAKE_GH = r"""#!/bin/bash
set -u
case "$1 $2" in
  "api repos/"*"/releases/tags/catalog")
    if [ -n "${FAKE_VIEW_ERROR:-}" ]; then echo "$FAKE_VIEW_ERROR" >&2; exit 1; fi
    echo "$FAKE_VIEW"
    ;;
  "release download")
    dir=
    while [ $# -gt 0 ]; do [ "$1" = -D ] && dir=$2; shift; done
    if [ -n "${FAKE_DOWNLOAD_ERROR:-}" ]; then echo "$FAKE_DOWNLOAD_ERROR" >&2; exit 1; fi
    mkdir -p "$dir"
    for name in $FAKE_FILES; do echo "content of $name" > "$dir/$name"; done
    ;;
  *) echo "unexpected gh $*" >&2; exit 2 ;;
esac
"""


def function(text, name):
    """The shell function name() { … } of a run block, dedented."""
    lines = text.splitlines()
    for i, line in enumerate(lines):
        match = re.match(r"^(\s*)" + re.escape(name) + r"\(\) \{$", line)
        if match:
            indent = match.group(1)
            for j in range(i + 1, len(lines)):
                if lines[j] == indent + "}":
                    return textwrap.dedent("\n".join(lines[i:j + 1]))
    raise AssertionError(f"{name}() not found in {WORKFLOW.name}")


class LoadCatalogTest(unittest.TestCase):
    def setUp(self):
        self.text = WORKFLOW.read_text()
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp)
        bin_dir = self.tmp / "bin"
        bin_dir.mkdir()
        gh = bin_dir / "gh"
        gh.write_text(FAKE_GH)
        gh.chmod(0o755)
        self.path = f"{bin_dir}:{os.environ['PATH']}"

    def load(self, view='{"assets":[]}', view_error="", files="", download_error=""):
        script = function(self.text, "load_catalog") + '\nset -e\nif load_catalog; then echo "current=$current"; else echo failed; fi\n'
        env = dict(os.environ, PATH=self.path, PLUGINS="ra5on/TitanOS-Plugins", FAKE_VIEW=view,
                   FAKE_VIEW_ERROR=view_error, FAKE_FILES=files, FAKE_DOWNLOAD_ERROR=download_error)
        result = subprocess.run(["bash", "-c", script], cwd=self.tmp, env=env, capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout.strip().splitlines()[-1]

    def test_a_missing_release_starts_a_new_catalog(self):
        self.assertEqual(self.load(view_error="gh: Not Found (HTTP 404)"), "current=-")

    def test_an_empty_release_starts_a_new_catalog(self):
        # Created by a run whose upload failed: nothing was ever published.
        self.assertEqual(self.load(view='{"assets":[]}'), "current=-")

    def test_the_current_catalog_is_loaded(self):
        view = '{"assets":[{"name":"catalog.json"},{"name":"catalog.json.minisig"}]}'
        self.assertEqual(self.load(view=view, files="catalog.json catalog.json.minisig"), "current=catalog-current")
        self.assertTrue((self.tmp / "catalog-current/catalog.json").is_file())

    def test_a_failed_look_at_the_release_stops(self):
        for error in ("gh: Server Error (HTTP 502)", "gh: Bad credentials (HTTP 401)", "dial tcp: i/o timeout"):
            with self.subTest(error=error):
                self.assertEqual(self.load(view_error=error), "failed")

    def test_a_failed_download_stops(self):
        view = '{"assets":[{"name":"catalog.json"},{"name":"catalog.json.minisig"}]}'
        self.assertEqual(self.load(view=view, download_error="HTTP 502: Bad Gateway"), "failed")

    def test_a_release_without_the_catalog_file_stops(self):
        view = '{"assets":[{"name":"catalog.json.minisig"}]}'
        self.assertEqual(self.load(view=view, files="catalog.json.minisig"), "failed")

    def test_the_catalog_is_loaded_before_the_set_is_published(self):
        # A later run only signs drafts: a set made public before its catalog
        # entry failed would never get one.
        loop = self.text[self.text.index('for tag in $(jq -r "$DRAFTS'):]
        self.assertLess(loop.index("if ! load_catalog; then"), loop.index("python3 scripts/sign-plugin-set.py set"))
        self.assertLess(loop.index("if ! load_catalog; then"), loop.index("-F draft=false"))
        pointer = loop[loop.index("sign-plugin-set.py pointer"):].splitlines()[0]
        self.assertIn('"$current"', pointer)
        self.assertNotIn("gh release download catalog", loop[loop.index("-F draft=false"):])


if __name__ == "__main__":
    unittest.main()
