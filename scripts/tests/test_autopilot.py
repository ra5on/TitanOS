"""Which tag the autopilot cuts, and what it may publish without the owner."""
import importlib.util
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("autopilot", REPO / "scripts/autopilot.py")
autopilot = importlib.util.module_from_spec(spec)
spec.loader.exec_module(autopilot)

DAY = 24 * 3600
NOW = 1_800_000_000
MARK = "Sicherheitsupdate (Autopilot)"


def published(*tags):
    return lambda tag: tag in tags


class PlanTest(unittest.TestCase):
    def test_next_free_patch_after_the_latest_release(self):
        tags = [("v0.45.0", NOW - 9 * DAY, "x"), ("v0.45.1", NOW - 5 * DAY, "x")]
        self.assertEqual(autopilot.plan("0.45.1", tags, published("v0.45.0", "v0.45.1"), NOW)[:2], ("tag", "v0.45.2"))

    def test_existing_tags_are_skipped_even_unpublished_old_ones(self):
        # v0.45.2 was tagged long ago and never published, a pre-release too.
        tags = [("v0.45.1", NOW - 9 * DAY, "x"), ("v0.45.2", NOW - 9 * DAY, "x"), ("v0.45.3-beta.1", NOW - 8 * DAY, "x")]
        self.assertEqual(autopilot.plan("0.45.1", tags, published("v0.45.1"), NOW)[:2], ("tag", "v0.45.4"))

    def test_waits_for_its_own_unpublished_tag(self):
        # Building, waiting for plugin sets, or failed: never a second tag.
        tags = [("v0.45.1", NOW - 9 * DAY, "x"), ("v0.45.2", NOW - 20 * DAY, MARK)]
        self.assertEqual(autopilot.plan("0.45.1", tags, published("v0.45.1"), NOW)[0], "wait")

    def test_waits_for_a_fresh_tag_of_the_owner(self):
        tags = [("v0.45.1", NOW - 9 * DAY, "x"), ("v0.46.1", NOW - DAY, "Ein volles Systemupdate")]
        self.assertEqual(autopilot.plan("0.45.1", tags, published("v0.45.1"), NOW)[0], "wait")

    def test_an_old_unpublished_tag_of_the_owner_does_not_block(self):
        # Like v0.46.0, which was tagged and then dropped.
        tags = [("v0.45.1", NOW - 9 * DAY, "x"), ("v0.46.0", NOW - 4 * DAY, "NVR")]
        self.assertEqual(autopilot.plan("0.45.1", tags, published("v0.45.1"), NOW)[:2], ("tag", "v0.45.2"))

    def test_published_later_tags_do_not_block(self):
        tags = [("v0.45.1", NOW - 9 * DAY, "x"), ("v0.45.2", NOW - DAY, MARK)]
        self.assertEqual(autopilot.plan("0.45.2", tags, published("v0.45.1", "v0.45.2"), NOW)[:2], ("tag", "v0.45.3"))

    def test_from_0_72_the_second_number_rises(self):
        # PLAN 76 (F-13-17): no 0.74.1; a taken number is skipped.
        self.assertEqual(autopilot.plan("0.74.0", [("v0.74.0", NOW - 10 * 86400, "0.74")], published("v0.74.0"), NOW)[:2], ("tag", "v0.75.0"))
        tags = [("v0.74.0", NOW - 10 * 86400, "0.74"), ("v0.75.0", NOW - 10 * 86400, "never released")]
        self.assertEqual(autopilot.plan("0.74.0", tags, published("v0.74.0"), NOW)[:2], ("tag", "v0.76.0"))

    def test_no_autopilot_on_a_pre_release(self):
        self.assertEqual(autopilot.plan("0.46.0-beta.1", [], published(), NOW)[0], "wait")


class PendingTest(unittest.TestCase):
    def test_newest_autopilot_tag_without_release(self):
        tags = [("v0.45.2", NOW, MARK), ("v0.45.3", NOW, MARK), ("v0.46.0", NOW, "owner")]
        self.assertEqual(autopilot.pending(tags, published("v0.45.2")), "v0.45.3")
        self.assertEqual(autopilot.pending(tags, published("v0.45.2", "v0.45.3")), "")
        self.assertEqual(autopilot.pending([("v0.46.0", NOW, "owner")], published()), "")
        # PLAN 76 (F-13-1): an autopilot tag that never came out and was
        # overtaken by a published release is not pending for ever.
        stale = [*tags, ("v0.46.0", NOW, "0.46"), ("v0.47.0", NOW, "0.47")]
        self.assertEqual(autopilot.pending(stale, published("v0.45.2", "v0.46.0")), "")
        self.assertEqual(autopilot.pending(stale, published("v0.45.2")), "v0.45.3")


class GuardTest(unittest.TestCase):
    def guard(self, version="0.45.2", parents=("a",), tags=("v0.45.1",), changed=("build/debian-security.txt",), pub=("v0.45.1",)):
        return autopilot.guard(version, list(parents), list(tags), list(changed), published(*pub))[0]

    def test_a_patch_that_changes_the_list_alone_is_allowed(self):
        self.assertTrue(self.guard())

    def test_anything_else_needs_the_owner(self):
        self.assertFalse(self.guard(changed=("build/debian-security.txt", "backend/main.go")))
        self.assertFalse(self.guard(changed=("build/mkosi.conf",)))
        self.assertFalse(self.guard(changed=()))
        self.assertFalse(self.guard(version="0.46.0"))  # not a patch of 0.45.1
        self.assertFalse(self.guard(version="0.45.1"))  # not later
        self.assertFalse(self.guard(version="0.45.2-beta.1"))
        self.assertFalse(self.guard(pub=()))  # the parent was never published
        self.assertFalse(self.guard(tags=()))  # the parent is no release at all
        self.assertFalse(self.guard(parents=("a", "b")))  # a merge

    def test_guard_from_0_72_wants_the_next_minor_version(self):
        ok = dict(tags=("v0.74.0",), pub=("v0.74.0",))
        self.assertTrue(self.guard(version="0.75.0", **ok))
        self.assertTrue(self.guard(version="0.76.0", **ok))  # 0.75.0 was taken
        self.assertFalse(self.guard(version="0.74.1", **ok))  # no third number any more
        self.assertFalse(self.guard(version="0.75.1", **ok))
        self.assertFalse(self.guard(version="1.0.0", **ok))  # another major version
        self.assertFalse(self.guard(version="0.74.0", **ok))  # not later
        self.assertFalse(self.guard(version="0.75.0", tags=("v0.74.0",), pub=()))
        self.assertFalse(self.guard(version="0.75.0", changed=("backend/main.go",), **ok))
        # The last three-part release may be followed by a two-part one.
        self.assertTrue(self.guard(version="0.72.0", tags=("v0.71.1",), pub=("v0.71.1",)))


class VerifyTest(unittest.TestCase):
    LIST = """Debian updates for the image of TitanOS 0.45.1 (latest release 0.45.1).
A full release (not an app release) brings them.
Checked on 2026-11-03.

linux-image-amd64: 6.12.113-1 -> 6.12.115-2
openssl: 3.5.1-1 -> 3.5.1-1+deb13u1

Image with the newer kernel (trixie-backports):
linux-image-amd64: 7.2.11-1~bpo13+1 -> 7.2.13-1~bpo13+1
"""

    def test_sections_of_the_list(self):
        base, variant = autopilot.listed(self.LIST)
        self.assertEqual(base, [("linux-image-amd64", "6.12.115-2"), ("openssl", "3.5.1-1+deb13u1")])
        self.assertEqual(variant, [("linux-image-amd64", "7.2.13-1~bpo13+1")])

    @unittest.skipUnless(shutil.which("dpkg"), "dpkg compares Debian versions")
    def test_an_image_without_the_update_fails(self):
        base, _ = autopilot.listed(self.LIST)
        image = [{"ecosystem": "Debian", "name": "linux-image-amd64", "version": "6.12.113-1"},
                 {"ecosystem": "Debian", "name": "openssl", "version": "3.5.1-1+deb13u2"}]
        self.assertEqual(autopilot.missing(base, image, autopilot.dpkg_at_least),
                         [("linux-image-amd64", "6.12.115-2", "6.12.113-1")])
        image[0]["version"] = "6.12.115-2"
        self.assertEqual(autopilot.missing(base, image, autopilot.dpkg_at_least), [])
        self.assertEqual(autopilot.missing(base, image[:1], autopilot.dpkg_at_least),
                         [("openssl", "3.5.1-1+deb13u1", None)])


class GitTest(unittest.TestCase):
    """The commands of plan, pending and guard on a real repository."""

    def git(self, *args):
        subprocess.run(["git", *args], cwd=self.dir, check=True, capture_output=True,
                       env={**os.environ, "GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t"})

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = self.tmp.name
        self.git("init", "-q")
        Path(self.dir, "build").mkdir()
        Path(self.dir, "build/mkosi.conf").write_text("x\n")
        self.git("add", ".")
        self.git("commit", "-qm", "release")
        self.git("tag", "-a", "v0.45.1", "-m", "Ein volles Systemupdate")
        Path(self.dir, "build/debian-security.txt").write_text("linux-image-amd64: 1 -> 2\n")
        self.git("add", ".")
        self.git("commit", "-qm", "security")
        self.git("tag", "-a", "v0.45.2", "-m", MARK + "\n\nbody")
        self.cwd = os.getcwd()
        os.chdir(self.dir)

    def tearDown(self):
        os.chdir(self.cwd)
        self.tmp.cleanup()

    def test_tags_and_their_subjects(self):
        tags = {name: subject for name, _, subject in autopilot.local_tags()}
        self.assertEqual(tags, {"v0.45.1": "Ein volles Systemupdate", "v0.45.2": MARK})
        self.assertEqual(autopilot.pending(autopilot.local_tags(), published("v0.45.1")), "v0.45.2")

    def test_guard_reads_parent_tag_and_change(self):
        parents = autopilot.git("rev-list", "--parents", "-n", "1", "HEAD").split()[1:]
        tags = autopilot.git("tag", "--points-at", parents[0]).split()
        changed = autopilot.git("diff", "--name-only", "HEAD^", "HEAD").split()
        self.assertEqual((tags, changed), (["v0.45.1"], ["build/debian-security.txt"]))
        self.assertTrue(autopilot.guard("0.45.2", parents, tags, changed, published("v0.45.1"))[0])


if __name__ == "__main__":
    unittest.main()
