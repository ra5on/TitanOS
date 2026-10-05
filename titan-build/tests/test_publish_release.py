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
        for name in ('sign-artifacts.sh','publish-release.sh'):
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

    def prepare(self, bridge=False):
        directory = self.root/('artifacts-bridge' if bridge else 'artifacts-canonical'); directory.mkdir()
        version = '2.0.0-titan.3' if bridge else '2.0.0'
        release = {'version':version,'osVersion':version,'versionName':'TitanOS 2.0.0','stage':'stable'}
        if bridge: release['legacyUpdateBridgeTo']='2.0.0'
        (directory/'release.json').write_text(json.dumps(release))
        manifest = {'releaseEligible':True,'stage':'stable','releaseVersion':version,'osVersion':version,
                    'imageVerification':{'uefiHttpSmoke':{'status':'passed','installedRelease':{'version':version}}}}
        (directory/'build-manifest.json').write_text(json.dumps(manifest))
        (directory/(f'titan-{version}-amd64.update' if bridge else f'titan-{version}.update')).write_bytes(b'Fixture update')
        if not bridge: (directory/f'titan-{version}.img.xz').write_bytes(b'Fixture compressed image')
        self.env['TITAN_ARTIFACT_DIR']=str(directory)
        subprocess.run(['bash',str(self.root/'titan-build/sign-artifacts.sh')],env=self.env,check=True,capture_output=True)
        self.assertEqual(list((self.root/'runner').glob('titan-signing.*')), [])
        return directory

    def publish(self):
        return subprocess.run(['bash',str(self.root/'titan-build/publish-release.sh')],env=self.env,capture_output=True,text=True)

    def gh_calls(self):
        return [json.loads(line) for line in self.calls.read_text().splitlines()] if self.calls.exists() else []

    def test_canonical_custom_artifact_directory_signs_and_publishes_stable_latest(self):
        directory=self.prepare(); result=self.publish(); self.assertEqual(result.returncode,0,result.stderr)
        calls=self.gh_calls(); create=next(c for c in calls if c[:2]==['release','create'])
        self.assertIn('--prerelease=false',create); self.assertEqual(create[2],'v2.0.0')
        upload=next(c for c in calls if c[:2]==['release','upload'])
        self.assertIn(str(directory/'titan-2.0.0.img.xz'),upload)
        self.assertIn('--latest=true',calls[-1])

    def test_stable_bridge_is_update_only_and_never_becomes_latest(self):
        directory=self.prepare(bridge=True); result=self.publish(); self.assertEqual(result.returncode,0,result.stderr)
        calls=self.gh_calls(); upload=next(c for c in calls if c[:2]==['release','upload'])
        self.assertIn(str(directory/'titan-2.0.0-titan.3-amd64.update'),upload)
        self.assertFalse(any(value.endswith('.img.xz') for value in upload))
        self.assertIn('--prerelease=false',calls[-1]); self.assertIn('--latest=false',calls[-1])

    def test_modified_asset_fails_before_any_github_call(self):
        directory=self.prepare(); (directory/'titan-2.0.0.update').write_bytes(b'Tampered')
        self.assertNotEqual(self.publish().returncode,0); self.assertEqual(self.gh_calls(),[])

    def test_public_release_is_never_uploaded_over_or_edited(self):
        self.prepare(); self.env['GH_PUBLIC']='1'
        self.assertNotEqual(self.publish().returncode,0)
        self.assertEqual([c[:2] for c in self.gh_calls()],[['release','view']])
