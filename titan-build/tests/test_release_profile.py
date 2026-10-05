import importlib.util
from pathlib import Path
import unittest

HELPER = Path(__file__).resolve().parents[1] / 'release-profile.py'
SPEC = importlib.util.spec_from_file_location('titan_release_profile', HELPER)
PROFILE = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(PROFILE)


class StableReleaseProfiles(unittest.TestCase):
    def setUp(self):
        self.canonical = {'version':'2.0.0','osVersion':'2.0.0','versionName':'TitanOS 2.0.0','stage':'stable','nested':{'unchanged':True}}

    def test_canonical_identity_is_clean_and_original_is_not_mutated(self):
        selected = PROFILE.metadata_for(self.canonical, 'canonical')
        self.assertEqual(selected, self.canonical)
        selected['nested']['unchanged'] = False
        self.assertTrue(self.canonical['nested']['unchanged'])

    def test_bridge_is_truthfully_versioned_stable_and_returns_to_clean_canonical(self):
        selected = PROFILE.metadata_for(self.canonical, 'legacy-bridge')
        self.assertEqual((selected['version'],selected['osVersion'],selected['stage']), ('2.0.0-titan.3','2.0.0-titan.3','stable'))
        self.assertEqual(selected['legacyUpdateBridgeTo'], '2.0.0')
        self.assertNotIn('legacyUpdateBridgeTo', PROFILE.metadata_for(self.canonical, 'canonical'))

    def test_experimental_versions_stages_and_unrecognized_profiles_are_rejected(self):
        for changes in ({'version':'2.0.0-titan.2'}, {'stage':'alpha'}, {'stage':'beta'}, {'osVersion':'2.0.1'}):
            with self.subTest(changes=changes), self.assertRaises(ValueError): PROFILE.metadata_for({**self.canonical,**changes}, 'canonical')
        with self.assertRaises(ValueError): PROFILE.metadata_for(self.canonical, 'alpha')
        with self.assertRaises(ValueError): PROFILE.metadata_for({**self.canonical,'version':'2.0.1','osVersion':'2.0.1'}, 'legacy-bridge')
