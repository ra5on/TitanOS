#!/usr/bin/env python3
"""Verify the presentation patch cannot rename compatibility identifiers."""

import importlib.util
import json
from pathlib import Path
import shutil
import struct
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('configure-branding.py')
spec = importlib.util.spec_from_file_location('branding', SCRIPT)
branding = importlib.util.module_from_spec(spec)
spec.loader.exec_module(branding)
SOURCE = Path(__file__).resolve().parent.parent


class BrandingTests(unittest.TestCase):
    def test_translation_values_protect_external_products_and_urls(self):
        value = 'Your Titan runs titanOS 2.0. Titan Pro and Titan App Store: https://umbrel.com/umbrelos'
        self.assertEqual(
            branding.rebrand_text(value),
            'Your Titan runs TitanOS 2.0. Titan Pro and Titan App Store: https://umbrel.com/umbrelos',
        )
        for value in ('Titan for Mac', 'Titan app', 'Titan Local HTTPS CA', 'titan-local-ca.crt'):
            self.assertEqual(branding.rebrand_text(value), value)
        self.assertEqual(branding.rebrand_text('あなたのtitanOSを更新'), 'あなたのTitanOSを更新')
        self.assertEqual(branding.rebrand_text('/titanOS_marker titanOS_id'), '/titanOS_marker titanOS_id')

    def test_files_and_photos_rebrand_device_but_keep_real_client_names(self):
        self.assertEqual(
            branding.rebrand_text('Titan for Mac findet deinen Titan. Titan Backup'),
            'Titan for Mac findet deinen Titan. Titan Backup',
        )
        for client in ('Titan for iPhone', 'Titan pour iPhone', 'Titan за iPhone', 'iPhone için Titan'):
            self.assertEqual(branding.rebrand_text(f'{client}: Titan'), f'{client}: Titan')

    def test_device_names_with_localized_case_endings(self):
        self.assertEqual(branding.rebrand_text('{{name}} Titanje', 'hu'), '{{name}} Titanja')
        self.assertEqual(branding.rebrand_text('Titaneden / Titannek', 'hu'), 'Titanodon / Titannak')
        self.assertEqual(branding.rebrand_text('Titanis / Titanist', 'et'), 'Titanis / Titanist')
        self.assertEqual(branding.rebrand_text('Titanu / Titana', 'hr'), 'Titanu / Titana')
        self.assertEqual(branding.rebrand_text('Titanom', 'sl'), 'Titanom')
        self.assertEqual(branding.rebrand_text('Titan for Mac / Titanje_id', 'hu'), 'Titan for Mac / Titanje_id')

    def test_generated_favicon_size_and_notification(self):
        icon = branding.make_icon(32)
        self.assertEqual(icon[:8], b'\x89PNG\r\n\x1a\n')
        self.assertEqual(struct.unpack('>II', icon[16:24]), (32, 32))
        self.assertNotEqual(icon, branding.make_icon(32, True))
        self.assertEqual(icon, branding.make_icon(32))

    def test_full_patch_is_idempotent_preserves_keys_and_runtime_names(self):
        paths = (
            'packages/ui/index.html', 'packages/ui/public/locales', 'packages/ui/public/favicon',
            'packages/ui/public/site.webmanifest', 'packages/ui/public/assets/titan-app.svg',
            'packages/ui/src/components/titan-logo.tsx', 'packages/ui/src/components/titan-logo-draw.tsx',
            'packages/ui/src/components/iframe-checker.tsx', 'packages/ui/src/utils/tab-attention.ts',
            'packages/ui/src/routes/whats-new.ts', 'packages/ui/src/routes/settings/advanced.tsx',
            'packages/ui/src/routes/settings/_components/software-update-list-row.tsx',
            'packages/ui/src/routes/settings/mobile/software-update.tsx',
            'packages/ui/src/features/files/components/shared/cloud-constellation.tsx',
            'packages/ui/src/features/files/components/cloud-break-diagram.tsx',
            'packages/ui/src/features/photos/components/sources/source-icon.tsx',
            'packages/ui/src/hooks/use-is-home-or-pro.ts',
            'packages/ui/src/features/files/components/listing/search-listing/index.tsx',
            'packages/os/overlay/opt/titan-tty-message/titan-tty-message',
            'packages/os/overlay/etc/motd', 'packages/os/overlay/titanOS',
            'packages/os/usb-installer/custom-tty', 'packages/os/usb-installer/configuration.nix',
            'LICENSE.md',
        )
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            for relative in paths:
                source, dest = SOURCE / relative, root / relative
                dest.parent.mkdir(parents=True, exist_ok=True)
                if source.is_dir():
                    shutil.copytree(source, dest)
                else:
                    shutil.copy2(source, dest)
            (root / '.titan').mkdir()
            (root / '.titan/release.json').write_text(json.dumps({'versionName': 'TitanOS 2.0.1'}))
            english = root / 'packages/ui/public/locales/en.json'
            original_translations = json.loads(english.read_text())
            original_keys = set(original_translations)
            license_before = (root / 'LICENSE.md').read_bytes()
            advanced_before = (root / 'packages/ui/src/routes/settings/advanced.tsx').read_bytes()
            branding.apply(root)
            snapshot = {str(path.relative_to(root)): path.read_bytes() for path in root.rglob('*') if path.is_file()}
            branding.apply(root)
            self.assertEqual(snapshot, {str(path.relative_to(root)): path.read_bytes() for path in root.rglob('*') if path.is_file()})
            self.assertIn('TitanOS 2.0.1', (root / 'packages/ui/index.html').read_text())
            self.assertIn('TitanOS 2.0.1', (root / 'packages/os/overlay/etc/motd').read_text())
            self.assertIn('TitanOS 2.0.1', (root / 'packages/ui/src/routes/whats-new.ts').read_text())
            translations = json.loads(english.read_text())
            self.assertEqual(original_keys, set(translations))
            self.assertEqual(translations['titanos'], 'TitanOS')
            self.assertEqual(translations['titan'], 'Titan')
            self.assertEqual(translations['files-type.titan-backup'], 'Titan Backup')
            self.assertEqual(
                translations['whats-new-titanos-2-0.photos-description'],
                original_translations['whats-new-titanos-2-0.photos-description'],
            )
            german = json.loads((root / 'packages/ui/public/locales/de.json').read_text())
            self.assertEqual(german['titanos'], 'TitanOS')
            self.assertEqual(license_before, (root / 'LICENSE.md').read_bytes())
            self.assertTrue((root / 'packages/os/overlay/titanOS').exists())
            self.assertEqual(advanced_before, (root / 'packages/ui/src/routes/settings/advanced.tsx').read_bytes())
            self.assertIn('titan login:', (root / 'packages/os/overlay/opt/titan-tty-message/titan-tty-message').read_text())


if __name__ == '__main__':
    unittest.main()
