"""Release preflight skips existing images and fails closed on uncertain API state."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
import urllib.error
from unittest.mock import Mock, patch

BUILD = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(BUILD))
from release_identity import COMPATIBILITY

SPEC = importlib.util.spec_from_file_location('titan_release_needed', BUILD / 'release-needed.py')
GUARD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GUARD)


def release():
    return {'version': '2.0.1', 'osVersion': '2.0.1', 'versionName': 'TitanOS 2.0.1',
            'stage': 'stable', 'architecture': 'amd64', 'systemCompatibility': COMPATIBILITY}


class Response(io.BytesIO):
    def __init__(self, contents, status=200):
        super().__init__(contents)
        self.status = status


class ReleasePreflightTests(unittest.TestCase):
    def check(self, result=None, *, error=None, body=None, status=200, token=''):
        contents = json.dumps(result).encode() if body is None else body
        opener = Mock()
        if error is None:
            opener.open.return_value = Response(contents, status)
        else:
            opener.open.side_effect = error
        with patch.object(GUARD.urllib.request, 'build_opener', return_value=opener):
            needed = GUARD.release_needed(release(), token)
        return needed, opener.open.call_args.args[0]

    def test_published_exact_tag_skips_rebuild_with_only_a_read_request(self):
        needed, request = self.check({'tag_name': 'v2.0.1', 'draft': False})
        self.assertFalse(needed)
        self.assertEqual(request.get_method(), 'GET')
        self.assertEqual(request.full_url,
                         'https://api.github.com/repos/ra5on/TitanOS/releases/tags/v2.0.1')
        self.assertIsNone(request.get_header('Authorization'))

    def test_draft_allows_build_and_token_is_used_for_api_authentication(self):
        needed, request = self.check({'tag_name': 'v2.0.1', 'draft': True}, token='fixture-token')
        self.assertTrue(needed)
        self.assertEqual(request.get_header('Authorization'), 'Bearer fixture-token')

    def test_published_prerelease_is_also_protected_from_replacement(self):
        needed, _ = self.check({'tag_name': 'v2.0.1', 'draft': False, 'prerelease': True})
        self.assertFalse(needed)

    def test_only_http_404_allows_a_missing_release_build(self):
        error = urllib.error.HTTPError('https://api.github.com', 404, 'Not Found', {}, None)
        needed, _ = self.check(error=error)
        self.assertTrue(needed)

    def test_other_http_errors_never_allow_a_build(self):
        for status in (301, 302, 401, 403, 429, 500, 503):
            with self.subTest(status=status), self.assertRaises(GUARD.ReleaseCheckError):
                self.check(error=urllib.error.HTTPError(
                    'https://api.github.com', status, 'Fixture error', {}, None))

    def test_unexpected_success_status_is_rejected(self):
        for status in (201, 202, 204):
            with self.subTest(status=status), self.assertRaises(GUARD.ReleaseCheckError):
                self.check({'tag_name': 'v2.0.1', 'draft': False}, status=status)

    def test_network_and_timeout_failures_never_allow_a_build(self):
        for error in (urllib.error.URLError('Offline'), TimeoutError('Timed out')):
            with self.subTest(error=error), self.assertRaises(GUARD.ReleaseCheckError):
                self.check(error=error)

    def test_wrong_tag_or_untyped_draft_is_rejected(self):
        invalid = [{'tag_name': 'titan-3.0.1', 'draft': False}, {'draft': False},
                   {'tag_name': 'v2.0.1'}, None, [],
                   *({'tag_name': 'v2.0.1', 'draft': value} for value in (None, 0, 1, 'false'))]
        for result in invalid:
            with self.subTest(result=result), self.assertRaises(GUARD.ReleaseCheckError):
                self.check(result)

    def test_malformed_duplicate_or_oversized_response_is_rejected(self):
        invalid = [b'{', b'\xff', b'{"tag_name":"v2.0.1","draft":true,"draft":false}',
                   b' ' * (GUARD.MAX_RESPONSE + 1)]
        for contents in invalid:
            with self.subTest(size=len(contents)), self.assertRaises(GUARD.ReleaseCheckError):
                self.check(body=contents)

    def test_redirects_are_not_followed_with_authentication_headers(self):
        request = GUARD.urllib.request.Request('https://api.github.com',
                                              headers={'Authorization': 'Bearer fixture-token'})
        self.assertIsNone(GUARD.NoRedirect().redirect_request(
            request, None, 302, 'Found', {}, 'https://unrelated.example/release'))

    def test_foreign_release_metadata_fails_before_the_network_request(self):
        with patch.object(GUARD.urllib.request, 'build_opener') as opener:
            with self.assertRaises(ValueError):
                GUARD.release_needed({'format': 'titan-debian-ab-v1', 'version': '3.0.1'})
        opener.assert_not_called()

    def test_cli_prints_one_boolean_and_no_output_on_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'release.json'
            path.write_text(json.dumps(release()))
            for needed, expected in ((True, 'true\n'), (False, 'false\n')):
                output = io.StringIO()
                with patch.object(GUARD, 'release_needed', return_value=needed), \
                        contextlib.redirect_stdout(output):
                    self.assertEqual(GUARD.main([str(path)]), 0)
                self.assertEqual(output.getvalue(), expected)
            output, errors = io.StringIO(), io.StringIO()
            with patch.object(GUARD, 'release_needed', side_effect=GUARD.ReleaseCheckError('HTTP 503')), \
                    contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
                self.assertEqual(GUARD.main([str(path)]), 1)
            self.assertEqual(output.getvalue(), '')
            self.assertIn('HTTP 503', errors.getvalue())


if __name__ == '__main__':
    unittest.main()
