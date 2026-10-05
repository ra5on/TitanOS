"""Real Git archive/import and scoped publication regressions; no network access."""
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tarfile
import tempfile
import unittest
from unittest import mock

HELPER = Path(__file__).resolve().parents[1] / 'import-upstream.py'
SPEC = importlib.util.spec_from_file_location('titan_source_bootstrap', HELPER)
BOOTSTRAP = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(BOOTSTRAP)


def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args], text=True, stderr=subprocess.DEVNULL).strip()


def write(root, name, value, mode=0o644):
    path=root/name; path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(value if isinstance(value,bytes) else value.encode()); path.chmod(mode)


def commit(root):
    git(root, 'add', '--all')
    git(root, '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-m', 'Fixture source')


@unittest.skipUnless(shutil.which('git'), 'git required')
class CompleteSourceBootstrapTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.directory=Path(self.temp.name)
        self.source=self.directory/'old'; self.root=self.directory/'new'
        for path in (self.source,self.root):
            subprocess.run(['git','init','--quiet','--initial-branch=main',str(path)],check=True)
        for name in BOOTSTRAP.REQUIRED_SOURCE_FILES:
            write(self.source,name,'Complete Titan source '+name)
        write(self.source,'LICENSE.md','Complete immutable upstream license')
        write(self.source,'.titan/release-public.pem','Same embedded public key')
        write(self.source,'.titan/release.json',json.dumps({'version':'2.0.0'}))
        write(self.source,'.github/workflows/titan-image.yml','Old repository workflow')
        write(self.source,'titan-build/import-upstream.py','Old importer')
        write(self.source,'packages/ui/src/constants/links.ts','Old repository links')
        write(self.source,'packages/ui/src/features/machines/titan-only.tsx','Modern Titan VM feature')
        write(self.source,'packages/os/overlay/etc/systemd/system/example.service','Service source')
        link=self.source/'packages/os/overlay/etc/systemd/system/multi-user.target.wants/example.service'
        link.parent.mkdir(parents=True); link.symlink_to('../example.service')
        write(self.source,'.gitignore','.secrets/\n*.env\nnode_modules/\n')
        commit(self.source)
        self.commit=git(self.source,'rev-parse','HEAD'); self.tree=git(self.source,'rev-parse','HEAD^{tree}')
        self.minimum=len(BOOTSTRAP.tracked_files(self.source))
        # These are real migration controls and an intentionally partial package
        # seed; package.json alone must never mean complete source is present.
        self.seed={
            'LICENSE.md':('Complete immutable upstream license',0o644),
            '.titan/release-public.pem':('Same embedded public key',0o644),
            '.titan/release.json':(json.dumps({'version':'2.0.1','updateProvider':'ra5on/TitanOS'}),0o644),
            '.github/workflows/titan-image.yml':('New repository workflow',0o644),
            'titan-build/import-upstream.py':('New safe importer',0o755),
            'packages/ui/src/constants/links.ts':('New repository links',0o644),
            'packages/umbreld/package.json':('Partial package seed',0o644),
            'README.md':('New TitanOS documentation',0o644),
            '[seed-control].txt':('Literal brackets in a tracked filename',0o644),
            '.gitignore':('.secrets/\n*.env\nnode_modules/\n',0o644),
        }
        for name,(value,mode) in self.seed.items(): write(self.root,name,value,mode)
        commit(self.root)
        self.seed_commit=git(self.root,'rev-parse','HEAD')
        # Never import old untracked private data or stage new working data.
        write(self.source,'.secrets/private.pem','OLD PRIVATE DATA')
        write(self.source,'credentials.env','OLD ENVIRONMENT')
        write(self.root,'.secrets/private.pem','NEW PRIVATE DATA')
        write(self.root,'credentials.env','NEW ENVIRONMENT')
        write(self.root,'working-notes.txt','Untracked working document')

    def bootstrap(self, **overrides):
        return BOOTSTRAP.bootstrap(self.root, **{
            'repository':self.source.as_uri(), 'commit':self.commit,
            'tree':self.tree, 'minimum':self.minimum, **overrides,
        })

    def test_real_pinned_git_archive_preserves_all_seed_bytes_modes_and_titan_features(self):
        imported=self.bootstrap()
        for name,(value,mode) in self.seed.items():
            self.assertEqual((self.root/name).read_text(),value,name)
            self.assertEqual(stat.S_IMODE((self.root/name).stat().st_mode),mode,name)
        self.assertEqual((self.root/'packages/ui/src/features/machines/titan-only.tsx').read_text(),'Modern Titan VM feature')
        self.assertEqual(os.readlink(self.root/'packages/os/overlay/etc/systemd/system/multi-user.target.wants/example.service'),'../example.service')
        self.assertNotIn('.secrets/private.pem',imported); self.assertNotIn('credentials.env',imported)
        self.assertEqual((self.root/'.secrets/private.pem').read_text(),'NEW PRIVATE DATA')
        provenance=json.loads((self.root/'.titan/source-bootstrap.json').read_text())
        self.assertEqual((provenance['sourceCommit'],provenance['sourceTree'],provenance['seedCommit']),(self.commit,self.tree,self.seed_commit))
        self.assertEqual(provenance['preservedSeedFiles'],sorted(self.seed))

    def test_real_scoped_commit_push_excludes_ignored_and_untracked_working_data_then_is_idempotent(self):
        imported=self.bootstrap()
        remote=self.directory/'remote.git'
        subprocess.run(['git','init','--quiet','--bare',str(remote)],check=True)
        git(self.root,'remote','add','origin',str(remote))
        with mock.patch.dict(os.environ,{'GITHUB_REPOSITORY':'ra5on/TitanOS'}):
            BOOTSTRAP.persist_source_changes(self.root,imported)
        tracked=BOOTSTRAP.tracked_files(self.root)
        self.assertNotIn('.secrets/private.pem',tracked); self.assertNotIn('credentials.env',tracked)
        self.assertNotIn('working-notes.txt',tracked)
        self.assertIn('[seed-control].txt',tracked)
        published=git(remote,'rev-parse','refs/heads/main')
        self.assertEqual(published,git(self.root,'rev-parse','HEAD'))
        with mock.patch.object(BOOTSTRAP,'fetch_source',side_effect=AssertionError('Must not fetch again')):
            self.assertEqual(self.bootstrap(),[])
        with mock.patch.dict(os.environ,{'GITHUB_REPOSITORY':'ra5on/TitanOS'}):
            BOOTSTRAP.persist_source_changes(self.root,[])
        self.assertEqual(git(self.root,'rev-parse','HEAD'),published)

    def test_wrong_commit_tree_license_or_public_key_fails_before_seed_is_changed(self):
        with self.assertRaisesRegex(ValueError,'commit or tree'):
            self.bootstrap(tree='0'*40)
        for name,message in [('LICENSE.md','license'),('.titan/release-public.pem','signing public key')]:
            original=(self.root/name).read_bytes(); (self.root/name).write_text('Unexpected replacement')
            with self.subTest(name=name), self.assertRaisesRegex(ValueError,message): self.bootstrap()
            (self.root/name).write_bytes(original)
            self.assertFalse((self.root/'packages/ui/src/features/machines/titan-only.tsx').exists())
        self.assertEqual(git(self.root,'rev-parse','HEAD'),self.seed_commit)

    def test_source_publication_refuses_other_repository_before_staging(self):
        imported=self.bootstrap()
        with mock.patch.dict(os.environ,{'GITHUB_REPOSITORY':'ra5on/Titan'}):
            with self.assertRaisesRegex(ValueError,'only in ra5on/TitanOS'):
                BOOTSTRAP.persist_source_changes(self.root,imported)
        self.assertEqual(git(self.root,'diff','--cached','--name-only'),'')

    def test_pinned_complete_source_requires_all_critical_components(self):
        write(self.source,'LICENSE.md','Complete immutable upstream license')
        (self.source/'packages/os/umbrelos.Dockerfile').unlink(); commit(self.source)
        with self.assertRaisesRegex(ValueError,'incomplete'):
            self.bootstrap(commit=git(self.source,'rev-parse','HEAD'),tree=git(self.source,'rev-parse','HEAD^{tree}'))
        self.assertFalse((self.root/'packages/ui/src/features/machines/titan-only.tsx').exists())


class SafeSourceArchiveTests(unittest.TestCase):
    def test_unsafe_paths_links_special_files_and_duplicate_entries_fail_before_extraction(self):
        attacks=[('../escape',tarfile.REGTYPE,''),('/absolute',tarfile.REGTYPE,''),
                 ('.git/config',tarfile.REGTYPE,''),('.secrets/key',tarfile.REGTYPE,''),
                 ('dist/image.img',tarfile.REGTYPE,''),('packages/node_modules/data',tarfile.REGTYPE,''),
                 ('hardlink',tarfile.LNKTYPE,'safe'),('device',tarfile.CHRTYPE,''),
                 ('link',tarfile.SYMTYPE,'/tmp/escape'),('link',tarfile.SYMTYPE,'../escape'),
                 ('link',tarfile.SYMTYPE,'.git/config'),('safe',tarfile.REGTYPE,'')]
        for name,kind,target in attacks:
            with self.subTest(name=name,kind=kind), tempfile.TemporaryDirectory() as directory:
                root=Path(directory); archive=root/'source.tar'; destination=root/'out'; destination.mkdir()
                with tarfile.open(archive,'w') as output:
                    entry=tarfile.TarInfo('safe'); entry.size=2; output.addfile(entry,io.BytesIO(b'OK'))
                    entry=tarfile.TarInfo(name); entry.type=kind; entry.linkname=target
                    output.addfile(entry,io.BytesIO(b''))
                with self.assertRaises(ValueError): BOOTSTRAP.extract_source(archive,destination)
                self.assertEqual(list(destination.iterdir()),[])


@unittest.skipUnless(shutil.which('openssl'), 'openssl required')
class EarlySigningKeyTests(unittest.TestCase):
    def test_key_match_mismatch_and_invalid_input_do_not_leak_secrets_or_leave_private_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); (root/'titan-build').mkdir(); (root/'.titan').mkdir(); (root/'runner').mkdir()
            script=root/'titan-build/check-signing-key.sh'
            shutil.copyfile(HELPER.parent/'check-signing-key.sh',script)
            key=root/'key.pem'; other=root/'other.pem'
            for path in (key,other):
                subprocess.run(['openssl','genpkey','-algorithm','ED25519','-out',str(path)],check=True,capture_output=True)
            subprocess.run(['openssl','pkey','-in',str(key),'-pubout','-out',str(root/'.titan/release-public.pem')],check=True,capture_output=True)
            for value,expected,message in [(key.read_text(),0,'matches'),(other.read_text(),1,'does not match'),
                                           ('SECRET-INVALID-PRIVATE-KEY',1,'not a valid'),('',1,'required')]:
                with self.subTest(message=message):
                    result=subprocess.run(['bash',str(script)],env={**os.environ,'RUNNER_TEMP':str(root/'runner'),'TITAN_SIGNING_KEY':value},capture_output=True,text=True)
                    self.assertEqual(result.returncode,expected,result.stderr)
                    self.assertIn(message,result.stdout+result.stderr)
                    if value:self.assertNotIn(value,result.stdout+result.stderr)
                    self.assertEqual(list((root/'runner').iterdir()),[])


if __name__=='__main__': unittest.main()
