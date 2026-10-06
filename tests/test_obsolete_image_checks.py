"""Development cleanup cannot retire production, current or newer builds."""
import importlib.util
from pathlib import Path
import unittest
import urllib.error

import yaml

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('obsolete_images', ROOT/'scripts/retire-obsolete-image-checks.py')
cleanup = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(cleanup)
SHA = 'a' * 40


def record(identifier=1, **changes):
    return {'id': identifier, 'head_sha': 'b'*40, 'head_branch': cleanup.BRANCH,
            'event': 'push', 'path': '.github/workflows/debian-image.yml',
            'status': 'in_progress', **changes}


class ObsoleteImageTests(unittest.TestCase):
    def api(self, records, latest=SHA, changes=None):
        posts = []
        def call(path, token, method='GET'):
            self.assertEqual(token, 'private-test-token')
            if method == 'POST':
                posts.append(path)
                return None
            if path.startswith('git/ref/'):
                return {'ref': 'refs/heads/'+cleanup.BRANCH, 'object': {'sha': latest}}
            if path.startswith('actions/workflows/'):
                return {'workflow_runs': records}
            value = next(row for row in records if path == 'actions/runs/'+str(row['id']))
            return {**value, **(changes or {})}
        return call, posts

    def test_only_obsolete_push_images_on_exact_development_branch_are_cancelled(self):
        rows = [record(), record(2, head_sha=SHA), record(3, head_branch='main'),
                record(4, path='.github/workflows/debian-security.yml'),
                record(5, event='workflow_dispatch'), record(6, status='completed')]
        api, posts = self.api(rows)
        self.assertEqual(cleanup.retire(SHA, 'private-test-token', api), [1])
        self.assertEqual(posts, ['actions/runs/1/cancel'])

    def test_delayed_ci_for_old_head_cannot_cancel_newer_push(self):
        api, posts = self.api([record(head_sha='c'*40)], latest='c'*40)
        self.assertEqual(cleanup.retire(SHA, 'private-test-token', api), [])
        self.assertEqual(posts, [])

    def test_run_identity_or_completion_changed_before_mutation_is_skipped(self):
        for changes in ({'head_sha': 'c'*40}, {'head_branch':'main'}, {'status':'completed'},
                        {'event':'schedule'}, {'path':'.github/workflows/debian-security.yml'}, {'id':999}):
            with self.subTest(changes=changes):
                api, posts = self.api([record()], changes=changes)
                self.assertEqual(cleanup.retire(SHA, 'private-test-token', api), [])
                self.assertEqual(posts, [])

    def test_new_push_during_inventory_read_prevents_cancellation(self):
        api, posts = self.api([record()])
        reads = 0
        def advancing(path, token, method='GET'):
            nonlocal reads
            if path.startswith('git/ref/'):
                reads += 1
                if reads > 1:
                    return {'object': {'sha':'c'*40}}
            return api(path, token, method)
        self.assertEqual(cleanup.retire(SHA, 'private-test-token', advancing), [])
        self.assertEqual(posts, [])

    def test_invalid_inventory_fails_before_any_mutation(self):
        for row in (record(id=True), record(head_sha='invalid'), record(id=-1)):
            api, posts = self.api([row])
            with self.subTest(row=row), self.assertRaises(ValueError):
                cleanup.retire(SHA, 'private-test-token', api)
            self.assertEqual(posts, [])

    def test_racing_completion_is_ignored_but_permission_errors_stop_cleanup(self):
        for code in (409, 403):
            api, posts = self.api([record()])
            def error(path, token, method='GET'):
                if method == 'POST':
                    raise urllib.error.HTTPError('fixed-endpoint', code, 'fixture', {}, None)
                return api(path, token, method)
            with self.subTest(code=code):
                if code == 409:
                    self.assertEqual(cleanup.retire(SHA, 'private-test-token', error), [])
                else:
                    with self.assertRaises(urllib.error.HTTPError):
                        cleanup.retire(SHA, 'private-test-token', error)

    def test_cleanup_job_has_no_publish_or_signing_permissions_and_never_runs_on_pr(self):
        workflow = yaml.load((ROOT/'.github/workflows/ci.yml').read_text(), Loader=yaml.BaseLoader)
        job = workflow['jobs']['retire_obsolete_images']
        self.assertEqual(job['permissions'], {'contents':'read', 'actions':'write'})
        self.assertEqual(job['if'], "github.event_name == 'push' && github.ref == 'refs/heads/titan-own-stable'")
        self.assertEqual(job['steps'][0]['with']['persist-credentials'], 'false')
        self.assertNotIn('secrets.', str(job))
