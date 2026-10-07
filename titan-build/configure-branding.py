#!/usr/bin/env python3
"""Apply current TitanOS presentation assets and numeric release labels.

The checked-out source already uses the Titan runtime namespace. This helper
updates version text and owned artwork without rewriting external catalog,
hardware, license, or attribution contracts.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import re
import struct
import zlib


LANGUAGES = (
    'bg', 'cs', 'da', 'de', 'el', 'en', 'es', 'et', 'fr', 'hr', 'hu',
    'is', 'it', 'ja', 'ko', 'nb', 'nl', 'pl', 'pt', 'pt-BR', 'ro', 'ru',
    'sk', 'sl', 'sv', 'tr', 'uk', 'zh', 'zh-TW',
)
PROTECTED_PHRASES = re.compile(
    r'https?://[^\s<>"\']+|'
    r'Titan (?:Home|Pro|App Store|Private Cloud|Local HTTPS CA|Support)|'
    r'Titan (?:for|pour|para|per|fyrir|за|na) (?:Mac|iPhone|iOS)|'
    r'iPhone için Titan|'
    r'Titan(?:-App| app)\b|titan-local-ca\.crt',
    re.IGNORECASE,
)
OS_NAME = re.compile(r'(?<![A-Za-z0-9_])(?:titanOS|TitanOS|Titan OS)(?![A-Za-z0-9_])')
DEVICE_NAME = re.compile(r'(?<![A-Za-z0-9_])Titan(?![A-Za-z0-9_])')
# These languages attach case/possessive endings to the product name. Replace
# only the known translated device forms, never account names or runtime IDs.
DEVICE_INFLECTIONS = {
    'hu': {
        'Titanbe': 'Titanba', 'Titaned': 'Titanod', 'Titaneden': 'Titanodon',
        'Titanedet': 'Titanodat', 'Titanedhez': 'Titanodhoz', 'Titanedre': 'Titanodra',
        'Titanedről': 'Titanodról', 'Titanemen': 'Titanomon', 'Titanemet': 'Titanomat',
        'Titanen': 'Titanon', 'Titanhez': 'Titanhoz', 'Titanje': 'Titanja',
        'Titanjében': 'Titanjában', 'Titannek': 'Titannak', 'Titanre': 'Titanra',
        'Titanről': 'Titanról', 'Titant': 'Titant',
    },
    'et': {f'Titan{ending}': f'Titan{ending}' for ending in ('i', 'iga', 'ile', 'is', 'isse', 'ist', 'it')},
    'hr': {f'Titan{ending}': f'Titan{ending}' for ending in ('a', 'om', 'u')},
    'sl': {f'Titan{ending}': f'Titan{ending}' for ending in ('a', 'om', 'u')},
}
# These are actual separately published clients, not Titan products. Some
# languages translate the words between "Titan" and "Mac/iPhone", so protecting
# the translation keys is safer than relying only on literal English phrases.
UPSTREAM_CLIENT_KEYS = (
    'desktop.welcome.files.mac-', 'desktop.welcome.photos.description',
    'photos-source.phone-settings-note', 'photos-empty.source-iphone-description',
    'whats-new-titanos-2-0.mac-app-',
)

def rebrand_text(value: str, language: str | None = None) -> str:
    """Replace presentation names while protecting URLs and real products."""
    def device_text(text: str) -> str:
        inflections = DEVICE_INFLECTIONS.get(language, {})
        if inflections:
            pattern = r'(?<![\w])(?:' + '|'.join(map(re.escape, sorted(inflections, key=len, reverse=True))) + r')(?![\w])'
            text = re.sub(pattern, lambda match: inflections[match.group()], text)
        return DEVICE_NAME.sub('Titan', OS_NAME.sub('TitanOS', text))

    pieces = []
    start = 0
    for match in PROTECTED_PHRASES.finditer(value):
        pieces.append(device_text(value[start:match.start()]))
        pieces.append(match.group())
        start = match.end()
    pieces.append(device_text(value[start:]))
    return ''.join(pieces)


def replace_guarded(path: Path, before: str, after: str) -> None:
    content = path.read_text()
    if before in content:
        path.write_text(content.replace(before, after))
    elif after not in content:
        raise RuntimeError(f'Pinned branding source did not match: {path} ({before!r})')


def make_icon(size: int, notification: bool = False) -> bytes:
    """Render the original T mark to PNG using only the Python standard library."""
    pixels = bytearray()
    # Sample four times per pixel for smooth edges at small favicon sizes.
    for y in range(size):
        pixels.append(0)  # PNG filter: none
        for x in range(size):
            samples = []
            for dy in (0.25, 0.75):
                for dx in (0.25, 0.75):
                    px, py = (x + dx) * 96 / size, (y + dy) * 96 / size
                    corner_x = max(22 - px, 0, px - 74)
                    corner_y = max(22 - py, 0, py - 74)
                    if corner_x * corner_x + corner_y * corner_y > 22 * 22:
                        color = (0, 0, 0, 0)
                    elif (18 <= px <= 78 and 21 <= py <= 34) or (41 <= px <= 55 and 34 <= py <= 80):
                        color = (93, 217, 251, 255)
                    else:
                        color = (19, 35, 61, 255)
                    if notification and (px - 76) ** 2 + (py - 20) ** 2 <= 12 ** 2:
                        color = (255, 156, 59, 255)
                    samples.append(color)
            pixels.extend(round(sum(p[c] for p in samples) / 4) for c in range(4))

    def chunk(kind: bytes, data: bytes) -> bytes:
        return struct.pack('>I', len(data)) + kind + data + struct.pack('>I', zlib.crc32(kind + data) & 0xffffffff)

    return b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', size, size, 8, 6, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(bytes(pixels), 9)) + chunk(b'IEND', b'')


def apply(root: Path) -> None:
    root = root.resolve()
    assets = Path(__file__).resolve().parent / 'branding'
    ui = root / 'packages/ui'
    metadata = root / '.titan/release.json'
    version_name = json.loads(metadata.read_text()).get('versionName', 'TitanOS 2.0.0') if metadata.is_file() else 'TitanOS 2.0.0'
    if not re.fullmatch(r'TitanOS [0-9]+\.[0-9]+\.[0-9]+', version_name):
        raise RuntimeError('Invalid Titan presentation version')
    # Explicit file list: an upstream change needs review rather than a broad rename.
    for name in LANGUAGES:
        path = ui / f'public/locales/{name}.json'
        translations = json.loads(path.read_text())
        if not isinstance(translations, dict) or 'titanos' not in translations or 'titan' not in translations:
            raise RuntimeError(f'Unexpected translations in {path}')
        for key, value in translations.items():
            if isinstance(value, str) and not key.startswith(UPSTREAM_CLIENT_KEYS):
                translations[key] = rebrand_text(value, name)
        translations['titan'] = 'Titan'
        translations['titanos'] = 'TitanOS'
        translations['beta-program'] = 'TitanOS Update-Kanal' if name == 'de' else 'TitanOS update channel'
        translations['beta-program-description'] = (
            'TitanOS erhält signierte Stable-Systemupdates. Alpha- und Beta-Kennzeichnungen gelten nur für einzelne Funktionen.'
            if name == 'de' else
            'TitanOS receives signed stable system updates. Alpha and beta labels apply only to individual features.'
        )
        translations['photos'] = "Foto's"
        if name in ('de', 'en'):
            translations['software-update.title'] = ('Software-Update & Wiederherstellung' if name == 'de'
                                                      else 'Software Update & Recovery')
        path.write_text(json.dumps(translations, ensure_ascii=False, indent=2) + '\n')

    # Upgrade already-branded presentation text as well as pristine upstream text.
    for relative in ('index.html', 'src/routes/whats-new.ts'):
        path = ui / relative
        path.write_text(re.sub(r'\bTitanOS [0-9]+\.[0-9]+(?:\.[0-9]+)?(?![.\d])', version_name, path.read_text()))

    for relative, before, after in (
        ('index.html', '<title>Titan</title>', '<title>Titan</title>'),
        ('index.html', '<h1>titanOS</h1>', f'<h1>{version_name}</h1>'),
        ('src/utils/tab-attention.ts', '`Titan: ${this.notification.title}`', '`Titan: ${this.notification.title}`'),
        ('src/routes/whats-new.ts', "WHATS_NEW_VERSION_NAME = 'titanOS 2.0'", f"WHATS_NEW_VERSION_NAME = '{version_name}'"),
        ('src/routes/settings/_components/software-update-list-row.tsx', '`titanOS ${LOADING_DASH}`', '`TitanOS ${LOADING_DASH}`'),
        ('src/routes/settings/mobile/software-update.tsx', '`titanOS ${LOADING_DASH}`', '`TitanOS ${LOADING_DASH}`'),
        ('src/features/files/components/shared/cloud-constellation.tsx', "alt='titanOS'", "alt='TitanOS'"),
        ('src/features/files/components/cloud-break-diagram.tsx', "alt='titanOS'", "alt='TitanOS'"),
        ('src/features/photos/components/sources/source-icon.tsx', "alt='titanOS'", "alt='TitanOS'"),
        ('src/hooks/use-is-home-or-pro.ts', "t('device-name.home-or-pro')", "t('titan')"),
        ('src/features/files/components/listing/search-listing/index.tsx', "userName ?? 'Titan'", "userName ?? 'Titan'"),
    ):
        replace_guarded(ui / relative, before, after)

    for source, destination in (
        ('titan-logo.tsx', 'src/components/titan-logo.tsx'),
        ('titan-logo-draw.tsx', 'src/components/titan-logo-draw.tsx'),
        ('titan-mark.svg', 'public/assets/titan-app.svg'),
        ('titan-mark.svg', 'public/favicon/titan.svg'),
    ):
        (ui / destination).write_bytes((assets / source).read_bytes())

    manifest_path = ui / 'public/site.webmanifest'
    manifest = json.loads(manifest_path.read_text())
    manifest.update(name='TitanOS', short_name='Titan')
    manifest_path.write_text(json.dumps(manifest, indent=2) + '\n')

    icons = (
        ('favicon-16x16.png', 16, False), ('favicon-32x32.png', 32, False),
        ('favicon-notification.png', 32, True), ('apple-touch-icon.png', 180, False),
        ('android-chrome-192x192.png', 192, False), ('android-chrome-512x512.png', 512, False),
    )
    for name, size, notification in icons:
        (ui / 'public/favicon' / name).write_bytes(make_icon(size, notification))
    png = make_icon(32)
    # ICO directory wraps the exact same PNG mark for browsers requesting /favicon.ico.
    (ui / 'public/favicon/favicon.ico').write_bytes(
        struct.pack('<HHH', 0, 1, 1) + struct.pack('<BBBBHHII', 32, 32, 0, 0, 1, 32, len(png), 22) + png
    )

    # Keep console identity aligned with the current Titan runtime.
    tty = root / 'packages/os/overlay/opt/titan-tty-message/titan-tty-message'
    tty.write_text(re.sub(r'TitanOS [0-9]+\.[0-9]+(?:\.[0-9]+)? (?:is now accessible at:|ist jetzt erreichbar unter:)', f'{version_name} ist jetzt erreichbar unter:', tty.read_text()))
    replace_guarded(tty, 'Your Titan is now accessible at:', f'{version_name} ist jetzt erreichbar unter:')
    motd = root / 'packages/os/overlay/etc/motd'
    motd.write_text(
        f'\n{version_name}\n\n'
        'Der Terminalzugriff dient der Diagnose. Änderungen am Systemabbild\n'
        'bleiben nach einem Neustart nicht erhalten. Nutze für zusätzliche\n'
        'Software eine App oder eine virtuelle Maschine.\n\n'
    )
    for path in ('packages/os/usb-installer/custom-tty', 'packages/os/usb-installer/configuration.nix'):
        source = root / path
        source.write_text(source.read_text().replace('titanOS', 'TitanOS'))
    print(f'Titan branding applied: Titan / {version_name}; runtime compatibility and upstream notices retained.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source', type=Path, help='Pinned Titan source directory')
    args = parser.parse_args()
    apply(args.source)
