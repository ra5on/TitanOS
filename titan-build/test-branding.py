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
        value = 'Your Umbrel runs umbrelOS 2.0. Umbrel Pro and Umbrel App Store: https://umbrel.com/umbrelos'
        self.assertEqual(
            branding.rebrand_text(value),
            'Your Titan runs TitanOS 2.0. Umbrel Pro and Umbrel App Store: https://umbrel.com/umbrelos',
        )
        for value in ('Umbrel for Mac', 'Umbrel app', 'Umbrel Local HTTPS CA', 'umbrel-local-ca.crt'):
            self.assertEqual(branding.rebrand_text(value), value)
        self.assertEqual(branding.rebrand_text('あなたのumbrelOSを更新'), 'あなたのTitanOSを更新')
        self.assertEqual(branding.rebrand_text('/umbrelOS_marker umbrelOS_id'), '/umbrelOS_marker umbrelOS_id')

    def test_files_and_photos_rebrand_device_but_keep_real_client_names(self):
        self.assertEqual(
            branding.rebrand_text('Umbrel for Mac findet deinen Umbrel. Umbrel Backup'),
            'Umbrel for Mac findet deinen Titan. Titan Backup',
        )
        for client in ('Umbrel for iPhone', 'Umbrel pour iPhone', 'Umbrel за iPhone', 'iPhone için Umbrel'):
            self.assertEqual(branding.rebrand_text(f'{client}: Umbrel'), f'{client}: Titan')

    def test_device_names_with_localized_case_endings(self):
        self.assertEqual(branding.rebrand_text('{{name}} Umbrelje', 'hu'), '{{name}} Titanja')
        self.assertEqual(branding.rebrand_text('Umbreleden / Umbrelnek', 'hu'), 'Titanodon / Titannak')
        self.assertEqual(branding.rebrand_text('Umbrelis / Umbrelist', 'et'), 'Titanis / Titanist')
        self.assertEqual(branding.rebrand_text('Umbrelu / Umbrela', 'hr'), 'Titanu / Titana')
        self.assertEqual(branding.rebrand_text('Umbrelom', 'sl'), 'Titanom')
        self.assertEqual(branding.rebrand_text('Umbrel for Mac / Umbrelje_id', 'hu'), 'Umbrel for Mac / Umbrelje_id')

    def test_generated_favicon_size_and_notification(self):
        icon = branding.make_icon(32)
        self.assertEqual(icon[:8], b'\x89PNG\r\n\x1a\n')
        self.assertEqual(struct.unpack('>II', icon[16:24]), (32, 32))
        self.assertNotEqual(icon, branding.make_icon(32, True))
        self.assertEqual(icon, branding.make_icon(32))

    def test_full_patch_is_idempotent_preserves_keys_and_runtime_names(self):
        paths = (
            'packages/ui/index.html', 'packages/ui/public/locales', 'packages/ui/public/favicon',
            'packages/ui/public/site.webmanifest', 'packages/ui/public/assets/umbrel-app.svg',
            'packages/ui/src/components/umbrel-logo.tsx', 'packages/ui/src/components/umbrel-logo-draw.tsx',
            'packages/ui/src/components/iframe-checker.tsx', 'packages/ui/src/utils/tab-attention.ts',
            'packages/ui/src/routes/whats-new.ts', 'packages/ui/src/routes/settings/advanced.tsx',
            'packages/ui/src/routes/settings/_components/software-update-list-row.tsx',
            'packages/ui/src/routes/settings/mobile/software-update.tsx',
            'packages/ui/src/features/files/components/shared/cloud-constellation.tsx',
            'packages/ui/src/features/files/components/cloud-break-diagram.tsx',
            'packages/ui/src/features/photos/components/sources/source-icon.tsx',
            'packages/ui/src/hooks/use-is-home-or-pro.ts',
            'packages/ui/src/features/files/components/listing/search-listing/index.tsx',
            'packages/os/overlay/opt/umbrel-tty-message/umbrel-tty-message',
            'packages/os/overlay/etc/motd', 'packages/os/overlay/umbrelOS',
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
            self.assertEqual(translations['umbrelos'], 'TitanOS')
            self.assertEqual(translations['umbrel'], 'Titan')
            self.assertEqual(translations['desktop.welcome.files.mac-description'], original_translations['desktop.welcome.files.mac-description'])
            self.assertEqual(translations['files-type.umbrel-backup'], 'Titan Backup')
            self.assertEqual(translations['photos-phone-backup.title'], 'Your phone-to-Titan magic')
            self.assertEqual(
                translations['files-share.instructions.macos.app-description'],
                'Umbrel for Mac automatically finds your Titan and gives you access to your shared folders in Finder.',
            )
            self.assertEqual(
                translations['whats-new-umbrelos-2-0.photos-description'],
                'Photos and videos on your Titan show up in the new Photos app automatically. '
                'And with the new Umbrel for iPhone app, your entire camera roll backs up to your Titan on its own.',
            )
            german = json.loads((root / 'packages/ui/public/locales/de.json').read_text())
            self.assertEqual(german['photos-phone-backup.title'], 'Die Magie zwischen deinem Smartphone und Titan')
            self.assertIn('Umbrel for Mac findet deinen Titan', german['files-share.instructions.macos.app-description'])
            self.assertEqual(license_before, (root / 'LICENSE.md').read_bytes())
            self.assertTrue((root / 'packages/os/overlay/umbrelOS').exists())
            self.assertEqual(advanced_before, (root / 'packages/ui/src/routes/settings/advanced.tsx').read_bytes())
            self.assertIn('umbrel login:', (root / 'packages/os/overlay/opt/umbrel-tty-message/umbrel-tty-message').read_text())


if __name__ == '__main__':
    unittest.main()
