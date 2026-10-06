#!/usr/bin/env bash
# Publish one signed, boot-tested stable TitanOS release; public tags are immutable.
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TASK_ARTIFACT_DIR="${TITAN_ARTIFACT_DIR:-${TASK_ROOT}/dist}"
cd "${TASK_ROOT}"
openssl pkeyutl -verify -rawin -pubin -inkey .titan/release-public.pem \
  -in "${TASK_ARTIFACT_DIR}/SHA256SUMS" -sigfile "${TASK_ARTIFACT_DIR}/SHA256SUMS.sig"
(cd "${TASK_ARTIFACT_DIR}"; sha256sum --check SHA256SUMS)
python3 - "${TASK_ARTIFACT_DIR}" "${TASK_ROOT}" <<'PY'
import hashlib, json, pathlib, re, subprocess, sys
sys.path.insert(0, str(pathlib.Path(sys.argv[2])/'titan-build'))
from release_identity import COMPATIBILITY, validate_release
d = pathlib.Path(sys.argv[1])
m = json.loads((d/'build-manifest.json').read_text())
r = json.loads((d/'release.json').read_text())
version = validate_release(r)
expected = {'schemaVersion': 1, 'releaseVersion': version, 'osVersion': version,
            'architecture': 'amd64', 'firmware': 'UEFI', 'updateFormat': 'rugix',
            'systemCompatibility': COMPATIBILITY, 'stage': 'stable',
            'ownTitanUpdateChannel': True, 'releaseEligible': True}
if any(type(m.get(key)) is not type(value) or m.get(key) != value for key, value in expected.items()):
    raise ValueError('The signed manifest is not a supported stable TitanOS release')
verification = m.get('imageVerification', {})
boot = verification.get('uefiHttpSmoke', {})
if (verification.get('structuralCheck') != 'passed' or boot.get('status') != 'passed'
        or boot.get('installedRelease') != {'version': version, 'name': r['versionName']}
        or type(boot.get('bootDiskSizeBytes')) is not int or boot['bootDiskSizeBytes'] < 32 * 1024**3):
    raise ValueError('The exact TitanOS release must pass a real UEFI boot on a sufficient target disk')
head = subprocess.check_output(['git', '-C', sys.argv[2], 'rev-parse', 'HEAD'], text=True).strip()
if m.get('buildCommit') != head:
    raise ValueError('The release was not built from the checkout being published')
assets = m.get('assets')
names = {f'titan-{version}.img.xz', f'titan-{version}.update'}
if (not isinstance(assets, list) or len(assets) != 2
        or any(not isinstance(item, dict) or not isinstance(item.get('name'), str) for item in assets)
        or {item.get('name') for item in assets} != names):
    raise ValueError('The release needs exactly the TitanOS image and update bundle')
for asset in assets:
    path = d / asset['name']
    if (not path.is_file() or path.is_symlink() or type(asset.get('sizeBytes')) is not int
            or not 0 < asset['sizeBytes'] < 2 * 1024**3 or path.stat().st_size != asset['sizeBytes']):
        raise ValueError('Invalid TitanOS release asset size')
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''): digest.update(block)
    if digest.hexdigest() != asset.get('sha256'):
        raise ValueError('The manifest does not match the release asset')
files = {path.name for path in d.iterdir() if path.is_file()}
if any(path.is_symlink() or not path.is_file() for path in d.iterdir()):
    raise ValueError('Only regular signed files may be published')
sums = (d/'SHA256SUMS').read_text().splitlines()
checksummed = [re.fullmatch(r'[a-f0-9]{64}  ([A-Za-z0-9][A-Za-z0-9._-]{0,180})', line) for line in sums]
if (any(match is None for match in checksummed)
        or len({match[1] for match in checksummed}) != len(checksummed)
        or {match[1] for match in checksummed} != files - {'SHA256SUMS', 'SHA256SUMS.sig'}):
    raise ValueError('Every uploaded file must be covered by the signed checksum list')
PY
task_version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["version"])' "${TASK_ARTIFACT_DIR}/release.json")
task_tag="v${task_version}"
task_source=$(git rev-parse HEAD)
if gh release view "$task_tag" --repo ra5on/TitanOS --json isDraft --jq .isDraft > "$RUNNER_TEMP/titan-release-exists" 2>/dev/null; then
  if [[ "$(cat "$RUNNER_TEMP/titan-release-exists")" != true ]]; then
    echo 'An already published release is immutable; increment .titan/release.json.' >&2
    exit 1
  fi
else
  gh release create "$task_tag" --repo ra5on/TitanOS --draft --prerelease=false --target "$task_source" \
    --title "TitanOS ${task_version}" --notes-file titan-build/RELEASE.md
fi
gh release upload "$task_tag" --repo ra5on/TitanOS "${TASK_ARTIFACT_DIR}"/* --clobber
gh release edit "$task_tag" --repo ra5on/TitanOS --draft=false --prerelease=false --latest=true
