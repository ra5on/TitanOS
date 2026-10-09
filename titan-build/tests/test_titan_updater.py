"""Real detached Ed25519 checks and fail-closed local Rugix installation."""
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import shutil
import subprocess
import stat
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
        self.version, self.current = '2.0.2', '2.0.1'
        self.payload = b'A disposable signed Rugix bundle fixture'
        self.name = updater.update_asset_name(self.version)
        self.release = {'version':self.version,'osVersion':self.version,'stage':'stable',
                        'systemCompatibility':updater.COMPATIBILITY,'architecture':'amd64','versionName':'TitanOS 2.0.2'}
        self.asset = {'name':self.name,'sizeBytes':len(self.payload),'sha256':hashlib.sha256(self.payload).hexdigest()}
        self.image_name=f'titan-{self.version}.img.xz'
        self.image_payload=b'Fixture image'
        self.image_asset={'name':self.image_name,'sizeBytes':len(self.image_payload),'sha256':hashlib.sha256(self.image_payload).hexdigest()}
        self.manifest = {'schemaVersion':1,'releaseVersion':self.version,'osVersion':self.version,
                         'architecture':'amd64','firmware':'UEFI','updateFormat':'rugix','stage':'stable',
                         'systemCompatibility':updater.COMPATIBILITY,'ownTitanUpdateChannel':True,'releaseEligible':True,
                         'imageVerification':{'structuralCheck':'passed','uefiHttpSmoke':{'status':'passed','installedRelease':{'version':self.version,'name':f'TitanOS {self.version}'},'bootDiskSizeBytes':32*1024**3}},'assets':[self.asset,self.image_asset]}
        self.github = {'tag_name':'v'+self.version,'draft':False,'prerelease':False,
                       'assets':[{'name':self.name,'size':len(self.payload),'browser_download_url':updater.asset_url(self.version,self.name)}]}
        self.responses = {}
        self.sign()

    def sign(self):
        files = {'release.json':json.dumps(self.release).encode(), 'build-manifest.json':json.dumps(self.manifest).encode(),self.name:self.payload,self.image_name:self.image_payload}
        sums = ''.join(hashlib.sha256(value).hexdigest()+'  '+name+'\n' for name,value in sorted(files.items())).encode()
        path = self.root/'SHA256SUMS'; path.write_bytes(sums)
        signature = subprocess.check_output(['openssl','pkeyutl','-sign','-rawin','-inkey',str(self.private),'-in',str(path)])
        self.responses = {updater.asset_url(self.version,name):value for name,value in {**files,'SHA256SUMS':sums,'SHA256SUMS.sig':signature}.items()}

    def verified(self, channel='stable'):
        with patch.object(updater,'PUBLIC_KEY',self.public), patch.object(updater,'fetch_bytes',side_effect=lambda url,maximum:self.responses[url]):
            return updater.verify_release(self.version,channel,self.github)

    def test_real_signature_accepts_exact_manifest_and_local_bundle_descriptor(self):
        result = self.verified()
        self.assertEqual(result['version'],self.version); self.assertEqual(result['name'],'TitanOS 2.0.2')
        self.assertEqual(result['asset']['sha256'],hashlib.sha256(self.payload).hexdigest())

    def test_latest_fetches_only_the_new_repository_and_verifies_its_signed_release(self):
        self.responses[f'https://api.github.com/repos/{updater.REPOSITORY}/releases?per_page=100']=json.dumps([self.github]).encode()
        with patch.object(updater,'PUBLIC_KEY',self.public), patch.object(updater,'installed',return_value={**self.release,'version':self.current}), patch.object(updater,'fetch_bytes',side_effect=lambda url,maximum:self.responses[url]):
            offered=updater.latest(self.current,'stable')
        self.assertEqual(updater.REPOSITORY,'ra5on/TitanOS')
        self.assertEqual(offered['version'],self.version)
        self.assertIn('/ra5on/TitanOS/',offered['asset']['url'])

    def test_numeric_version_order_and_downgrades_are_checked_before_install(self):
        order = ['1.9.9', '2.0.0', '2.0.1', '2.0.2', '2.1.0', '2.10.0']
        self.assertEqual(sorted(reversed(order), key=updater.version_key), order)
        for value in ('2.0.0-beta.1', '2.0.0-alpha.1', '02.0.0', 'v2.0.0', '2.0.0+titan.1', '2.0.0-titan.0', '2.0.0-titan.2', '2.0.0-titan.3'):
            with self.subTest(value=value), self.assertRaises(updater.UpdateError): updater.version_key(value)
        for current, requested in [('2.0.2', '2.0.1'), ('2.0.1', '2.0.0'), ('2.0.1', '2.0.1')]:
            with self.subTest(current=current, requested=requested), patch.object(updater,'latest') as latest, self.assertRaises(updater.UpdateError):
                updater.install(current, 'stable', requested)
            latest.assert_not_called()

    def test_clean_asset_names_keep_the_signed_amd64_gate(self):
        self.assertEqual(updater.update_asset_name('2.0.0'), 'titan-2.0.0.update')
        self.assertEqual(updater.update_asset_name('2.0.1'), 'titan-2.0.1.update')
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
        for changes in ({'architecture':'arm64'},{'updateFormat':'rauc'},{'systemCompatibility':'titan-umbrel-rugix-amd64-v1'},
                        {'ownTitanUpdateChannel':False},{'releaseEligible':False},{'osVersion':self.current},
                        {'imageVerification':{'structuralCheck':'passed','uefiHttpSmoke':{'status':'failed'}}}):
            with self.subTest(changes=changes):
                previous=dict(self.manifest); self.manifest.update(changes); self.sign()
                with self.assertRaises(updater.UpdateError): self.verified()
                self.manifest=previous

    def test_only_stable_is_allowed_and_bound_to_signed_metadata(self):
        for channel in ('alpha', 'beta'):
            with self.subTest(channel=channel), self.assertRaises(updater.UpdateError): self.verified(channel)
        self.release['stage']='alpha'; self.manifest['stage']='alpha'; self.sign()
        with self.assertRaises(updater.UpdateError): self.verified('stable')

    def test_fresh_install_identity_rejects_previous_disk_layout_before_network_access(self):
        identity={**self.release,'version':self.current,'osVersion':self.current,
                  'systemCompatibility':'titan-umbrel-rugix-amd64-v1'}
        with patch.object(updater,'protected_read',return_value=json.dumps(identity).encode()), patch.object(updater,'fetch_bytes') as fetch:
            with self.assertRaises(updater.UpdateError): updater.latest(self.current,'stable')
            fetch.assert_not_called()

    def test_genuinely_signed_consistent_previous_layout_cannot_be_installed(self):
        self.manifest['systemCompatibility']='titan-umbrel-rugix-amd64-v1'
        self.release['systemCompatibility']='titan-umbrel-rugix-amd64-v1'
        self.sign()
        with self.assertRaises(updater.UpdateError): self.verified()

    def test_genuinely_signed_wrong_boot_version_disk_or_asset_list_is_rejected(self):
        previous=json.loads(json.dumps(self.manifest))
        changes=[{'imageVerification':{'structuralCheck':'passed','uefiHttpSmoke':{'status':'passed',
                      'installedRelease':{'version':self.current,'name':f'TitanOS {self.current}'},'bootDiskSizeBytes':32*1024**3}}},
                 {'imageVerification':{'structuralCheck':'passed','uefiHttpSmoke':{'status':'passed',
                      'installedRelease':{'version':self.version,'name':f'TitanOS {self.version}'},'bootDiskSizeBytes':8*1024**3}}},
                 {'assets':[self.asset]}, {'assets':[self.asset,{**self.image_asset,'name':['invalid']}]}]
        for change in changes:
            with self.subTest(change=change):
                self.manifest={**previous,**change}; self.sign()
                with self.assertRaises(updater.UpdateError): self.verified()

    def test_stale_foreign_draft_and_wrong_channel_github_releases_are_skipped(self):
        releases=[{**self.github,'tag_name':'v'+self.current},{**self.github,'draft':True},
                  {**self.github,'tag_name':'v0.5.3-alpha.1'},{**self.github,'prerelease':True}]
        local={**self.release,'version':self.current}
        with patch.object(updater,'installed',return_value=local), patch.object(updater,'fetch_bytes',return_value=json.dumps(releases).encode()), patch.object(updater,'verify_release') as verify:
            self.assertEqual(updater.latest(self.current,'stable')['version'],self.current)
            verify.assert_not_called()

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
        info={'boot':{'bootFlow':boot,'activeGroup':'a','defaultGroup':'a','groups':{'a':{},'b':{}}},'state':{'status':'Active'},
              'slots':{name:{'active':name.endswith('-a'),'hashes':{'sha256':'a'*64},'size':4096,'updatedAt':'2026-10-07T00:00:00Z'}
                       for name in ('boot-a','system-a','boot-b','system-b')}}
        with patch.object(updater,'latest',return_value=descriptor), patch.object(updater,'staging_directory',return_value=self.root), \
                patch.object(updater,'remember_current'), patch.object(updater,'sync_menu'), \
                patch.object(updater.subprocess,'check_output',return_value=json.dumps(info).encode()), \
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
        for version in (self.current,'1.0.0'):
            with self.subTest(version=version), patch.object(updater,'latest') as latest, self.assertRaises(updater.UpdateError):
                updater.install(self.current,'stable',version)
            latest.assert_not_called()

    def test_missing_persistent_data_mount_fails_before_staging(self):
        with patch.object(updater.subprocess,'check_output',return_value=b'{"filesystems":[{"target":"/","fstype":"ext4","options":"rw"}]}'), patch.object(updater,'STAGING',self.root/'missing'):
            with self.assertRaises(updater.UpdateError): updater.staging_directory()
            self.assertFalse((self.root/'missing').exists())

    def descriptor_for(self, version):
        original={name:getattr(self,name) for name in ('version','release','manifest','name','asset','image_name','image_asset','github')}
        previous=self.version
        try:
            for name,value in original.items():
                setattr(self,name,json.loads(json.dumps(value).replace(previous,version)))
            self.sign()
            return self.verified()
        finally:
            for name,value in original.items(): setattr(self,name,value)
            self.sign()

    @contextlib.contextmanager
    def recovery_fixture(self, pending=False):
        current=self.version
        info={'boot':{'bootFlow':'grub','activeGroup':'a','defaultGroup':'a','groups':{'a':{},'b':{}}},
              'state':{'status':'Active'},'slots':{}}
        for group in ('a','b'):
            for kind in ('boot','system'):
                info['slots'][f'{kind}-{group}']={'active':group=='a','hashes':{'sha256':group*64},
                    'size':4096,'updatedAt':'2026-10-07T00:00:00Z'}
        journal=updater.empty_journal()
        for group,version in (('a',current),('b',self.current)):
            descriptor=self.descriptor_for(version)
            updater.store_evidence(self.root,descriptor)
            journal['slots'][group]={'version':version,'name':descriptor['name'],'confirmed':True,
                                    'snapshot':updater.slot_snapshot(info,group)}
        if pending: journal['pending']={'type':'update','slot':'a','version':current}
        updater.atomic_json(self.root/'slots.json',journal)
        with patch.object(updater,'PUBLIC_KEY',self.public), patch.object(updater,'installed'), \
             patch.object(updater,'protected_read',side_effect=lambda path,maximum:Path(path).read_bytes()), \
             patch.object(updater,'staging_directory',return_value=self.root), \
             patch.object(updater,'system_info',side_effect=lambda:json.loads(json.dumps(info))):
            yield current,info,journal

    def test_fresh_or_factory_cloned_slots_do_not_infer_a_previous_version(self):
        with self.recovery_fixture() as (current,info,journal), patch.object(updater,'fetch_bytes') as fetch:
            updater.atomic_json(self.root/'slots.json',updater.empty_journal())
            state=updater.recovery_status(current)
            self.assertEqual(state['previous'],[])
            self.assertIn('Noch kein',state['reason'])
            fetch.assert_not_called()

    def test_confirmed_signed_previous_slot_is_available_offline_and_selection_is_bound(self):
        with self.recovery_fixture() as (current,info,journal), patch.object(updater,'fetch_bytes') as fetch:
            state=updater.recovery_status(current)
            self.assertEqual(state['current']['version'],current)
            self.assertEqual(state['previous'][0]['version'],self.current)
            self.assertEqual(state['previous'][0]['slot'],'b')
            self.assertRegex(state['previous'][0]['selection'],r'^[a-f0-9]{64}$')
            self.assertEqual((self.root/'slots.json').stat().st_mode & 0o777,0o600)
            fetch.assert_not_called()

    def test_tampered_signature_unconfirmed_slot_and_changed_native_payload_fail_closed(self):
        for reason in ('signature','unconfirmed','changed-payload','same-version','pending'):
            with self.subTest(reason=reason), self.recovery_fixture() as (current,info,journal):
                if reason=='signature':
                    path=self.root/f'evidence-{self.current}.json'
                    value=json.loads(path.read_text()); value['files']['SHA256SUMS.sig']='A'*88
                    updater.atomic_json(path,value)
                elif reason=='unconfirmed': journal['slots']['b']['confirmed']=False
                elif reason=='changed-payload': info['slots']['system-b']['updatedAt']='2026-10-08T00:00:00Z'
                elif reason=='same-version': journal['slots']['b']['version']=current; journal['slots']['b']['name']='TitanOS '+current
                else: journal['pending']={'type':'update','slot':'b','version':self.current}
                updater.atomic_json(self.root/'slots.json',journal)
                self.assertEqual(updater.recovery_status(current)['previous'],[])

    def test_native_rollback_uses_spare_try_boot_and_stale_selection_never_reboots(self):
        with self.recovery_fixture() as (current,info,journal):
            selection=updater.recovery_status(current)['previous'][0]['selection']
            original_run=subprocess.run
            native=[]
            def run(args,**kwargs):
                if args[0]=='/usr/bin/rugix-ctrl': native.append(args);return Mock(returncode=0)
                return original_run(args,**kwargs)
            with patch.object(updater.subprocess,'run',side_effect=run), contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaises(updater.UpdateError): updater.rollback(current,'0'*64)
                self.assertEqual(native,[])
                updater.rollback(current,selection)
                self.assertEqual(native,[['/usr/bin/rugix-ctrl','system','reboot','--spare']])
                with self.assertRaises(updater.UpdateError): updater.rollback(current,selection)
            pending=json.loads((self.root/'slots.json').read_text())['pending']
            self.assertEqual(pending,{'type':'rollback','slot':'b','version':self.current})

    def test_native_reboot_failure_clears_pending_and_keeps_original_default(self):
        with self.recovery_fixture() as (current,info,journal):
            selection=updater.recovery_status(current)['previous'][0]['selection']
            original_run=subprocess.run
            def run(args,**kwargs):
                if args[0]=='/usr/bin/rugix-ctrl': raise subprocess.CalledProcessError(1,args)
                return original_run(args,**kwargs)
            with patch.object(updater.subprocess,'run',side_effect=run), contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaises(subprocess.CalledProcessError): updater.rollback(current,selection)
            self.assertNotIn('pending',json.loads((self.root/'slots.json').read_text()))
            self.assertEqual(info['boot']['defaultGroup'],'a')

    def test_healthy_trial_is_committed_only_after_signed_fingerprint_validation(self):
        with self.recovery_fixture(pending=True) as (current,info,journal):
            info['boot']['defaultGroup']='b';journal['slots']['a']['confirmed']=False
            updater.atomic_json(self.root/'slots.json',journal)
            original_run=subprocess.run;native=[]
            def run(args,**kwargs):
                if args[0]=='/usr/bin/rugix-ctrl':
                    native.append(args);info['boot']['defaultGroup']='a';return Mock(returncode=0)
                return original_run(args,**kwargs)
            with patch.object(updater,'health_check',return_value=info),patch.object(updater,'sync_menu') as menu, \
                 patch.object(updater.subprocess,'run',side_effect=run):
                updater.confirm(current,3001)
            self.assertEqual(native,[['/usr/bin/rugix-ctrl','system','commit']])
            saved=json.loads((self.root/'slots.json').read_text())
            self.assertTrue(saved['slots']['a']['confirmed']);self.assertNotIn('pending',saved)
            menu.assert_called_once()

    def test_failed_health_or_changed_trial_payload_never_commits(self):
        for failure in ('health','payload'):
            with self.subTest(failure=failure),self.recovery_fixture(pending=True) as (current,info,journal):
                info['boot']['defaultGroup']='b';journal['slots']['a']['confirmed']=False
                updater.atomic_json(self.root/'slots.json',journal)
                if failure=='payload': info['slots']['system-a']['hashes']={'sha256':'f'*64}
                with patch.object(updater,'health_check',side_effect=updater.UpdateError('Not healthy') if failure=='health' else None,return_value=info), \
                     patch.object(updater.subprocess,'run') as native:
                    with self.assertRaises(updater.UpdateError): updater.confirm(current,3001)
                    self.assertFalse(any(call.args[0][:2]==['/usr/bin/rugix-ctrl','system'] for call in native.call_args_list))

    def test_native_info_requires_real_active_persistent_state_and_exact_two_groups(self):
        with self.recovery_fixture() as (current,info,journal):
            for mutation in ({'state':{'status':'Error'}},{'state':{'status':'Disabled'}},
                             {'boot':{**info['boot'],'groups':{'a':{}}}},
                             {'boot':{**info['boot'],'bootFlow':'mender-grub'}}):
                modified={**info,**mutation}
                # Call the real parser rather than the recovery fixture's stub.
                with patch.object(updater.subprocess,'check_output',return_value=json.dumps(modified).encode()):
                    with self.assertRaises(updater.UpdateError): self.original_system_info()

    def test_real_signed_legacy_trial_imports_only_the_old_native_default(self):
        with self.recovery_fixture() as (current,info,journal):
            info['boot']['defaultGroup']='b'
            updater.atomic_json(self.root/'slots.json',updater.empty_journal())
            descriptors={version:self.descriptor_for(version) for version in (current,self.current)}
            responses={}
            for version,descriptor in descriptors.items():
                responses.update({updater.asset_url(version,name):__import__('base64').b64decode(value)
                                  for name,value in descriptor['evidence']['files'].items()})
                github={**descriptor['evidence']['github'],'tag_name':'v'+version,'draft':False,'prerelease':False}
                responses[f'https://api.github.com/repos/{updater.REPOSITORY}/releases/tags/v{version}']=json.dumps(github).encode()
            old_identity={**self.release,'version':self.current,'osVersion':self.current,'versionName':'TitanOS '+self.current}
            original_run=subprocess.run;native=[]
            def run(args,**kwargs):
                if args[0]=='/usr/bin/rugix-ctrl':
                    native.append(args);info['boot']['defaultGroup']='a';return Mock(returncode=0)
                return original_run(args,**kwargs)
            with patch.object(updater,'health_check',return_value=info),patch.object(updater,'read_spare_identity',return_value=old_identity), \
                 patch.object(updater,'fetch_bytes',side_effect=lambda url,maximum:responses[url]), \
                 patch.object(updater,'sync_menu'),patch.object(updater.subprocess,'run',side_effect=run):
                updater.confirm(current,3001)
            self.assertEqual(native,[['/usr/bin/rugix-ctrl','system','commit']])
            saved=json.loads((self.root/'slots.json').read_text())
            self.assertEqual(saved['slots']['b']['version'],self.current)
            self.assertTrue(saved['slots']['b']['confirmed'])
            self.assertEqual(updater.recovery_status(current)['previous'][0]['version'],self.current)

    def test_unavailable_legacy_signature_never_promotes_trial_and_can_retry(self):
        with self.recovery_fixture() as (current,info,journal):
            info['boot']['defaultGroup']='b'
            updater.atomic_json(self.root/'slots.json',updater.empty_journal())
            old_identity={**self.release,'version':self.current,'osVersion':self.current,'versionName':'TitanOS '+self.current}
            with patch.object(updater,'health_check',return_value=info),patch.object(updater,'read_spare_identity',return_value=old_identity), \
                 patch.object(updater,'fetch_bytes',side_effect=updater.UpdateError('Offline')),patch.object(updater.subprocess,'run') as native:
                with self.assertRaisesRegex(updater.UpdateError,'Offline'): updater.confirm(current,3001)
            native.assert_not_called()
            self.assertEqual(json.loads((self.root/'slots.json').read_text())['slots'],{})
            self.assertEqual(info['boot']['defaultGroup'],'b')

    def test_spare_inspection_is_read_only_and_unmounts_even_on_invalid_metadata(self):
        target=self.root/'readonly-mount';target.mkdir()
        with patch.object(updater.tempfile,'mkdtemp',return_value=str(target)), \
             patch.object(updater.subprocess,'check_output',return_value=b'{"device":"/dev/vda5"}'), \
             patch.object(updater.os,'stat',return_value=Mock(st_mode=stat.S_IFBLK)), \
             patch.object(updater,'protected_read',side_effect=updater.UpdateError('Bad identity')), \
             patch.object(updater.subprocess,'run',return_value=Mock(returncode=0)) as native:
            with self.assertRaisesRegex(updater.UpdateError,'Bad identity'): updater.read_spare_identity('b')
        self.assertEqual(native.call_args_list[0].args[0],['/usr/bin/mount','-t','ext4','-o','ro,noload,nodev,nosuid,noexec','/dev/vda5',str(target)])
        self.assertEqual(native.call_args_list[1].args[0],['/usr/bin/umount',str(target)])
        self.assertFalse(target.exists())

    def test_untrusted_local_journal_and_signing_key_files_are_rejected(self):
        local=self.root/'unsafe';local.write_bytes(b'{}')
        # Unit tests run as an unprivileged account: root ownership itself is
        # required, independent of JSON content or a plausible previousVersion.
        if local.stat().st_uid != 0:
            with self.assertRaises(updater.UpdateError): updater.protected_read(local,65536)

    original_system_info=staticmethod(updater.system_info)


class ConfigureUpdaterTests(unittest.TestCase):
    def test_configuration_is_idempotent_for_the_clean_stable_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)
            def write(name,text):
                path=root/name; path.parent.mkdir(parents=True,exist_ok=True); path.write_text(text)
            write('.titan/release.json',json.dumps({'version':'2.0.1','osVersion':'2.0.1','stage':'stable','architecture':'amd64','systemCompatibility':updater.COMPATIBILITY,'versionName':'TitanOS 2.0.1'}))
            write('.titan/release-public.pem','public key fixture')
            write('packages/titand/package.json','{"version":"2.0.1","versionName":"TitanOS 2.0.1"}')
            write('packages/titand/source/modules/system/routes.ts',"channel: z.literal('stable')\nreturn 'stable' as const")
            write('packages/titand/source/index.ts',"releaseChannel: 'stable'\nawait this.store.set('settings.releaseChannel', 'stable')")
            write('packages/os/overlay/etc/hostname','titan')
            configure.configure(root); configure.configure(root)
            package=json.loads((root/'packages/titand/package.json').read_text())
            self.assertEqual(package['version'],'2.0.1');self.assertEqual(package['versionName'],'TitanOS 2.0.1')
            self.assertIn("await this.store.set('settings.releaseChannel', 'stable')",(root/'packages/titand/source/index.ts').read_text())
            self.assertEqual((root/'packages/os/overlay/etc/hostname').read_text(),'titan')
            source=(root/'packages/titand/source/modules/system/update.ts').read_text()
            for unwanted in ('api.titan.com','updateScript','bash -c','fetch('): self.assertNotIn(unwanted,source)
            self.assertIn('5 * 60 * 1000',source)
            self.assertTrue((root/'packages/os/overlay/usr/libexec/titan-system-update.py').exists())


    def test_build_copies_match_the_daemon_sources(self):
        # configure() overwrites these daemon files at build time. A stale copy
        # silently drops fixes made in the daemon (e.g. the boot confirmation gate).
        repository=ROOT.parent
        for build,source in (('update.ts','packages/titand/source/modules/system/update.ts'),
                             ('update.unit.test.ts','packages/titand/source/modules/system/update.unit.test.ts'),
                             ('titan-system-update.py','packages/os/overlay/usr/libexec/titan-system-update.py')):
            with self.subTest(build=build):
                self.assertEqual((ROOT/'updater'/build).read_bytes(),(repository/source).read_bytes())


if __name__=='__main__': unittest.main()
