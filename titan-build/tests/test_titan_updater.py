"""Real detached Ed25519 checks and fail-closed local Rugix installation."""
import contextlib
import hashlib
import importlib.util
import importlib.machinery
import io
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('titan_updater', ROOT/'updater/titan-system-update.py')
updater = importlib.util.module_from_spec(spec); spec.loader.exec_module(updater)
spec = importlib.util.spec_from_file_location('titan_configure_updater', ROOT/'configure-updater.py')
configure = importlib.util.module_from_spec(spec); spec.loader.exec_module(configure)


@unittest.skipUnless(shutil.which('openssl'), 'openssl required')
class SignedUpdaterTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(); self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.private, self.public = self.root/'private.pem', self.root/'public.pem'
        subprocess.run(['openssl','genpkey','-algorithm','ED25519','-out',str(self.private)],check=True,capture_output=True)
        subprocess.run(['openssl','pkey','-in',str(self.private),'-pubout','-out',str(self.public)],check=True,capture_output=True)
        self.version, self.current = '2.0.0', '2.0.0-titan.3'
        self.payload = b'A disposable signed Rugix bundle fixture'
        self.name = updater.update_asset_name(self.version)
        self.release = {'version':self.version,'osVersion':self.version,'stage':'stable',
                        'systemCompatibility':updater.COMPATIBILITY,'architecture':'amd64','versionName':'TitanOS 2.0.0'}
        self.asset = {'name':self.name,'sizeBytes':len(self.payload),'sha256':hashlib.sha256(self.payload).hexdigest()}
        self.manifest = {'schemaVersion':1,'releaseVersion':self.version,'osVersion':self.version,
                         'architecture':'amd64','firmware':'UEFI','updateFormat':'rugix','stage':'stable',
                         'systemCompatibility':updater.COMPATIBILITY,'ownTitanUpdateChannel':True,'releaseEligible':True,
                         'compatibleWithPreviousTitanRaucImages':False,
                         'imageVerification':{'structuralCheck':'passed','uefiHttpSmoke':{'status':'passed'}},'assets':[self.asset]}
        self.github = {'tag_name':'v'+self.version,'draft':False,'prerelease':False,
                       'assets':[{'name':self.name,'size':len(self.payload),'browser_download_url':updater.asset_url(self.version,self.name)}]}
        self.responses = {}
        self.sign()

    def sign(self):
        files = {'release.json':json.dumps(self.release).encode(), 'build-manifest.json':json.dumps(self.manifest).encode(),self.name:self.payload}
        sums = ''.join(hashlib.sha256(value).hexdigest()+'  '+name+'\n' for name,value in sorted(files.items())).encode()
        path = self.root/'SHA256SUMS'; path.write_bytes(sums)
        signature = subprocess.check_output(['openssl','pkeyutl','-sign','-rawin','-inkey',str(self.private),'-in',str(path)])
        self.responses = {updater.asset_url(self.version,name):value for name,value in {**files,'SHA256SUMS':sums,'SHA256SUMS.sig':signature}.items()}

    def verified(self, channel='stable'):
        with patch.object(updater,'PUBLIC_KEY',self.public), patch.object(updater,'fetch_bytes',side_effect=lambda url,maximum:self.responses[url]):
            return updater.verify_release(self.version,channel,self.github)

    def test_real_signature_accepts_exact_manifest_and_local_bundle_descriptor(self):
        result = self.verified()
        self.assertEqual(result['version'],self.version); self.assertEqual(result['name'],'TitanOS 2.0.0')
        self.assertEqual(result['asset']['sha256'],hashlib.sha256(self.payload).hexdigest())

    def test_installed_legacy_helper_accepts_signed_stable_bridge_then_new_helper_offers_canonical(self):
        source = ROOT/'tests/fixtures/legacy-titan-2-updater.py.txt'
        loader = importlib.machinery.SourceFileLoader('legacy_titan_2_updater', str(source))
        spec = importlib.util.spec_from_loader(loader.name, loader)
        legacy = importlib.util.module_from_spec(spec); loader.exec_module(legacy)
        # This immutable fixture is the actual updater shipped in titan.2, not
        # a reimplementation of its version or signature policy.
        with self.assertRaises(legacy.UpdateError): legacy.version_key('2.0.0')
        self.version, self.current = '2.0.0-titan.3', '2.0.0-titan.2'
        self.name = updater.update_asset_name(self.version)
        self.asset['name'] = self.name
        self.release.update(version=self.version, osVersion=self.version, legacyUpdateBridgeTo='2.0.0')
        self.manifest.update(releaseVersion=self.version, osVersion=self.version)
        self.github.update(tag_name='v'+self.version, prerelease=False,
                           assets=[{'name':self.name,'size':len(self.payload),'browser_download_url':legacy.asset_url(self.version,self.name)}])
        # The old NAS still requests the original repository. Keep the fixture
        # immutable and serve the bridge from that exact signed old-feed URL.
        with patch.object(updater, 'REPOSITORY', legacy.REPOSITORY):
            self.sign()
        self.responses[f'https://api.github.com/repos/{legacy.REPOSITORY}/releases?per_page=100'] = json.dumps([self.github]).encode()
        with patch.object(legacy, 'PUBLIC_KEY', self.public), patch.object(legacy, 'installed', return_value=self.release), \
                patch.object(legacy, 'fetch_bytes', side_effect=lambda url,maximum:self.responses[url]):
            offered = legacy.latest(self.current, 'stable')
        self.assertEqual(offered['version'], '2.0.0-titan.3')
        self.assertEqual(offered['asset']['sha256'], self.asset['sha256'])
        self.assertGreater(updater.version_key('2.0.0'), updater.version_key(offered['version']))
        self.assertGreater(updater.version_key('2.0.1'), updater.version_key('2.0.0'))

        # After installing a signed transition, the new helper really queries
        # TitanOS and verifies its canonical release, rather than just sorting
        # version strings. Identical trust and disk-layout gates still apply.
        self.version, self.current = '2.0.1', '2.0.0'
        self.name = updater.update_asset_name(self.version)
        self.asset['name'] = self.name
        self.release.pop('legacyUpdateBridgeTo', None)
        self.release.update(version=self.version, osVersion=self.version)
        self.manifest.update(releaseVersion=self.version, osVersion=self.version)
        self.github.update(tag_name='v'+self.version, assets=[{'name':self.name,'size':len(self.payload),'browser_download_url':updater.asset_url(self.version,self.name)}])
        self.sign()
        self.responses[f'https://api.github.com/repos/{updater.REPOSITORY}/releases?per_page=100'] = json.dumps([self.github]).encode()
        with patch.object(updater, 'PUBLIC_KEY', self.public), patch.object(updater, 'installed', return_value=self.release), \
                patch.object(updater, 'fetch_bytes', side_effect=lambda url,maximum:self.responses[url]):
            next_release = updater.latest(self.current, 'stable')
        self.assertEqual(updater.REPOSITORY, 'ra5on/TitanOS')
        self.assertEqual(next_release['version'], '2.0.1')
        self.assertEqual(next_release['asset']['sha256'], hashlib.sha256(self.payload).hexdigest())
        self.assertIn('/ra5on/TitanOS/', updater.asset_url(self.version,self.name))

    def test_canonical_order_and_rejected_downgrades_cover_legacy_transition(self):
        order = ['1.9.9', '2.0.0-titan.2', '2.0.0-titan.3', '2.0.0', '2.0.1', '2.1.0', '2.10.0']
        self.assertEqual(sorted(reversed(order), key=updater.version_key), order)
        for value in ('2.0.0-beta.1', '2.0.0-alpha.1', '02.0.0', 'v2.0.0', '2.0.0+titan.1', '2.0.0-titan.0'):
            with self.subTest(value=value), self.assertRaises(updater.UpdateError): updater.version_key(value)
        for current, requested in [('2.0.0', '2.0.0-titan.3'), ('2.0.1', '2.0.0'), ('2.0.0', '2.0.0')]:
            with self.subTest(current=current, requested=requested), patch.object(updater,'latest') as latest, self.assertRaises(updater.UpdateError):
                updater.install(current, 'stable', requested)
            latest.assert_not_called()

    def test_clean_asset_names_keep_amd64_compatibility_only_for_legacy_bridges(self):
        self.assertEqual(updater.update_asset_name('2.0.0'), 'titan-2.0.0.update')
        self.assertEqual(updater.update_asset_name('2.0.1'), 'titan-2.0.1.update')
        self.assertEqual(updater.update_asset_name('2.0.0-titan.3'), 'titan-2.0.0-titan.3-amd64.update')
        # Removing an architecture suffix never removes the signed architecture
        # gate. A correctly signed ARM image must still be rejected.
        self.manifest['architecture']='arm64'; self.release['architecture']='arm64'; self.sign()
        with self.assertRaises(updater.UpdateError): self.verified()

    def test_signature_checksum_and_signed_json_tampering_are_rejected(self):
        for name in ('SHA256SUMS','SHA256SUMS.sig','build-manifest.json','release.json'):
            with self.subTest(name=name):
                self.sign(); url=updater.asset_url(self.version,name)
                value=bytearray(self.responses[url]); value[0]^=1; self.responses[url]=bytes(value)
                with self.assertRaises(updater.UpdateError): self.verified()

    def test_wrong_public_key_cannot_authenticate_release(self):
        foreign=self.root/'foreign.pem'
        subprocess.run(['openssl','genpkey','-algorithm','ED25519','-out',str(foreign)],check=True,capture_output=True)
        foreign_public=self.root/'foreign-public.pem'
        subprocess.run(['openssl','pkey','-in',str(foreign),'-pubout','-out',str(foreign_public)],check=True,capture_output=True)
        with patch.object(self,'public',foreign_public), self.assertRaises(updater.UpdateError): self.verified()

    def test_signed_incompatible_unbooted_or_old_titan_image_is_not_offered(self):
        for changes in ({'architecture':'arm64'},{'updateFormat':'rauc'},{'systemCompatibility':'old-titan-debian'},
                        {'ownTitanUpdateChannel':False},{'releaseEligible':False},{'osVersion':'2.0.1'},
                        {'imageVerification':{'structuralCheck':'passed','uefiHttpSmoke':{'status':'failed'}}}):
            with self.subTest(changes=changes):
                previous=dict(self.manifest); self.manifest.update(changes); self.sign()
                with self.assertRaises(updater.UpdateError): self.verified()
                self.manifest=previous

    def test_only_stable_is_allowed_and_bound_to_signed_metadata(self):
        for channel in ('alpha', 'beta'):
            with self.subTest(channel=channel), self.assertRaises(updater.UpdateError): self.verified(channel)
        self.release['stage']='alpha'; self.manifest['stage']='alpha'; self.sign()
        with self.assertRaises(updater.OtherChannel): self.verified('stable')

    def test_stale_foreign_draft_and_wrong_channel_github_releases_are_skipped(self):
        releases=[{**self.github,'tag_name':'v'+self.current},{**self.github,'draft':True},
                  {**self.github,'tag_name':'v0.5.3-alpha.1'},{**self.github,'prerelease':True}]
        local={**self.release,'version':self.current}
        with patch.object(updater,'installed',return_value=local), patch.object(updater,'fetch_bytes',return_value=json.dumps(releases).encode()), patch.object(updater,'verify_release') as verify:
            self.assertEqual(updater.latest(self.current,'stable')['version'],self.current)
            verify.assert_not_called()

    def test_check_ignores_verified_other_channel_without_false_error(self):
        with patch.object(updater,'installed',return_value=self.release), patch.object(updater,'fetch_bytes',return_value=json.dumps([self.github]).encode()), patch.object(updater,'verify_release',side_effect=updater.OtherChannel('beta')):
            self.assertEqual(updater.latest(self.current,'stable')['version'],self.current)

    def test_authenticated_other_channel_metadata_does_not_hide_stable_release(self):
        releases=[{**self.github,'tag_name':'v2.0.'+str(number)} for number in range(0,12)]
        def verified(version,channel,release):
            if version == self.version: return {'version':version,'stage':'stable'}
            raise updater.OtherChannel('Authenticated alpha release')
        with patch.object(updater,'installed',return_value=self.release), patch.object(updater,'fetch_bytes',return_value=json.dumps(releases).encode()), patch.object(updater,'verify_release',side_effect=verified) as verify:
            self.assertEqual(updater.latest(self.current,'stable')['version'],self.version)
            self.assertEqual(verify.call_count,12)

    def test_invalid_signature_budget_is_bounded_and_fails_closed(self):
        releases=[{**self.github,'tag_name':'v2.0.'+str(number)} for number in range(1,29)]
        with patch.object(updater,'installed',return_value=self.release), patch.object(updater,'fetch_bytes',return_value=json.dumps(releases).encode()), patch.object(updater,'verify_release',side_effect=updater.UpdateError('Invalid signature')) as verify:
            with self.assertRaises(updater.UpdateError): updater.latest(self.current,'stable')
            self.assertEqual(verify.call_count,10)

    def test_safe_filenames_and_duplicate_checksum_entries_are_required(self):
        for text in ('a'*64+'  ../update\n', 'a'*64+'  file\n'+'b'*64+'  file\n', 'a'*64+'  /etc/config\n'):
            with self.subTest(text=text), self.assertRaises(updater.UpdateError): updater.checksum_entries(text.encode())
        with self.assertRaises(updater.UpdateError): updater.decode_json(b'{"stage":"alpha","stage":"stable"}')
        for name in ('../script','-option','x/evil','file\n'):
            with self.subTest(name=name), self.assertRaises(updater.UpdateError): updater.asset_url(self.version,name)

    @contextlib.contextmanager
    def install_fixture(self, payload=None, boot='grub'):
        descriptor=self.verified()
        calls=[]
        def installed(args,**kwargs):
            self.assertEqual(args[:2],['/usr/bin/rugix-ctrl','update'])
            self.assertEqual(Path(args[-1]).read_bytes(),self.payload)
            self.assertTrue(Path(args[-1]).is_relative_to(self.root))
            calls.append(args)
        with patch.object(updater,'latest',return_value=descriptor), patch.object(updater,'staging_directory',return_value=self.root), \
                patch.object(updater.subprocess,'check_output',return_value=json.dumps({'boot':{'bootFlow':boot}}).encode()), \
                patch.object(updater.subprocess,'run',side_effect=installed), \
                patch.object(updater,'response',return_value=io.BytesIO(self.payload if payload is None else payload)), \
                contextlib.redirect_stdout(io.StringIO()):
            yield calls

    def test_verified_local_file_is_only_input_to_native_rugix_installer(self):
        with self.install_fixture() as calls: updater.install(self.current,'stable',self.version)
        self.assertEqual(calls[0][:-1],['/usr/bin/rugix-ctrl','update','install','--reboot','set','--insecure-skip-bundle-verification'])
        self.assertFalse(any('https://' in value for value in calls[0]))
        self.assertEqual(list(self.root.glob('release-*')),[])

    def test_modified_truncated_oversized_bundle_never_reaches_rugix(self):
        for payload in (b'wrong bundle',self.payload[:-1],self.payload+b' extra'):
            with self.subTest(payload=payload), self.install_fixture(payload) as calls:
                with self.assertRaises(updater.UpdateError): updater.install(self.current,'stable',self.version)
                self.assertEqual(calls,[])
        self.assertEqual(list(self.root.glob('release-*')),[])

    def test_non_native_boot_and_non_newer_release_never_install(self):
        for boot in ('mender-grub','rpi-tryboot','unknown'):
            with self.subTest(boot=boot), self.install_fixture(boot=boot) as calls:
                with self.assertRaises(updater.UpdateError): updater.install(self.current,'stable',self.version)
                self.assertEqual(calls,[])
        for version in (self.current,'1.0.0-titan.1'):
            with self.subTest(version=version), patch.object(updater,'latest') as latest, self.assertRaises(updater.UpdateError):
                updater.install(self.current,'stable',version)
            latest.assert_not_called()

    def test_missing_persistent_data_mount_fails_before_staging(self):
        with patch.object(updater.subprocess,'check_output',return_value=b'{"filesystems":[{"target":"/","fstype":"ext4","options":"rw"}]}'), patch.object(updater,'STAGING',self.root/'missing'):
            with self.assertRaises(updater.UpdateError): updater.staging_directory()
            self.assertFalse((self.root/'missing').exists())


class ConfigureUpdaterTests(unittest.TestCase):
    def test_configuration_is_idempotent_keeps_internal_compatibility_and_has_no_cloud_script(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            def write(name,text):
                path=root/name; path.parent.mkdir(parents=True,exist_ok=True); path.write_text(text)
            write('.titan/release.json',json.dumps({'version':'2.0.0','osVersion':'2.0.0','stage':'stable','systemCompatibility':updater.COMPATIBILITY,'versionName':'TitanOS 2.0.0'}))
            write('.titan/release-public.pem','public key fixture')
            write('packages/umbreld/package.json','{"version":"2.0.0","versionName":"umbrelOS 2.0"}')
            write('packages/umbreld/source/modules/system/routes.ts',"channel: z.enum(['stable', 'beta'])\nreturn (await ctx.umbreld.store.get('settings.releaseChannel')) || 'stable'")
            write('packages/umbreld/source/index.ts',"releaseChannel: 'stable' | 'beta'\nif (!(await this.store.get('settings.releaseChannel'))) {\nawait this.store.set('settings.releaseChannel', this.version.includes('-beta') ? 'beta' : 'stable')\n}")
            write('packages/os/overlay/etc/hostname','umbrel')
            configure.configure(root); configure.configure(root)
            package=json.loads((root/'packages/umbreld/package.json').read_text())
            self.assertEqual(package['version'],'2.0.0');self.assertEqual(package['versionName'],'TitanOS 2.0.0')
            self.assertIn("await this.store.set('settings.releaseChannel', 'stable')",(root/'packages/umbreld/source/index.ts').read_text())
            self.assertEqual((root/'packages/os/overlay/etc/hostname').read_text(),'umbrel')
            source=(root/'packages/umbreld/source/modules/system/update.ts').read_text()
            for unwanted in ('api.umbrel.com','updateScript','bash -c','fetch('): self.assertNotIn(unwanted,source)
            self.assertIn('5 * 60 * 1000',source)
            self.assertTrue((root/'packages/os/overlay/usr/libexec/titan-system-update.py').exists())


if __name__=='__main__': unittest.main()
