"""Exercise signing and immutable publishing against a local GitHub CLI stub."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


@unittest.skipUnless(shutil.which('openssl') and shutil.which('git'), 'openssl and git required')
class StablePublicationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(); self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        for name in ('titan-build','.titan','runner','bin'):
            (self.root/name).mkdir()
        for name in ('sign-artifacts.sh','publish-release.sh','release_identity.py'):
            shutil.copyfile(ROOT/name, self.root/'titan-build'/name)
        (self.root/'titan-build/RELEASE.md').write_text('Fixture release notes')
        self.key = self.root/'private.pem'
        subprocess.run(['openssl','genpkey','-algorithm','ED25519','-out',str(self.key)],check=True,capture_output=True)
        subprocess.run(['openssl','pkey','-in',str(self.key),'-pubout','-out',str(self.root/'.titan/release-public.pem')],check=True,capture_output=True)
        subprocess.run(['git','init',str(self.root)],check=True,capture_output=True)
        subprocess.run(['git','-C',str(self.root),'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','--allow-empty','-m','Fixture'],check=True,capture_output=True)
        stub = self.root/'bin/gh'
        stub.write_text('#!/usr/bin/env python3\nimport json,os,sys\nwith open(os.environ["GH_CALLS"], "a") as stream: stream.write(json.dumps(sys.argv[1:])+"\\n")\nif sys.argv[1:3] == ["release","view"]:\n if os.environ.get("GH_PUBLIC") == "1": print("false"); sys.exit(0)\n sys.exit(1)\n')
        stub.chmod(0o755)
        self.calls = self.root/'calls.jsonl'
        self.env = {**os.environ,'PATH':str(self.root/'bin')+os.pathsep+os.environ['PATH'],
                    'GH_CALLS':str(self.calls),'GITHUB_ACTIONS':'true','RUNNER_TEMP':str(self.root/'runner'),
                    'TITAN_SIGNING_KEY':self.key.read_text()}

    def prepare(self, changes=None, manifest_changes=None, bad_name=False):
        directory = self.root/'artifacts'; directory.mkdir()
        version = '2.0.1'
        release = {'version':version,'osVersion':version,'versionName':'TitanOS '+version,'stage':'stable',
                   'architecture':'amd64','systemCompatibility':'titan-rugix-amd64-v2'}
        release.update(changes or {})
        (directory/'release.json').write_text(json.dumps(release))
        assets=[]
        for name,payload in [(f'titan-{version}.update',b'Fixture update'),
                             (f'titan-{version}-amd64.img.xz' if bad_name else f'titan-{version}.img.xz',b'Fixture compressed image')]:
            (directory/name).write_bytes(payload)
            assets.append({'name':name,'sizeBytes':len(payload),'sha256':hashlib.sha256(payload).hexdigest()})
        head=subprocess.check_output(['git','-C',str(self.root),'rev-parse','HEAD'],text=True).strip()
        manifest = {'schemaVersion':1,'releaseEligible':True,'stage':'stable','releaseVersion':version,'osVersion':version,
                    'architecture':'amd64','firmware':'UEFI','updateFormat':'rugix','systemCompatibility':'titan-rugix-amd64-v2',
                    'ownTitanUpdateChannel':True,'buildCommit':head,'assets':assets,
                    'imageVerification':{'structuralCheck':'passed','uefiHttpSmoke':{'status':'passed',
                                         'installedRelease':{'version':version,'name':'TitanOS '+version},'bootDiskSizeBytes':32*1024**3},
                                         'bridgeNetworkSmoke':{'status':'passed','passedTests':12,'report':'bridge-smoke.json'}}}
        manifest.update(manifest_changes or {})
        (directory/'build-manifest.json').write_text(json.dumps(manifest))
        report = {'success':True,'numTotalTests':12,'numPassedTests':12,'numFailedTests':0,
                  'numPendingTests':0,'numTodoTests':0,'numTotalTestSuites':4,'numPassedTestSuites':4,
                  'numFailedTestSuites':0,'numPendingTestSuites':0,
                  'testResults':[{'status':'passed','name':str(self.root/'packages/titand/source/modules/machines/automatic-bridge.vm.test.ts'),
                                  'assertionResults':[{'status':'passed','fullName':f'Real bridge scenario {index}'} for index in range(12)]}]}
        (directory/'bridge-smoke.json').write_text(json.dumps(report))
        (directory/'LICENSE.md').write_text('Fixture attribution')
        (directory/'UPSTREAM.md').write_text('Fixture source provenance')
        self.env['TITAN_ARTIFACT_DIR']=str(directory)
        self.sign()
        return directory

    def sign(self):
        subprocess.run(['bash',str(self.root/'titan-build/sign-artifacts.sh')],env=self.env,check=True,capture_output=True)
        self.assertEqual(list((self.root/'runner').glob('titan-signing.*')), [])

    def publish(self):
        return subprocess.run(['bash',str(self.root/'titan-build/publish-release.sh')],env=self.env,capture_output=True,text=True)

    def gh_calls(self):
        return [json.loads(line) for line in self.calls.read_text().splitlines()] if self.calls.exists() else []

    def test_clean_custom_artifact_directory_signs_and_publishes_stable_latest(self):
        directory=self.prepare(); result=self.publish(); self.assertEqual(result.returncode,0,result.stderr)
        calls=self.gh_calls(); create=next(c for c in calls if c[:2]==['release','create'])
        self.assertIn('--prerelease=false',create); self.assertEqual(create[2],'v2.0.1')
        upload=next(c for c in calls if c[:2]==['release','upload'])
        self.assertIn(str(directory/'titan-2.0.1.img.xz'),upload)
        self.assertIn('--latest=true',calls[-1])
        self.assertEqual(create[create.index('--title')+1],'TitanOS 2.0.1')
        for call in calls: self.assertEqual(call[call.index('--repo')+1],'ra5on/TitanOS')

    def test_signed_experimental_legacy_or_foreign_layout_is_rejected_before_github(self):
        for changes in ({'version':'2.0.0-titan.3','osVersion':'2.0.0-titan.3'}, {'stage':'alpha'},
                        {'systemCompatibility':'titan-titan-rugix-amd64-v1'}, {'architecture':'arm64'}):
            with self.subTest(changes=changes):
                directory=self.prepare(changes=changes)
                self.assertNotEqual(self.publish().returncode,0)
                self.assertEqual(self.gh_calls(),[])
                shutil.rmtree(directory)

    def test_signed_wrong_filename_wrong_boot_or_wrong_commit_is_rejected_before_github(self):
        cases=[{'bad_name':True}, {'manifest_changes':{'buildCommit':'0'*40}},
               {'manifest_changes':{'imageVerification':{'structuralCheck':'passed','uefiHttpSmoke':{'status':'passed',
                  'installedRelease':{'version':'2.0.0','name':'TitanOS 2.0.0'},'bootDiskSizeBytes':32*1024**3}}}}]
        for kwargs in cases:
            with self.subTest(kwargs=kwargs):
                directory=self.prepare(**kwargs)
                self.assertNotEqual(self.publish().returncode,0); self.assertEqual(self.gh_calls(),[])
                shutil.rmtree(directory)

    def test_unchecked_additional_file_is_never_uploaded(self):
        directory=self.prepare(); (directory/'unsigned.txt').write_text('Unsigned data')
        self.assertNotEqual(self.publish().returncode,0); self.assertEqual(self.gh_calls(),[])

    def test_modified_asset_fails_before_any_github_call(self):
        directory=self.prepare(); (directory/'titan-2.0.1.update').write_bytes(b'Tampered')
        self.assertNotEqual(self.publish().returncode,0); self.assertEqual(self.gh_calls(),[])

    def test_missing_skipped_failed_or_redirected_signed_bridge_evidence_blocks_publishing(self):
        for change in (None, {'status':'skipped'}, {'status':'failed'}, {'report':'other.json'},
                       {'report':'../bridge-smoke.json'}, {'passedTests':11}, {'passedTests':True}):
            with self.subTest(change=change):
                directory=self.prepare()
                path=directory/'build-manifest.json'; manifest=json.loads(path.read_text())
                if change is None: del manifest['imageVerification']['bridgeNetworkSmoke']
                else: manifest['imageVerification']['bridgeNetworkSmoke'].update(change)
                path.write_text(json.dumps(manifest)); self.sign()
                self.assertNotEqual(self.publish().returncode,0); self.assertEqual(self.gh_calls(),[])
                shutil.rmtree(directory)

    def test_missing_malformed_or_duplicate_key_bridge_report_blocks_publishing(self):
        for content in (None, 'not JSON', '{"success":false,"success":true}'):
            with self.subTest(content=content):
                directory=self.prepare(); path=directory/'bridge-smoke.json'
                if content is None: path.unlink()
                else: path.write_text(content)
                self.sign()
                self.assertNotEqual(self.publish().returncode,0); self.assertEqual(self.gh_calls(),[])
                shutil.rmtree(directory)

    def test_signed_report_with_failed_skipped_todo_or_inconsistent_counts_is_rejected(self):
        changes=[{'success':False}, {'success':'true'}, {'numFailedTests':1}, {'numPendingTests':1},
                 {'numTodoTests':1}, {'numFailedTestSuites':1}, {'numPendingTestSuites':1},
                 {'numPassedTestSuites':3}, {'numPassedTests':11}, {'numTotalTests':13},
                 {'numFailedTests':False}, {'numPassedTests':True}]
        for change in changes:
            with self.subTest(change=change):
                directory=self.prepare(); path=directory/'bridge-smoke.json'
                report=json.loads(path.read_text()); report.update(change)
                path.write_text(json.dumps(report)); self.sign()
                self.assertNotEqual(self.publish().returncode,0); self.assertEqual(self.gh_calls(),[])
                shutil.rmtree(directory)

    def test_substituted_report_or_incomplete_assertions_is_rejected_even_when_signed(self):
        for attack in ('foreign-test-file','empty-results','fewer-assertions','skipped-assertion','duplicate-assertions'):
            with self.subTest(attack=attack):
                directory=self.prepare(); path=directory/'bridge-smoke.json'; report=json.loads(path.read_text())
                suite=report['testResults'][0]
                if attack=='foreign-test-file': suite['name']=suite['name'].replace('automatic-bridge.vm.test.ts','unrelated.unit.test.ts')
                elif attack=='empty-results': report['testResults']=[]
                elif attack=='fewer-assertions': suite['assertionResults'].pop()
                elif attack=='skipped-assertion': suite['assertionResults'][0]['status']='pending'
                elif attack=='duplicate-assertions': suite['assertionResults'][1]=dict(suite['assertionResults'][0])
                path.write_text(json.dumps(report)); self.sign()
                self.assertNotEqual(self.publish().returncode,0); self.assertEqual(self.gh_calls(),[])
                shutil.rmtree(directory)

    def test_bridge_report_cannot_be_substituted_after_signing(self):
        directory=self.prepare(); path=directory/'bridge-smoke.json'
        report=json.loads(path.read_text()); report['success']=False; path.write_text(json.dumps(report))
        self.assertNotEqual(self.publish().returncode,0); self.assertEqual(self.gh_calls(),[])

    def test_public_release_is_never_uploaded_over_or_edited(self):
        self.prepare(); self.env['GH_PUBLIC']='1'
        self.assertNotEqual(self.publish().returncode,0)
        self.assertEqual([c[:2] for c in self.gh_calls()],[['release','view']])
