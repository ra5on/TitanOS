"""Final offline identity cleanup must survive all guest customizations."""
import hashlib
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
BUILDER=ROOT/'scripts/build-debian-ab-image.sh'


class DistributionIdentityTests(unittest.TestCase):
    def cleanup(self):
        source=BUILDER.read_text()
        return re.search(r'''guestfish --rw -a "\$task_base" -m "\$task_root" <<'IDENTITY'\n.*?\nIDENTITY\n''',source,re.S).group(0)

    def test_cleanup_follows_last_customize_and_verified_root_before_raw_export(self):
        source=BUILDER.read_text();cleanup=self.cleanup()
        at=source.index(cleanup)
        self.assertGreater(at,source.rindex('virt-customize '))
        self.assertGreater(at,source.index('[[ "$task_root" == /dev/sda3 ]]'))
        self.assertLess(at,source.index('run : download "$task_root" "$task_dir/rootfs.ext4"'))
        self.assertNotIn('--run-command',cleanup)
        self.assertNotIn('virt-customize',source[at:])
        self.assertIn('umount-all',cleanup)
        self.assertIn('sync',cleanup)
        # Distribution guard remains in the real boot test, before an overlay.
        smoke=(ROOT/'scripts/smoke-image.sh').read_text()
        guard='[[ -z "$task_machine_id" ]] ||'
        self.assertIn(guard,smoke)
        self.assertLess(smoke.index(guard),smoke.index('qemu-img create'))
        self.assertIn('Distribution image contains a fixed machine ID.',smoke)

    def test_final_commands_remove_regenerated_ids_and_keys_without_touching_os_or_persist_hooks(self):
        with tempfile.TemporaryDirectory() as temporary:
            work=Path(temporary);guest=work/'guest';bin_dir=work/'bin';bin_dir.mkdir()
            contents={'etc/machine-id':'0123456789abcdef0123456789abcdef\n',
                      'var/lib/systemd/random-seed':'private appliance seed',
                      'etc/ssh/ssh_host_ed25519_key':'private appliance key',
                      'etc/ssh/ssh_host_ed25519_key.pub':'appliance public key',
                      'etc/ssh/ssh_host_rsa_key':'private second key',
                      'etc/titan/web.env':'configuration preserved',
                      'etc/fstab':'persistent partition bindings',
                      'usr/share/titan/image-info.json':'signed release identity',
                      'usr/share/titan/image-info.json.sig':'release signature',
                      'usr/share/titan/rauc-root.pem':'release trust root',
                      'var/lib/titan-agent/config.json':'package control state',
                      'usr/share/initramfs-tools/scripts/init-bottom/titan-persist':'persistent init hook'}
            for name,value in contents.items():
                path=guest/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_text(value)
            dbus=guest/'var/lib/dbus/machine-id';dbus.parent.mkdir(parents=True);dbus.symlink_to('../../../etc/machine-id')
            unaffected={name:hashlib.sha256((guest/name).read_bytes()).hexdigest() for name in contents if not name.startswith(('etc/machine-id','var/lib/systemd/random-seed','etc/ssh/ssh_host_'))}
            fake=bin_dir/'guestfish'
            fake.write_text('''#!/usr/bin/env python3
import os,sys,shlex
from pathlib import Path
root=Path(os.environ['FIXTURE_GUEST'])
assert sys.argv[1:]==['--rw','-a',os.environ['FIXTURE_IMAGE'],'-m','/dev/sda3']
if os.environ.get('FIXTURE_IO_FAILURE'):sys.exit(17)
for line in sys.stdin:
    args=shlex.split(line)
    if not args:continue
    if args[0]=='truncate':
        target=root/args[1].lstrip('/');assert target.is_file();target.write_bytes(b'')
    elif args[0]=='rm-f':(root/args[1].lstrip('/')).unlink(missing_ok=True)
    elif args[:2]==['glob','rm-f']:
        for target in root.glob(args[2].lstrip('/')):target.unlink()
    else:assert args in (['sync'],['umount-all'])
''');fake.chmod(0o755)
            image=work/'private-base.img';image.write_bytes(b'regular image fixture')
            environment={**os.environ,'PATH':str(bin_dir)+os.pathsep+os.environ['PATH'],
                         'FIXTURE_GUEST':str(guest),'FIXTURE_IMAGE':str(image)}
            script='set -euo pipefail\ntask_base="$FIXTURE_IMAGE"\ntask_root=/dev/sda3\n'+self.cleanup()+'\nprintf cleaned\n'
            for _ in range(2):
                result=subprocess.run(['bash'],input=script,env=environment,text=True,capture_output=True)
                self.assertEqual(result.returncode,0,result.stderr)
                self.assertEqual(result.stdout,'cleaned')
                self.assertEqual((guest/'etc/machine-id').read_bytes(),b'')
                self.assertFalse(dbus.is_symlink())
                self.assertFalse((guest/'var/lib/systemd/random-seed').exists())
                self.assertEqual(list((guest/'etc/ssh').glob('ssh_host_*')),[])
                for name,digest in unaffected.items():self.assertEqual(hashlib.sha256((guest/name).read_bytes()).hexdigest(),digest)
            failed=subprocess.run(['bash'],input=script,env={**environment,'FIXTURE_IO_FAILURE':'true'},text=True,capture_output=True)
            self.assertEqual(failed.returncode,17)
            self.assertNotIn('cleaned',failed.stdout,'An offline cleanup I/O error cannot continue to export')


if __name__=='__main__':unittest.main()
