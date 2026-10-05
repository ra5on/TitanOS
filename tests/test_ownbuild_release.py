import importlib.util
import json
import os
from pathlib import Path
import unittest
from unittest.mock import patch

from titan import updates
import test_debian_release_evidence as previous


class OwnReleaseTests(unittest.TestCase):
    def setUp(self):
        fixture = previous.EvidenceTests('test_complete_checks_allow_signing')
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        self.ab, self.root, self.save = fixture.ab, fixture.root, fixture.save

    def bootstrap(self):
        self.ab.update(baseline_source='fresh-install-bootstrap', baseline_version='2.9.99')
        self.ab['checks'] = [name for name in self.ab['checks'] if name not in ('published_release_baseline','fresh_install_bootstrap_baseline')]
        self.ab['checks'].append('fresh_install_bootstrap_baseline')
        self.save()

    def test_bootstrap_requires_draft_and_complete_ab_recovery(self):
        self.bootstrap()
        spec=importlib.util.spec_from_file_location('own_evidence',Path(__file__).resolve().parents[1]/'scripts/validate-system-evidence.py')
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        with patch.dict(os.environ,{'TITAN_INITIAL_RELEASE':'true','TITAN_DRAFT_RELEASE':'true'}):
            self.assertEqual(set(module.validate(self.root).values()),{'passed'})
            self.ab['checks'].remove('failed_candidate_fallback_after_reset');self.save()
            with self.assertRaises(ValueError):module.validate(self.root)
        self.bootstrap()
        with patch.dict(os.environ,{'TITAN_INITIAL_RELEASE':'true','TITAN_DRAFT_RELEASE':'false'}):
            with self.assertRaises(ValueError):module.validate(self.root)

    def test_titan_release_tag_is_numeric_identity_alias(self):
        self.assertEqual(updates.version('titan-3.0.0'),updates.version('3.0.0'))
        with self.assertRaises(updates.Error):updates.version('build-titan-3.0.0')

    def test_checksum_parser_rejects_duplicates_and_paths(self):
        spec=importlib.util.spec_from_file_location('baseline',Path(__file__).resolve().parents[1]/'scripts/fetch-debian-baseline.py')
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        line='a'*64+'  titan-3.0.0-amd64.img.xz'
        self.assertEqual(module.checksums(line.encode()),{'titan-3.0.0-amd64.img.xz':'a'*64})
        for value in (line+'\n'+line,'a'*64+'  ../unsafe.img.xz','invalid'):
            with self.assertRaises(ValueError):module.checksums(value.encode())
