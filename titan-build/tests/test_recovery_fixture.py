"""Check that the private recovery baseline really changes the disk identities."""
import errno
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

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


class FixtureArtifactCopyTests(unittest.TestCase):
    def setUp(self):
        self.temporary=tempfile.TemporaryDirectory(prefix='titan-fixture-artifact-')
        self.addCleanup(self.temporary.cleanup)
        self.directory=Path(self.temporary.name)
        self.source=self.directory/'source.update'; self.source.write_bytes(b'Verified native update bundle')
        self.target=self.directory/'copied.update'

    def test_owned_readable_artifact_keeps_the_hardlink_optimization(self):
        FIXTURE.link_or_copy(self.source,self.target)
        self.assertEqual(self.source.stat().st_ino,self.target.stat().st_ino)
        self.assertEqual(self.target.read_bytes(),self.source.read_bytes())

    @unittest.skipIf(os.geteuid()==0,'Run as the unprivileged image-builder user to exercise protected_hardlinks')
    def test_readable_root_owned_artifact_is_copied_without_changing_the_source(self):
        # Use public OS data with the same ownership and access as Bakery's
        # root:root 0644 .update. Do not need sudo or mutate a root-owned file.
        candidates=(Path('/usr/share/zoneinfo/Etc/UTC'),Path('/etc/debian_version'),Path('/usr/lib/os-release'))
        writable=(Path('/var/tmp'),Path.home(),Path.cwd(),Path(tempfile.gettempdir()))
        locations=next(((source,parent) for source in candidates if source.is_file() and not source.is_symlink()
                        and source.stat().st_uid==0 and not os.access(source,os.W_OK)
                        for parent in writable if parent.is_dir() and os.access(parent,os.W_OK)
                        and parent.stat().st_dev==source.stat().st_dev),None)
        if locations is None: self.skipTest('No public root-owned read-only file on a writable test filesystem')
        source,parent=locations
        with tempfile.TemporaryDirectory(prefix='titan-root-owned-artifact-',dir=parent) as temporary:
            target=Path(temporary)/'native.update'
            before=source.stat()
            try:
                os.link(source,target)
            except OSError as error:
                self.assertIn(error.errno,(errno.EPERM,errno.EACCES))
            else:
                target.unlink()
                self.skipTest('This host does not enforce protected_hardlinks for the fixture')
            FIXTURE.link_or_copy(source,target)
            self.assertEqual(target.read_bytes(),source.read_bytes())
            self.assertNotEqual(target.stat().st_ino,before.st_ino)
            after=source.stat()
            self.assertEqual((after.st_ino,after.st_uid,after.st_mode,after.st_size,after.st_mtime_ns),
                             (before.st_ino,before.st_uid,before.st_mode,before.st_size,before.st_mtime_ns))

    def test_permission_filesystem_and_link_limit_failures_fall_back_to_copy(self):
        for code in {errno.EXDEV,errno.EPERM,errno.EACCES,errno.EMLINK,errno.ENOSYS,errno.ENOTSUP,errno.EOPNOTSUPP}:
            with self.subTest(errno=code),patch.object(FIXTURE.os,'link',side_effect=OSError(code,'Link is unavailable')):
                FIXTURE.link_or_copy(self.source,self.target)
                self.assertEqual(self.target.read_bytes(),self.source.read_bytes())
                self.assertNotEqual(self.target.stat().st_ino,self.source.stat().st_ino)
                self.target.unlink()

    def test_real_io_errors_propagate_without_attempting_a_copy(self):
        for code in (errno.EIO,errno.ENOSPC,errno.ENOENT,errno.EEXIST,errno.EROFS):
            error=OSError(code,'Actual artifact I/O failure')
            with (self.subTest(errno=code),patch.object(FIXTURE.os,'link',side_effect=error),
                  patch.object(FIXTURE.shutil,'copyfile') as copy):
                with self.assertRaises(OSError) as result: FIXTURE.link_or_copy(self.source,self.target)
                self.assertIs(result.exception,error); copy.assert_not_called()
                self.assertFalse(self.target.exists())

    def test_copy_failure_is_not_swallowed(self):
        error=OSError(errno.ENOSPC,'The destination ran out of disk space')
        with (patch.object(FIXTURE.os,'link',side_effect=PermissionError(errno.EPERM,'protected_hardlinks')),
              patch.object(FIXTURE.shutil,'copyfile',side_effect=error)):
            with self.assertRaises(OSError) as result: FIXTURE.link_or_copy(self.source,self.target)
            self.assertIs(result.exception,error)


if __name__=='__main__': unittest.main()
