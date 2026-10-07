"""Exercise EFI recovery metadata and safe first-stage migration."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT/'packages/os/rugix/recipes/titanos-boot/files/titan-recovery-menu.py'
SPEC = importlib.util.spec_from_file_location('titan_recovery_menu', SOURCE)
MENU = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(MENU)


class RecoveryMenuTests(unittest.TestCase):
    def setUp(self):
        self.temporary=tempfile.TemporaryDirectory(); self.addCleanup(self.temporary.cleanup)
        self.root=Path(self.temporary.name); self.config=self.root/'config'; (self.config/'rugpi').mkdir(parents=True)
        (self.config/'rugpi/primary.grubenv').write_bytes(b'native default must remain unchanged')
        self.old=b'original first stage'; (self.config/'rugpi/grub.cfg').write_bytes(self.old)
        self.loader=self.root/'first.grub.cfg'; self.loader.write_bytes(b'# Titan recovery menu.\nnew loader')
        self.mount={'filesystems':[{'target':str(self.config),'fstype':'vfat','options':'ro,nosuid,nodev'}]}
        self.metadata={'versions':{'a':'2.0.3','b':'2.0.4'},'previous':{'slot':'a','version':'2.0.3'}}

    def sync(self):
        with patch.object(MENU,'CONFIG',self.config),patch.object(MENU,'LOADER',self.loader),patch.object(MENU.os,'geteuid',return_value=0),\
             patch.object(MENU.subprocess,'check_output',return_value=json.dumps(self.mount)),patch.object(MENU.subprocess,'run') as command:
            MENU.sync_recovery_menu(self.metadata)
            return command

    def test_atomic_migration_restores_read_only_mount_and_preserves_native_default(self):
        command=self.sync()
        self.assertEqual([call.args[0] for call in command.call_args_list],
                         [['mount','-o','remount,rw',str(self.config)],['mount','-o','remount,ro',str(self.config)]])
        self.assertEqual((self.config/'rugpi/grub.cfg').read_bytes(),self.loader.read_bytes())
        self.assertEqual((self.config/'rugpi/primary.grubenv').read_bytes(),b'native default must remain unchanged')
        env=(self.config/'rugpi/titan-menu.grubenv').read_bytes()
        self.assertEqual(len(env),1024); self.assertIn(b'titan_rollback_part=2\n',env)
        self.assertFalse(list((self.config/'rugpi').glob('.titan-menu-*')))

    def test_invalidation_removes_previous_entry_instead_of_reusing_stale_data(self):
        self.sync(); self.metadata={'versions':{'b':'2.0.4'},'previous':None}; self.sync()
        env=(self.config/'rugpi/titan-menu.grubenv').read_bytes()
        self.assertNotIn(b'rollback',env); self.assertNotIn(b'titan_a_version',env)

    def test_versions_and_slot_selection_cannot_inject_grub_commands(self):
        for metadata in ({'versions':{'a':'2.0.4\nset root=evil'},'previous':None},
                         {'versions':{'c':'2.0.4'},'previous':None},
                         {'versions':{'a':'2.0.4'},'previous':{'slot':'b','version':'2.0.3'}},
                         {'versions':{'a':'2.0.4'},'previous':{'slot':'a','version':'2.0.4'}}):
            with self.subTest(metadata=metadata),self.assertRaises(ValueError): MENU.menu_values(metadata)

    def test_non_efi_mount_is_rejected_before_any_write(self):
        for filesystem in ('ext4','tmpfs'):
            self.mount['filesystems'][0]['fstype']=filesystem
            with self.subTest(filesystem=filesystem),self.assertRaises(ValueError): self.sync()
            self.assertEqual((self.config/'rugpi/grub.cfg').read_bytes(),self.old)

    def test_failed_write_still_restores_read_only_mount(self):
        with patch.object(MENU,'atomic_write',side_effect=OSError('disk full')):
            with patch.object(MENU,'CONFIG',self.config),patch.object(MENU,'LOADER',self.loader),patch.object(MENU.os,'geteuid',return_value=0),\
                 patch.object(MENU.subprocess,'check_output',return_value=json.dumps(self.mount)),patch.object(MENU.subprocess,'run') as command:
                with self.assertRaises(OSError): MENU.sync_recovery_menu(self.metadata)
                self.assertEqual(command.call_args_list[-1].args[0],['mount','-o','remount,ro',str(self.config)])

    @unittest.skipUnless(shutil.which('grub-editenv'),'GRUB is required for actual environment compatibility')
    def test_native_grub_reads_only_validated_presentation_values(self):
        path=self.root/'menu.grubenv'; path.write_bytes(MENU.encode_environment(MENU.menu_values(self.metadata)))
        result=subprocess.check_output(['grub-editenv',str(path),'list'],text=True)
        self.assertIn('titan_rollback_part=2\n',result); self.assertIn('titan_b_version=2.0.4\n',result)
        self.assertNotIn('rugpi_bootpart=',result)

    @unittest.skipUnless(shutil.which('grub-script-check'),'GRUB is required for actual script validation')
    def test_native_grub_accepts_the_recovery_first_stage(self):
        subprocess.run(['grub-script-check',str(SOURCE.with_name('first.grub.cfg'))],check=True,capture_output=True)


if __name__=='__main__': unittest.main()
