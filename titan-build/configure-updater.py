#!/usr/bin/env python3
"""Configure the local signed updater in the complete TitanOS source tree."""
import json
from pathlib import Path
import shutil
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from release_identity import validate_release


def configure(root):
    root = Path(root).resolve()
    release = json.loads((root / '.titan/release.json').read_text())
    version = validate_release(release)
    sources = Path(__file__).resolve().parent / 'updater'
    overlay = root / 'packages/os/overlay'
    payload = overlay / 'usr/libexec/titan-system-update.py'
    payload.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(sources / 'titan-system-update.py', payload)
    payload.chmod(0o755)
    trust = overlay / 'usr/share/titan'
    trust.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(root / '.titan/release-public.pem', trust / 'release-public.pem')
    shutil.copyfile(root / '.titan/release.json', trust / 'release.json')
    for path in trust.iterdir(): path.chmod(0o644)
    shutil.copyfile(sources / 'update.ts', root / 'packages/titand/source/modules/system/update.ts')
    shutil.copyfile(sources / 'update.unit.test.ts', root / 'packages/titand/source/modules/system/update.unit.test.ts')
    package = root / 'packages/titand/package.json'
    value = json.loads(package.read_text())
    value.update(version=version, versionName=release['versionName'])
    package.write_text(json.dumps(value, indent='\t') + '\n')
    lock = root / 'packages/titand/package-lock.json'
    if lock.exists():
        value = json.loads(lock.read_text()); value['version'] = version
        if '' in value.get('packages', {}): value['packages']['']['version'] = version
        lock.write_text(json.dumps(value, indent='\t') + '\n')
    index = root / 'packages/titand/source/index.ts'
    text = index.read_text()
    if "releaseChannel: 'stable'" not in text or "await this.store.set('settings.releaseChannel', 'stable')" not in text:
        raise SystemExit('The TitanOS daemon must use the stable release channel')
    routes = root / 'packages/titand/source/modules/system/routes.ts'
    text = routes.read_text()
    if "channel: z.literal('stable')" not in text or "return 'stable' as const" not in text:
        raise SystemExit('The TitanOS API must use the stable release channel')
    (overlay / 'etc/os-release').write_text(f'NAME="TitanOS"\nPRETTY_NAME="{release.get("versionName", "TitanOS " + version)}"\nID=titanos\nID_LIKE=debian\n'
                                          f'VERSION="{version}"\nVERSION_ID="{version}"\nHOME_URL="https://github.com/ra5on/TitanOS"\n')


if __name__ == '__main__':
    configure(sys.argv[1] if len(sys.argv) == 2 else Path(__file__).resolve().parents[1])
