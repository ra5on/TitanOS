#!/usr/bin/env python3
"""Run the two-account old/new Agent API and MQTT cutover on an isolated loopback.

Requires bwrap, minisign and Mosquitto with its CLI clients. Pass the original
published titand, its signed manifest and the independently built candidate:
  python3 scripts/smoke-agent-legacy.py --version 1.34.0 --stable /tmp/old/titanos-1.34.0-titand \
      --manifest /tmp/old/manifest.json --candidate /tmp/candidate
--mqtt-root may point to an extracted distribution package tree; no host service
is started. The same accounts also exercise native mail, calendar and panel
context before and after upgrading. This development-mode test is separate from QEMU.
"""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
KEYS = SCRIPTS.parent / 'build/mkosi.extra/usr/share/titanos/update-keys'


def run(args):
    stable, manifest = args.stable.resolve(strict=True), args.manifest.resolve(strict=True)
    keys = sorted(KEYS.glob('titanos-release-*.pub')) + sorted(KEYS.glob('titanos-recovery-*.pub'))
    if not any(subprocess.run(['minisign', '-V', '-q', '-p', key, '-m', manifest],
                              capture_output=True).returncode == 0 for key in keys):
        raise RuntimeError('the predecessor manifest has no trusted release signature')
    meta = json.loads(manifest.read_bytes())
    if (meta['version'], meta['tag'], meta['channel']) != (args.version, 'v' + args.version, 'stable'):
        raise RuntimeError('wrong signed predecessor version')
    with stable.open('rb') as f:
        if hashlib.file_digest(f, 'sha256').hexdigest() != meta['app']['sha256']:
            raise RuntimeError('the predecessor binary differs from the release-signed digest')
    with tempfile.TemporaryDirectory(prefix='titanos-agent-legacy-') as temp:
        work = Path(temp)
        for directory in ['config/storage', 'volume', 'bin']:
            (work / directory).mkdir(parents=True)
        (work / 'config/storage/volumes.json').write_text('[{"name":"testdaten","filesystem":"ext4","level":"raid0"}]')
        for name, binary in [('stable', stable), ('candidate', args.candidate.resolve(strict=True))]:
            shutil.copyfile(binary, work / 'bin' / name)
            (work / 'bin' / name).chmod(0o755)
        # Private network and mount namespaces. Neither daemon can reach the NAS or any external service.
        runner = ['bwrap', '--unshare-all', '--die-with-parent', '--ro-bind', '/usr', '/usr',
                  '--symlink', 'usr/bin', '/bin', '--symlink', 'usr/lib', '/lib', '--symlink', 'usr/lib64', '/lib64',
                  '--symlink', 'usr/sbin', '/sbin', '--ro-bind', '/etc', '/etc', '--tmpfs', '/tmp',
                  '--tmpfs', '/run', '--proc', '/proc', '--dev', '/dev', '--dir', '/mnt',
                  '--bind', work / 'config', '/config', '--bind', work / 'volume', '/mnt/testdaten',
                  '--ro-bind', work / 'bin', '/testbin', '--ro-bind', SCRIPTS / 'agent_legacy_probe.py', '/probe.py',
                  '--ro-bind', SCRIPTS / 'agent_legacy_pim_probe.py', '/agent_legacy_pim_probe.py']
        if args.mqtt_root:
            runner.extend(['--ro-bind', args.mqtt_root.resolve(strict=True), '/mqtt-tools'])
        env = {'PATH': os.defpath + ':/usr/sbin', 'LANG': 'C.UTF-8', 'HOME': '/tmp'}
        if args.browser_output:
            # Only the browser tools are shared read-only. The two temporary
            # sessions and the synthetic PIM data stay inside the namespace.
            node = shutil.which('node')
            modules = args.node_modules
            if not node or not modules or not (modules / 'playwright/index.mjs').is_file():
                raise ValueError('the browser run needs Node and --node-modules containing Playwright')
            output = args.browser_output.resolve()
            output.mkdir(parents=True, exist_ok=True)
            browsers = Path(os.getenv('PLAYWRIGHT_BROWSERS_PATH', Path.home() / '.cache/ms-playwright'))
            runner.extend(['--ro-bind', Path(node).resolve(), '/tools/node',
                           '--ro-bind', modules.resolve(strict=True), '/tools/node_modules',
                           '--ro-bind', browsers.resolve(strict=True), '/tools/browsers',
                           '--ro-bind', SCRIPTS / 'test-ui-upgrade-pim.mjs', '/tools/test-ui-upgrade-pim.mjs',
                           '--bind', output, '/evidence'])
            env['PLAYWRIGHT_BROWSERS_PATH'] = '/tools/browsers'
        print(f"Original stable {args.version}: release signature and daemon SHA-256 verified.", flush=True)
        subprocess.run([*runner, '--chdir', '/tmp', 'python3', '-u', '/probe.py'], env=env, check=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--stable', required=True, type=Path)
    parser.add_argument('--version', required=True)
    parser.add_argument('--manifest', required=True, type=Path)
    parser.add_argument('--candidate', required=True, type=Path)
    parser.add_argument('--mqtt-root', type=Path)
    parser.add_argument('--browser-output', type=Path, help='also open both embedded apps for both users in Chromium and save the four screenshots here; requires an embedui candidate')
    parser.add_argument('--node-modules', type=Path, help='node_modules with Playwright for the optional browser run')
    run(parser.parse_args())
