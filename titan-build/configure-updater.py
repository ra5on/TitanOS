#!/usr/bin/env python3
"""Apply Titan's local signed updater to the pinned upstream source tree."""
import json
from pathlib import Path
import re
import shutil
import sys


def change(path, old, new):
    text = path.read_text()
    if old not in text:
        if new in text:
            return
        raise SystemExit(f"Pinned updater patch no longer matches {path}")
    path.write_text(text.replace(old, new))


def configure(root):
    root = Path(root).resolve()
    release = json.loads((root / '.titan/release.json').read_text())
    version = release['version']
    if (not re.fullmatch(r'(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-titan\.[1-9][0-9]*)?', version)
            or release.get('osVersion') != version or release.get('stage') != 'stable'
            or release.get('systemCompatibility') != 'titan-umbrel-rugix-amd64-v1'):
        raise SystemExit('Unsupported Titan updater identity')
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
    shutil.copyfile(sources / 'update.ts', root / 'packages/umbreld/source/modules/system/update.ts')
    shutil.copyfile(sources / 'update.unit.test.ts', root / 'packages/umbreld/source/modules/system/update.unit.test.ts')
    package = root / 'packages/umbreld/package.json'
    value = json.loads(package.read_text())
    value.update(version=version, versionName=release.get('versionName', 'TitanOS 2.0'))
    package.write_text(json.dumps(value, indent='\t') + '\n')
    lock = root / 'packages/umbreld/package-lock.json'
    if lock.exists():
        value = json.loads(lock.read_text()); value['version'] = version
        if '' in value.get('packages', {}): value['packages']['']['version'] = version
        lock.write_text(json.dumps(value, indent='\t') + '\n')
    index = root / 'packages/umbreld/source/index.ts'
    text = index.read_text()
    text, count = re.subn(r"releaseChannel: '(?:stable|alpha)'(?: \| '(?:alpha|beta|stable)')*", "releaseChannel: 'stable'", text)
    if count != 1: raise SystemExit('Pinned release-channel type no longer matches')
    text, count = re.subn(r"if \(!\(await this\.store\.get\('settings\.releaseChannel'\)\)\) \{\s*await this\.store\.set\('settings\.releaseChannel', [^\n]+\)\s*\}",
                          "// Normalize persisted experimental channels during the stable transition.\n\t\tawait this.store.set('settings.releaseChannel', 'stable')", text)
    if count != 1 and "await this.store.set('settings.releaseChannel', 'stable')" not in text:
        raise SystemExit('Pinned initial release-channel setting no longer matches')
    index.write_text(text)
    routes = root / 'packages/umbreld/source/modules/system/routes.ts'
    text = routes.read_text()
    text, count = re.subn(r"channel: z\.(?:enum\(\[[^\]]+\]\)|literal\('stable'\))", "channel: z.literal('stable')", text)
    if count != 1: raise SystemExit('Pinned release-channel route no longer matches')
    text = re.sub(r"return \(await ctx\.umbreld\.store\.get\('settings\.releaseChannel'\)\) \|\| '(?:alpha|stable)'", "return 'stable' as const", text)
    routes.write_text(text)
    mcp = root / 'packages/umbreld/source/modules/mcp/tools/system-management.ts'
    if mcp.exists():
        text = mcp.read_text().replace("z.enum(['stable', 'beta'])", "z.literal('stable')")
        text = text.replace('Toggle umbrelOS Beta Program enrollment by setting the release channel', 'Use the TitanOS stable update channel')
        text = text.replace('Choose the stable or beta umbrelOS update channel.', 'TitanOS supports only the stable system update channel.')
        mcp.write_text(text)
    # Keep /umbrelOS and internal compatibility paths unchanged; only the public
    # operating-system identity is branded. Debian's package/tool behavior stays.
    (overlay / 'etc/os-release').write_text(f'NAME="TitanOS"\nPRETTY_NAME="{release.get("versionName", "TitanOS " + version)}"\nID=titanos\nID_LIKE=debian\n'
                                          f'VERSION="{version}"\nVERSION_ID="{version}"\nHOME_URL="https://github.com/ra5on/TitanOS"\n')


if __name__ == '__main__':
    configure(sys.argv[1] if len(sys.argv) == 2 else Path(__file__).resolve().parents[1])
