"""Read-only OS handoff and backward-compatible mutable-state seeding."""
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / 'image/debian/ab/persist-initramfs.sh'
WRITABLE = ('var/lib/titan', 'var/lib/titan-agent', 'var/lib/titan-proxy',
            'var/lib/docker', 'var/lib/containerd', 'var/lib/libvirt',
            'var/lib/samba', 'var/lib/systemd', 'var/lib/private', 'var/lib/dbus', 'var/lib/wtmpdb', 'var/cache', 'var/log',
            'var/tmp', 'var/spool', 'var/srv/titan', 'home')
LEGACY = ('var/lib/titan', 'var/lib/titan-agent', 'var/lib/titan-proxy',
          'var/lib/docker', 'var/lib/containerd', 'var/lib/libvirt',
          'var/lib/samba', 'var/cache/samba', 'var/srv/titan', 'home')


class ReadonlyHandoffTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name); self.guest = self.work / 'root'
        for name in (*WRITABLE, 'tmp', 'etc', 'var/lib/titan-system', 'var/lib/dpkg', 'var/lib/apt'):
            path = self.guest / name; path.mkdir(parents=True, exist_ok=True)
            (path / 'factory').write_text('factory:' + name)
        for name in ('var/cache/samba', 'var/cache/libvirt'):
            path = self.guest / name; path.mkdir(); path.chmod(0o710)
            (path / 'factory').write_text('factory:' + name)
        (self.guest / 'var/tmp').chmod(0o1777)
        (self.guest / 'tmp').chmod(0o1777)
        self.data = self.guest / 'var/lib/titan-system'
        self.log = self.work / 'mounts.jsonl'
        self.cmdline = self.work / 'cmdline'; self.cmdline.write_text('root=LABEL=TITAN-A ro rauc.slot=A\n')
        functions = self.work / 'functions'
        functions.write_text('''panic() { printf '%s\\n' "$*" >&2; exit 88; }
sync() { :; }
cp() {
    case "$*" in *"${FAIL_COPY:-never-a-source}"*) return 17 ;; esac
    command cp "$@"
}
''')
        mount = self.work / 'mount'
        mount.write_text('''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
with open(os.environ['MOUNT_LOG'],'a') as stream:stream.write(json.dumps(sys.argv[1:])+'\\n')
assert Path(sys.argv[-1]).is_dir()
if os.environ.get('FAIL_MOUNT') in sys.argv[1:]:sys.exit(17)
if sys.argv[1:3]==['-o','bind']:assert Path(sys.argv[3]).is_dir()
'''); mount.chmod(0o755)
        fsck = self.work / 'fsck'; fsck.write_text('#!/bin/sh\nexit "${FAIL_FSCK:-0}"\n'); fsck.chmod(0o755)
        source = SCRIPT.read_text().replace('. /scripts/functions', '. ' + str(functions))
        source = source.replace('cat /proc/cmdline', 'cat ' + str(self.cmdline))
        source = source.replace('/bin/titan-mount', str(mount)).replace('/sbin/e2fsck', str(fsck))
        self.script = self.work / 'init-bottom'; self.script.write_text(source)

    def invoke(self, **environment):
        return subprocess.run(['sh', str(self.script)], text=True, capture_output=True, timeout=15,
                              env={**os.environ, 'rootmnt':str(self.guest), 'MOUNT_LOG':str(self.log), **environment})

    def mounts(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def legacy_seed(self):
        persistent = self.data / 'persistent'
        for name in (*LEGACY, 'etc'):
            target = persistent / name; target.mkdir(parents=True, exist_ok=True)
            (target / 'saved').write_text('saved:' + name)
        (persistent / 'var/cache/samba').chmod(0o700)
        (persistent / 'var/cache/samba/saved').chmod(0o600)
        return persistent

    def test_fresh_state_keeps_permissions_and_os_package_databases_slot_local(self):
        before = {name:(self.guest / name / 'factory').read_bytes() for name in ('var/lib/dpkg','var/lib/apt')}
        result = self.invoke(); self.assertEqual(result.returncode, 0, result.stderr)
        persistent = self.data / 'persistent'
        self.assertTrue((persistent / '.readonly-layout-v1').is_file())
        self.assertEqual(stat.S_IMODE((persistent / 'var/tmp').stat().st_mode), 0o1777)
        self.assertEqual(stat.S_IMODE((persistent / 'var/cache/samba').stat().st_mode), 0o710)
        for name, contents in before.items():
            self.assertEqual((self.guest / name / 'factory').read_bytes(), contents)
            self.assertFalse((persistent / name).exists())
        commands = self.mounts()
        self.assertEqual(commands[-1], ['-o','remount,ro',str(self.guest)])
        binds = {Path(command[-1]).relative_to(self.guest).as_posix() for command in commands if command[:2]==['-o','bind']}
        self.assertEqual(binds, set(WRITABLE))
        self.assertNotIn('var', binds)
        self.assertFalse(any('remount,rw' in command for command in commands))
        self.assertEqual(commands[-2], ['-t','tmpfs','-o','rw,nosuid,nodev,mode=1777,size=25%','tmpfs',str(self.guest / 'tmp')])

    def test_old_seed_preserves_existing_samba_identity_cache_and_mode_on_repeat(self):
        persistent = self.legacy_seed()
        for _ in range(2):
            result = self.invoke(); self.assertEqual(result.returncode, 0, result.stderr)
            for name in LEGACY:
                self.assertEqual((persistent / name / 'saved').read_text(), 'saved:' + name)
            self.assertEqual(stat.S_IMODE((persistent / 'var/cache/samba').stat().st_mode), 0o700)
            self.assertEqual(stat.S_IMODE((persistent / 'var/cache/samba/saved').stat().st_mode), 0o600)
            self.assertFalse((persistent / 'var/cache/samba/factory').exists())
            self.assertEqual((persistent / 'var/cache/libvirt/factory').read_text(), 'factory:var/cache/libvirt')
            for name in ('var/lib/systemd','var/log','var/tmp','var/spool'):
                self.assertEqual((persistent / name / 'factory').read_text(), 'factory:' + name)
            self.assertTrue((persistent / '.readonly-layout-v1').is_file())
            self.assertFalse((self.data / '.readonly-seed').exists())

    def test_interrupted_seed_is_not_published_and_resumes_without_resetting_saved_data(self):
        persistent = self.legacy_seed()
        failed = self.invoke(FAIL_COPY='var/log')
        self.assertEqual(failed.returncode, 88)
        self.assertFalse((persistent / '.readonly-layout-v1').exists())
        self.assertFalse(any(command[:2]==['-o','bind'] for command in self.mounts()))
        self.assertEqual((persistent / 'var/lib/samba/saved').read_text(), 'saved:var/lib/samba')
        result = self.invoke(); self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((persistent / '.readonly-layout-v1').is_file())
        self.assertEqual((persistent / 'var/lib/samba/saved').read_text(), 'saved:var/lib/samba')

    def test_missing_factory_mount_target_aborts_before_data_mount_or_new_state(self):
        (self.guest / 'var/spool/factory').unlink(); (self.guest / 'var/spool').rmdir()
        result = self.invoke(); self.assertEqual(result.returncode, 88)
        self.assertIn('Schreibpfad fehlt', result.stderr)
        self.assertEqual(self.mounts(), [])
        self.assertFalse((self.data / 'persistent').exists())

    def test_missing_or_symlinked_state_never_becomes_an_empty_healthy_nas(self):
        persistent = self.legacy_seed()
        (persistent / 'var/log').symlink_to(self.guest / 'var/log')
        result = self.invoke(); self.assertEqual(result.returncode, 88)
        self.assertIn('Ungültiger persistenter Schreibpfad', result.stderr)
        (persistent / 'var/log').unlink()
        (persistent / '.readonly-layout-v1').write_text('1\n')
        result = self.invoke(); self.assertEqual(result.returncode, 88)
        self.assertIn('unvollständig', result.stderr)

    def test_fsck_binding_and_final_readonly_failure_block_handoff(self):
        for environment in ({'FAIL_FSCK':'4'}, {'FAIL_MOUNT':'bind'}, {'FAIL_MOUNT':'remount,ro'}):
            with self.subTest(environment=environment):
                result = self.invoke(**environment)
                self.assertEqual(result.returncode, 88, result.stderr)
                self.assertIn('Titan:', result.stderr)

    def test_non_ab_boot_does_not_mutate_the_filesystem(self):
        self.cmdline.write_text('root=UUID=development rw\n')
        result = self.invoke(); self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.mounts(), [])
        self.assertFalse((self.data / 'persistent').exists())

    def test_grub_fstab_and_factory_targets_preserve_readonly_contract(self):
        grub = (ROOT / 'image/debian/ab/grub.cfg').read_text()
        for slot in ('A','B'):
            self.assertRegex(grub, rf'linux /vmlinuz root=LABEL=TITAN-{slot} ro rauc.slot={slot}\b')
        fstab = (ROOT / 'image/debian/ab/fstab').read_text()
        self.assertIn('/dev/root / ext4 ro 0 0', fstab)
        self.assertNotRegex(fstab, r'TITAN-[AB]\s+/\s')
        configure = (ROOT / 'image/debian/ab/configure-ab.sh').read_text()
        targets = re.search(r'for task_path in (.*?); do', configure).group(1).strip().split()
        self.assertTrue(set(WRITABLE).issubset(targets))
        self.assertIn('tmp', targets)
        self.assertIn('systemd-tmpfiles --create -E', configure)
        for source in (SCRIPT.read_text(), configure):
            self.assertNotIn('remount,rw', source)


class FirstbootReadonlyTests(unittest.TestCase):
    def test_writable_system_slot_blocks_firstboot_before_service_or_configuration_changes(self):
        spec = importlib.util.spec_from_file_location('readonly_firstboot',ROOT / 'image/firstboot.py')
        module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            info = Path(temporary) / 'image-info.json'
            info.write_text(json.dumps({'platform':'debian-rauc','format':'titan-debian-ab-v1',
                                        'system_accounts':{'users':{},'groups':{}}}))
            for options in ('rw,relatime', 'ro,relatime'):
                with self.subTest(options=options):
                    def command(args, **kwargs):
                        if args == ['systemd-sysusers','/usr/lib/sysusers.d/titan.conf']:
                            raise RuntimeError('past-readonly-guard')
                        return SimpleNamespace(stdout=options if args[0]=='findmnt' else '')
                    with patch.object(module,'Path',return_value=info), patch.object(module.os,'geteuid',return_value=0), \
                         patch.object(module,'run',side_effect=command) as run:
                        expected = 'not read-only' if options.startswith('rw') else 'past-readonly-guard'
                        with self.assertRaisesRegex(RuntimeError,expected):module.main()
                    commands = [call.args[0] for call in run.call_args_list]
                    for mount in ('/var/cache','/var/log','/var/tmp','/var/spool','/var/lib/systemd','/var/lib/private','/var/lib/dbus','/var/lib/wtmpdb','/home','/tmp'):
                        self.assertIn(['mountpoint','-q',mount], commands)
                    self.assertEqual(any(cmd[0]=='systemd-sysusers' for cmd in commands), options.startswith('ro'))


if __name__ == '__main__':unittest.main()
