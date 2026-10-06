#!/usr/bin/env python3
"""Retire obsolete development image runs without cancelling production jobs."""
import json
import os
import re
import urllib.error
import urllib.parse
import urllib.request

REPOSITORY = 'ra5on/TitanOS'
BRANCH = 'titan-own-stable'
ACTIVE = {'queued', 'pending', 'in_progress', 'waiting'}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, newurl):
        raise ValueError('Unexpected GitHub redirect')


def request(path, token, method='GET'):
    headers = {'Authorization': 'Bearer ' + token,
               'Accept': 'application/vnd.github+json',
               'X-GitHub-Api-Version': '2022-11-28'}
    query = urllib.request.Request('https://api.github.com/repos/' + REPOSITORY + '/' + path,
                                   headers=headers, method=method,
                                   data=b'' if method == 'POST' else None)
    with urllib.request.build_opener(NoRedirect()).open(query, timeout=30) as response:
        if method == 'POST':
            if response.status != 202:
                raise ValueError('Image cancellation was not accepted')
            return None
        raw = response.read(1024 * 1024 + 1)
        if len(raw) > 1024 * 1024:
            raise ValueError('GitHub response exceeds the safety limit')
        return json.loads(raw)


def retire(sha, token, api=request):
    if not isinstance(sha, str) or not re.fullmatch('[a-f0-9]{40}', sha) or not token:
        raise ValueError('Trusted development identity is missing')
    # A delayed CI run must not cancel a more recent push. Keep the global
    # release queue intact; only this exact development branch is eligible.
    latest = api('git/ref/heads/' + BRANCH, token)
    if not isinstance(latest, dict) or latest.get('ref') != 'refs/heads/' + BRANCH:
        raise ValueError('Development branch identity is invalid')
    if latest.get('object', {}).get('sha') != sha:
        return []
    query = urllib.parse.urlencode({'branch': BRANCH, 'event': 'push', 'per_page': 100})
    payload = api('actions/workflows/debian-image.yml/runs?' + query, token)
    runs = payload.get('workflow_runs') if isinstance(payload, dict) else None
    if not isinstance(runs, list) or len(runs) > 100:
        raise ValueError('Development workflow inventory is invalid')
    cancelled = []
    for run in runs:
        if not isinstance(run, dict) or run.get('head_branch') != BRANCH or run.get('event') != 'push' or run.get('path') != '.github/workflows/debian-image.yml':
            continue
        identifier, head = run.get('id'), run.get('head_sha')
        if type(identifier) is not int or identifier <= 0 or not isinstance(head, str) or not re.fullmatch('[a-f0-9]{40}', head):
            raise ValueError('Development workflow identity is invalid')
        if head == sha or run.get('status') not in ACTIVE:
            continue
        # Recheck before every mutation, including a newer push during cleanup.
        if api('git/ref/heads/' + BRANCH, token).get('object', {}).get('sha') != sha:
            break
        current = api('actions/runs/' + str(identifier), token)
        if (not isinstance(current, dict) or current.get('id') != identifier or
                current.get('head_branch') != BRANCH or current.get('event') != 'push' or
                current.get('path') != '.github/workflows/debian-image.yml' or
                current.get('head_sha') != head or current.get('status') not in ACTIVE):
            continue
        try:
            api('actions/runs/' + str(identifier) + '/cancel', token, 'POST')
        except urllib.error.HTTPError as exc:
            if exc.code != 409:  # The run may have completed during this check.
                raise
        else:
            cancelled.append(identifier)
    return cancelled


def main():
    if (os.environ.get('GITHUB_ACTIONS') != 'true' or
            os.environ.get('GITHUB_REPOSITORY') != REPOSITORY or
            os.environ.get('GITHUB_EVENT_NAME') != 'push' or
            os.environ.get('GITHUB_REF') != 'refs/heads/' + BRANCH):
        raise SystemExit('Requires the trusted development push workflow')
    try:
        result = retire(os.environ.get('GITHUB_SHA'), os.environ.get('GH_TOKEN'))
    except Exception:
        # Never print request objects, tokens or response bodies.
        raise SystemExit('Could not safely retire obsolete development image checks') from None
    print('Retired obsolete development image checks: ' + ', '.join(map(str, result)))


if __name__ == '__main__':
    main()
