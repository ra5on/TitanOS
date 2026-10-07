"""Check that the private recovery baseline really changes the disk identities."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC=importlib.util.spec_from_file_location('titan_recovery_fixture',ROOT/'prepare-recovery-fixture.py')
FIXTURE=importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(FIXTURE)
MKFS=shutil.which('mkfs.ext4') or ('/usr/sbin/mkfs.ext4' if Path('/usr/sbin/mkfs.ext4').is_file() else None)
DEBUGFS=shutil.which('debugfs') or ('/usr/sbin/debugfs' if Path('/usr/sbin/debugfs').is_file() else None)


@unittest.skipUnless(MKFS and DEBUGFS,'e2fsprogs required for actual baseline filesystems')
class BaselineFilesystemTests(unittest.TestCase):
    def test_private_baseline_has_its_own_version_in_system_daemon_and_boot_files(self):
        with tempfile.TemporaryDirectory(prefix='titan-recovery-baseline-') as temporary:
            directory=Path(temporary)
            system=directory/'system'; boot=directory/'boot'; system.mkdir(); boot.mkdir()
            for name in ('usr/share/titan','opt/titand','etc'): (system/name).mkdir(parents=True)
            release={'version':'2.0.4','osVersion':'2.0.4','versionName':'TitanOS 2.0.4','systemCompatibility':'titan-rugix-amd64-v2'}
            (system/'usr/share/titan/release.json').write_text(json.dumps(release))
            (system/'opt/titand/package.json').write_text(json.dumps({'version':'2.0.4','versionName':'TitanOS 2.0.4','name':'titand'}))
            (system/'etc/os-release').write_text('VERSION="2.0.4"\nVERSION_ID="2.0.4"\n')
            (boot/'titan-version.grubenv').write_bytes(b'old environment')
            images=[]
            for source in (boot,system):
                image=source.with_suffix('.img')
                with image.open('wb') as stream: stream.truncate(16*1024**2)
                subprocess.run([MKFS,'-q','-d',str(source),str(image)],check=True,capture_output=True)
                images.append(image)
            previous=os.environ.get('PATH','')
            os.environ['PATH']=str(Path(DEBUGFS).parent)+os.pathsep+previous
            try:
                baseline=FIXTURE.patch_baseline(*images,release,directory)
                self.assertEqual(baseline['version'],'0.0.0')
                installed=json.loads(FIXTURE.filesystem_read(images[1],'/usr/share/titan/release.json',directory))
                daemon=json.loads(FIXTURE.filesystem_read(images[1],'/opt/titand/package.json',directory))
                self.assertEqual(installed['version'],'0.0.0'); self.assertEqual(daemon['versionName'],'TitanOS 0.0.0')
                environment=FIXTURE.filesystem_read(images[0],'/titan-version.grubenv',directory)
                self.assertEqual(len(environment),1024); self.assertIn(b'titan_slot_version=0.0.0\n',environment)
                self.assertEqual(release['version'],'2.0.4')
            finally: os.environ['PATH']=previous


if __name__=='__main__': unittest.main()
