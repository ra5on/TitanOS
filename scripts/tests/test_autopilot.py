"""Which tag the autopilot cuts, and what it may publish without the owner."""
import importlib.util
import os
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

    def test_no_autopilot_on_a_pre_release(self):
        self.assertEqual(autopilot.plan("0.46.0-beta.1", [], published(), NOW)[0], "wait")


class PendingTest(unittest.TestCase):
    def test_newest_autopilot_tag_without_release(self):
        tags = [("v0.45.2", NOW, MARK), ("v0.45.3", NOW, MARK), ("v0.46.0", NOW, "owner")]
        self.assertEqual(autopilot.pending(tags, published("v0.45.2")), "v0.45.3")
        self.assertEqual(autopilot.pending(tags, published("v0.45.2", "v0.45.3")), "")
        self.assertEqual(autopilot.pending([("v0.46.0", NOW, "owner")], published()), "")


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
