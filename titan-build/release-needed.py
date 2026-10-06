#!/usr/bin/env python3
"""Check for an immutable public release before starting an image build."""
import argparse
import json
import os
from pathlib import Path
import sys
import urllib.error
import urllib.request

from release_identity import REPOSITORY, validate_release

MAX_RESPONSE = 2 * 1024 ** 2


class ReleaseCheckError(Exception):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ReleaseCheckError('GitHub returned duplicate release fields')
        result[key] = value
    return result


def release_needed(release, token=''):
    tag = 'v' + validate_release(release)
    headers = {'Accept': 'application/vnd.github+json', 'User-Agent': 'TitanOS-release-check'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(
        f'https://api.github.com/repos/{REPOSITORY}/releases/tags/{tag}', headers=headers)
    try:
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=30) as response:
            if response.status != 200:
                raise ReleaseCheckError('GitHub returned an unexpected release response')
            contents = response.read(MAX_RESPONSE + 1)
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return True
        raise ReleaseCheckError(f'GitHub release check failed with HTTP {error.code}') from error
    except (urllib.error.URLError, OSError) as error:
        raise ReleaseCheckError('GitHub release check is unavailable') from error
    if len(contents) > MAX_RESPONSE:
        raise ReleaseCheckError('GitHub release response is too large')
    try:
        result = json.loads(contents, object_pairs_hook=unique_pairs)
    except (ValueError, UnicodeDecodeError) as error:
        raise ReleaseCheckError('GitHub returned invalid release JSON') from error
    if (not isinstance(result, dict) or result.get('tag_name') != tag
            or type(result.get('draft')) is not bool):
        raise ReleaseCheckError('GitHub returned an invalid or mismatched release identity')
    return result['draft']


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('metadata', type=Path, nargs='?',
                        default=Path(__file__).resolve().parents[1] / '.titan/release.json')
    args = parser.parse_args(argv)
    try:
        release = json.loads(args.metadata.read_text())
        needed = release_needed(release, os.environ.get('GH_TOKEN', ''))
    except (ReleaseCheckError, OSError, ValueError) as error:
        print(str(error), file=sys.stderr)
        return 1
    print('true' if needed else 'false')
    return 0


if __name__ == '__main__':
    sys.exit(main())
