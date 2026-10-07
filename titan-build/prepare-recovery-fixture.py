#!/usr/bin/env python3
"""Prepare a private versioned A/B baseline; never include fixture keys in guests."""
import argparse
import errno
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import urllib.parse

BAKERY = 'ghcr.io/rugix/rugix-bakery@sha256:41fbea6785fccec14e43d22501b50af8cb4812f3560fc5d5abf41e2607350ef7'
BASELINE = '0.0.0'


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def digest(path):
    result = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 ** 2), b''): result.update(block)
    return result.hexdigest()


def filesystem_read(filesystem, target, temporary):
    destination = temporary / 'readback'
    run('debugfs', '-R', f'dump {target} {destination}', str(filesystem), capture_output=True)
    value = destination.read_bytes()
    destination.unlink()
    return value


def filesystem_write(filesystem, target, contents, temporary):
    source = temporary / 'replacement'
    source.write_bytes(contents)
    run('debugfs', '-w', '-R', f'rm {target}', str(filesystem), capture_output=True)
    run('debugfs', '-w', '-R', f'write {source} {target}', str(filesystem), capture_output=True)
    run('debugfs', '-w', '-R', f'set_inode_field {target} mode 0100644', str(filesystem), capture_output=True)
    source.unlink()
    if filesystem_read(filesystem, target, temporary) != contents:
        raise ValueError(f'The private baseline filesystem did not accept {target}')


def patch_baseline(boot, system, release, temporary):
    baseline = {**release, 'version': BASELINE, 'osVersion': BASELINE, 'versionName': f'TitanOS {BASELINE}'}
    filesystem_write(system, '/usr/share/titan/release.json', (json.dumps(baseline) + '\n').encode(), temporary)
    package = json.loads(filesystem_read(system, '/opt/titand/package.json', temporary))
    package.update(version=BASELINE, versionName=f'TitanOS {BASELINE}')
    filesystem_write(system, '/opt/titand/package.json', (json.dumps(package) + '\n').encode(), temporary)
    os_release = filesystem_read(system, '/etc/os-release', temporary).decode()
    filesystem_write(system, '/etc/os-release', os_release.replace(release['version'], BASELINE).encode(), temporary)
    value = f'# GRUB Environment Block\ntitan_slot_version={BASELINE}\n'.encode()
    filesystem_write(boot, '/titan-version.grubenv', value + b'#' * (1024 - len(value)), temporary)
    return baseline


def signed_descriptor(directory, release, image, update, verification, commit, key):
    version = release['version']
    target = directory / f'v{version}'
    target.mkdir()
    assets = []
    for source, suffix in ((image, '.img.xz'), (update, '.update')):
        name = f'titan-{version}{suffix}'
        try:
            os.link(source, target / name)
        except OSError as error:
            if error.errno != errno.EXDEV:
                raise
            shutil.copyfile(source, target / name)
        assets.append({'name': name, 'sizeBytes': source.stat().st_size, 'sha256': digest(source)})
    (target / 'release.json').write_text(json.dumps(release) + '\n')
    manifest = {'schemaVersion': 1, 'releaseVersion': version, 'osVersion': version,
                'architecture': 'amd64', 'firmware': 'UEFI', 'updateFormat': 'rugix',
                'systemCompatibility': release['systemCompatibility'], 'stage': 'stable',
                'ownTitanUpdateChannel': True, 'releaseEligible': True, 'buildCommit': commit,
                'imageVerification': verification, 'assets': assets,
                'privateRecoveryFixture': {'baseline': BASELINE, 'source': 'current build with isolated older version identity',
                                           'doesNotProveLegacyMigration': True}}
    (target / 'build-manifest.json').write_text(json.dumps(manifest) + '\n')
    sums = ''.join(f'{digest(path)}  {path.name}\n' for path in sorted(target.iterdir()))
    (target / 'SHA256SUMS').write_text(sums)
    run('openssl', 'pkeyutl', '-sign', '-rawin', '-inkey', str(key), '-in', str(target / 'SHA256SUMS'),
        '-out', str(target / 'SHA256SUMS.sig'), capture_output=True)
    gh_assets = [{'name': path.name, 'size': path.stat().st_size,
                  'browser_download_url': f'https://github.com/ra5on/TitanOS/releases/download/v{version}/{path.name}'}
                 for path in sorted(target.iterdir())]
    descriptor = {'tag_name': f'v{version}', 'name': release['versionName'], 'draft': False, 'prerelease': False, 'assets': gh_assets}
    (target / 'github.json').write_text(json.dumps(descriptor) + '\n')
    return descriptor


def prepare(root, artifacts, output):
    if not os.environ.get('TITAN_SIGNING_KEY'):
        raise ValueError('The private recovery gate needs TITAN_SIGNING_KEY; it must never be copied to the guest')
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    release = json.loads((root / '.titan/release.json').read_text())
    verification = json.loads((artifacts / 'image-verification.json').read_text())
    if verification.get('bridgeNetworkSmoke', {}).get('status') != 'passed':
        raise ValueError('Run the real bridge gate before preparing signed recovery fixtures')
    commit = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
    raw = artifacts / f'titan-{release["version"]}.img'
    compressed = raw.with_suffix('.img.xz')
    if not compressed.exists():
        with compressed.open('xb') as destination:
            run('xz', '--threads=2', '-3', '--stdout', str(raw), stdout=destination)
        run('xz', '--test', str(compressed))
    payloads = output / 'bundle' / 'payloads'
    payloads.mkdir(parents=True)
    image = output / f'titan-{BASELINE}.img'
    run('cp', '--sparse=always', str(raw), str(image))
    try:
        for number in (2, 4):
            partition = next(part for part in verification['partitions'] if part['number'] == number)
            file = payloads / f'partition-{number}.img'
            run('dd', f'if={raw}', f'of={file}', 'bs=1M', f'skip={partition["firstLba"] * 512}',
                f'count={partition["sizeBytes"]}', 'iflag=skip_bytes,count_bytes', 'conv=sparse', 'status=none')
        baseline = patch_baseline(payloads / 'partition-2.img', payloads / 'partition-4.img', release, output)
        for number in (2, 4):
            partition = next(part for part in verification['partitions'] if part['number'] == number)
            run('dd', f'if={payloads / f"partition-{number}.img"}', f'of={image}', 'bs=1M',
                f'seek={partition["firstLba"] * 512}', 'oflag=seek_bytes', 'conv=notrunc', 'status=none')
        bundle = output / f'titan-{BASELINE}.update'
        (payloads.parent / 'rugix-bundle.toml').write_text('''update-type = "full"
[[payloads]]
filename = "partition-2.img"
delivery = { type = "slot", slot = "boot" }
block-encoding = { chunker = "casync-64", deduplicate = true, compression = { type = "xz", level = 1 } }
[[payloads]]
filename = "partition-4.img"
delivery = { type = "slot", slot = "system" }
block-encoding = { chunker = "casync-64", deduplicate = true, compression = { type = "xz", level = 1 } }
''')
        run('docker', 'run', '--rm', '--user', f'{os.getuid()}:{os.getgid()}', '--entrypoint', '/usr/local/bin/rugix-bundler',
            '-v', f'{output}:/fixture', BAKERY, 'bundle', '/fixture/bundle', f'/fixture/{bundle.name}')
    finally:
        shutil.rmtree(payloads.parent, ignore_errors=True)
    baseline_compressed = image.with_suffix('.img.xz')
    with baseline_compressed.open('xb') as destination:
        run('xz', '--threads=2', '-1', '--stdout', str(image), stdout=destination)
    run('xz', '--test', str(baseline_compressed))
    with tempfile.TemporaryDirectory(prefix='titan-recovery-key-', dir=os.environ.get('RUNNER_TEMP')) as secret:
        secret = Path(secret)
        secret.chmod(0o700)
        key = secret / 'private.pem'
        key.write_text(os.environ['TITAN_SIGNING_KEY'].rstrip() + '\n'); key.chmod(0o600)
        # Check the fixture is signed by exactly the image's normal public key.
        public = subprocess.check_output(['openssl', 'pkey', '-in', str(key), '-pubout'], stderr=subprocess.DEVNULL)
        if public != (root / '.titan/release-public.pem').read_bytes():
            raise ValueError('The recovery fixture signing key does not match this image')
        baseline_verification = json.loads(json.dumps(verification))
        baseline_verification['uefiHttpSmoke']['installedRelease'] = {'version': BASELINE, 'name': f'TitanOS {BASELINE}'}
        signed_descriptor(output, baseline, baseline_compressed, bundle, baseline_verification, commit, key)
        candidate = signed_descriptor(output, release, compressed, artifacts / f'titan-{release["version"]}.update', verification, commit, key)
    run('openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2',
        '-subj', '/CN=Titan private recovery fixture', '-addext', 'subjectAltName=IP:10.0.2.2',
        '-keyout', str(output / 'https-key.pem'), '-out', str(output / 'https-cert.pem'), capture_output=True)
    (output / 'https-key.pem').chmod(0o600)
    (output / 'latest.json').write_text(json.dumps(candidate) + '\n')
    (output / 'fixture.json').write_text(json.dumps({'baselineVersion': BASELINE, 'candidateVersion': release['version'],
        'baselineImage': str(image), 'sourceCommit': commit, 'privateBaseline': True, 'legacyMigrationTested': False}) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--artifacts', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    prepare(args.root.resolve(), args.artifacts.resolve(), args.output.resolve())


if __name__ == '__main__':
    main()
