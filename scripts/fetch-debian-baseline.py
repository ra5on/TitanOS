#!/usr/bin/env python3
"""Fetch a signed installation baseline of this system family into CI files."""
import hashlib
import importlib.util
import json
import lzma
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
spec = importlib.util.spec_from_file_location('maintenance', ROOT/'scripts/debian-maintenance.py')
maintenance = importlib.util.module_from_spec(spec)
spec.loader.exec_module(maintenance)
updates = maintenance.updates


def checksums(data):
    records = {}
    for line in data.decode('ascii').splitlines():
        match = re.fullmatch(r'([a-f0-9]{64})  ([A-Za-z0-9_.+-]+)', line)
        if not match or match[2] in records:
            raise ValueError('Invalid signed checksums')
        records[match[2]] = match[1]
    return records


def main():
    if os.environ.get('GITHUB_ACTIONS') != 'true':
        raise SystemExit('Requires disposable GitHub Actions runner')
    token = os.environ.get('GH_TOKEN')
    releases = maintenance.release_list(token)
    candidates = []
    maximum = updates.version(os.environ['TITAN_PREVIOUS_TAG']) if os.environ.get('TITAN_PREVIOUS_TAG') else None
    for release in releases:
        names = [a.get('name') for a in release.get('assets', [])]
        if not any(isinstance(name, str) and name.endswith('-amd64.img.xz') for name in names):
            continue
        try:
            version = updates.version(release['tag_name'])
        except updates.Error:
            continue
        if maximum is None or version <= maximum:
            candidates.append((version, release))
    directory = ROOT/'dist/debian-baseline'
    directory.mkdir(parents=True, exist_ok=True)
    for _, release in sorted(candidates, key=lambda row:row[0], reverse=True):
        # Fail closed if an eligible published own-system release is invalid.
        manifest, assets = maintenance.checked_release(release, token)
        name = f"titan-{manifest['version']}-amd64.img.xz"
        if name not in assets:
            continue
        sums = updates.fetch(assets['SHA256SUMS'], token, binary=True, maximum=32768)
        signature = updates.fetch(assets['SHA256SUMS.sig'], token, binary=True, maximum=1024)
        with tempfile.TemporaryDirectory(prefix='titan-baseline-signature-') as temporary:
            work = Path(temporary)
            (work/'checksums').write_bytes(sums)
            (work/'signature').write_bytes(signature)
            subprocess.run(['openssl','pkeyutl','-verify','-rawin','-pubin','-inkey',
                            str(updates.PUBLIC_KEY),'-in',str(work/'checksums'),'-sigfile',str(work/'signature')],
                           check=True, capture_output=True)
        expected = checksums(sums).get(name)
        if not expected:
            raise ValueError('Installation image absent from signed checksums')
        compressed = directory/'baseline.img.xz'
        raw = directory/'baseline.img'
        if compressed.exists() or raw.exists():
            raise ValueError('Baseline files already exist')
        headers = {'Accept':'application/octet-stream','User-Agent':'Titan-Baseline'}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        request = urllib.request.Request(assets[name], headers=headers)
        digest = hashlib.sha256()
        total = 0
        deadline = time.monotonic() + 1800
        try:
            with compressed.open('xb') as output, urllib.request.build_opener(updates.Redirect()).open(request, timeout=30) as response:
                while chunk := response.read(1024*1024):
                    total += len(chunk)
                    if total > 2147483648 or time.monotonic() > deadline:
                        raise ValueError('Baseline exceeds download limits')
                    output.write(chunk)
                    digest.update(chunk)
            if digest.hexdigest() != expected:
                raise ValueError('Baseline image hash differs from signed checksums')
            size = 0
            with lzma.open(compressed,'rb') as source, raw.open('xb') as output:
                while chunk := source.read(4*1024*1024):
                    size += len(chunk)
                    if size > 48*1024**3:
                        raise ValueError('Unexpected baseline disk capacity')
                    if chunk.strip(b'\0'):
                        output.write(chunk)
                    else:
                        output.seek(len(chunk),1)
                output.truncate(size)
            if size != 48*1024**3:
                raise ValueError('Baseline disk layout is not this installation family')
            (directory/'identity.json').write_text(json.dumps({'version':manifest['version'],
                'tag':release['tag_name'],'compressed_sha256':expected})+'\n')
            env = os.environ.get('GITHUB_ENV')
            if env:
                with open(env,'a') as stream:
                    stream.write('TITAN_BASELINE_VERSION='+manifest['version']+'\n')
            print('Verified signed installation baseline: ' + manifest['version'])
            return
        except Exception:
            raw.unlink(missing_ok=True)
            raise
        finally:
            compressed.unlink(missing_ok=True)
    raise ValueError('No compatible signed published installation baseline is available')


if __name__ == '__main__':
    main()
