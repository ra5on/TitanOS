"""The image build uses the current complete checkout, never an imported feed."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

BUILD = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BUILD))
from release_identity import COMPATIBILITY, validate_release
SPEC = importlib.util.spec_from_file_location('titan_verify_source', BUILD / 'verify-source.py')
SOURCE = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(SOURCE)


def release():
    return {'version': '2.0.1', 'osVersion': '2.0.1', 'versionName': 'TitanOS 2.0.1',
            'stage': 'stable', 'architecture': 'amd64', 'systemCompatibility': COMPATIBILITY,
            'upstreamCommit': 'a' * 40, 'upstreamTag': '2.0.0', 'upstreamArchiveSha256': 'b' * 64}


class StableIdentityTests(unittest.TestCase):
    def test_clean_numeric_version_is_supported_without_mutating_metadata(self):
        metadata = release(); before = json.dumps(metadata)
        self.assertEqual(validate_release(metadata), '2.0.1')
        self.assertEqual(json.dumps(metadata), before)

    def test_experimental_versions_old_layout_and_mismatched_identity_are_rejected(self):
        attacks = [{'version': '2.0.0-titan.3'}, {'version': '2.0.1-alpha.1'}, {'version': '2.0.1+build'},
                   {'version': '02.0.1'}, {'stage': 'alpha'}, {'stage': 'beta'}, {'architecture': 'arm64'},
                   {'osVersion': '2.0.0'}, {'versionName': 'Other OS'}, {'legacyUpdateBridgeTo': '2.0.0'},
                   {'systemCompatibility': 'titan-titan-rugix-amd64-v1'}]
        for changes in attacks:
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                validate_release({**release(), **changes})


@unittest.skipUnless(shutil.which('git'), 'git required')
class CurrentCheckoutTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(); self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        for name in SOURCE.REQUIRED_SOURCE_FILES:
            path = self.root / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_text('Source fixture')
        (self.root / '.titan/release.json').write_text(json.dumps(release()))
        (self.root / '.titan/imported-source.json').write_text(json.dumps({
            'upstreamCommit': 'a' * 40, 'upstreamTag': '2.0.0', 'archiveSha256': 'b' * 64}))
        self.git('init'); self.git('add', '.')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-m', 'Complete source fixture')

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.root), *args], stderr=subprocess.DEVNULL)

    def verify(self):
        with patch.object(SOURCE, 'MINIMUM_SOURCE_FILES', len(SOURCE.REQUIRED_SOURCE_FILES)), \
                patch.dict(os.environ, {'GITHUB_REPOSITORY': 'ra5on/TitanOS'}), contextlib.redirect_stdout(io.StringIO()):
            SOURCE.verify_source(self.root)

    def test_current_tracked_source_is_checked_without_network_writes_or_commits(self):
        (self.root / 'private-untracked-key.pem').write_text('Private fixture')
        before = (self.git('rev-parse', 'HEAD'), self.git('status', '--porcelain'))
        actual_run = subprocess.run
        def read_only_git(args, **kwargs):
            self.assertEqual(args, ['git', '-C', str(self.root), 'ls-files', '-z'])
            return actual_run(args, **kwargs)
        with patch.object(SOURCE.subprocess, 'run', side_effect=read_only_git):
            self.verify(); self.verify()
        self.assertEqual((self.git('rev-parse', 'HEAD'), self.git('status', '--porcelain')), before)

    def test_incomplete_untracked_missing_or_empty_required_source_is_rejected(self):
        path = self.root / 'packages/os/titanos.Dockerfile'; contents = path.read_bytes()
        path.unlink()
        with self.assertRaises(ValueError): self.verify()
        path.write_bytes(b'')
        with self.assertRaises(ValueError): self.verify()
        path.write_bytes(contents); self.git('rm', '--cached', str(path.relative_to(self.root)))
        with self.assertRaises(ValueError): self.verify()

    def test_wrong_repository_or_provenance_is_rejected(self):
        with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'ra5on/Titan'}), self.assertRaises(ValueError):
            SOURCE.verify_source(self.root)
        (self.root / '.titan/imported-source.json').write_text('{}')
        with self.assertRaises(ValueError): self.verify()

    def test_full_source_file_count_guard_is_not_disabled(self):
        with self.assertRaisesRegex(ValueError, 'complete TitanOS source'):
            SOURCE.verify_source(self.root)
