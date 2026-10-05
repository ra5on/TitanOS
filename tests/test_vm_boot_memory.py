"""VM mutations must respect the shared future Docker/libvirt boot budget."""
import threading
import unittest
from unittest.mock import Mock, patch
import xml.etree.ElementTree as ET

from titan.app_memory import GIB, MIB, vm_overhead
from titan.core import Error
import test_vm_management as management

OTHER='11111111-2222-3333-4444-555555555555'


def reservation(identifier, assigned):
    overhead=vm_overhead(assigned)
    return {'id':identifier,'assigned_bytes':assigned,'overhead_bytes':overhead,'limit_bytes':assigned+overhead}


class VMBootBudgetTests(unittest.TestCase):
    setUp=management.VMManagementTests.setUp
    tearDown=management.VMManagementTests.tearDown
    command=management.VMManagementTests.command

    def boot_context(self, vms=None, containers=None):
        from contextlib import ExitStack
        context=ExitStack()
        context.enter_context(patch('titan.app_memory.memory_snapshot',return_value={
            'memory_total':8*GIB,'memory_available':7*GIB,'swap_total':64*GIB}))
        context.enter_context(patch('titan.app_memory.vm_boot_reservations',return_value=vms or []))
        context.enter_context(patch.object(self.host,'_app_inspected_containers',return_value=containers or []))
        return context

    def test_stopped_six_gib_autostart_guest_blocks_another_on_eight_gib(self):
        root=ET.fromstring(self.xml);root.find('memory').text=str(6144*1024)
        self.xml=ET.tostring(root,encoding='unicode');self.metadata.write_text(self.xml)
        with self.boot_context([reservation(OTHER,6*GIB)]):
            with self.assertRaises(Error) as caught:self.host.op_vm_action(self.vm_id,'autostart')
        self.assertEqual(caught.exception.status,409)
        self.assertFalse(any(call[:2]==['virsh','autostart'] for call in self.calls))
        self.assertEqual(self.metadata.read_text(),self.xml)

    def test_autostart_enable_includes_stopped_always_container(self):
        container={'Id':'d'*64,'Name':'/large','State':{'Status':'exited','Running':False},
            'HostConfig':{'Memory':6*GIB,'RestartPolicy':{'Name':'always'}}}
        with self.boot_context(containers=[container]):
            with self.assertRaises(Error) as caught:self.host.op_vm_action(self.vm_id,'autostart')
        self.assertEqual(caught.exception.status,409)
        self.assertFalse(any(call[:2]==['virsh','autostart'] for call in self.calls))

    def test_allowed_autostart_has_fresh_budget_before_libvirt_mutation(self):
        with self.boot_context(),patch('titan.app_memory.check_boot_memory',return_value={'allowed':True}) as check:
            self.host.op_vm_action(self.vm_id,'autostart')
        self.assertEqual(check.call_args.kwargs['vm_override'],{
            'id':self.vm_id,'assigned_bytes':2*GIB,'autostart':True})
        self.assertIn(['virsh','autostart',self.vm_id],self.calls)

    def test_disabling_autostart_does_not_require_docker_or_memory_inventory(self):
        with patch('titan.app_memory.vm_boot_reservations',side_effect=Error('unknown',503)) as future, \
                patch.object(self.host,'_app_inspected_containers',side_effect=Error('Docker offline',503)) as docker, \
                patch('titan.app_memory.memory_snapshot',side_effect=Error('RAM unknown',503)) as ram:
            self.host.op_vm_action(self.vm_id,'disable-autostart')
        future.assert_not_called();docker.assert_not_called();ram.assert_not_called()
        self.assertIn(['virsh','autostart','--disable',self.vm_id],self.calls)

    def test_unknown_boot_inventory_blocks_enable_without_mutating(self):
        with self.boot_context(),patch('titan.app_memory.vm_boot_reservations',side_effect=Error('unknown boot VMs',503)):
            with self.assertRaises(Error) as caught:self.host.op_vm_action(self.vm_id,'autostart')
        self.assertEqual(caught.exception.status,503)
        self.assertFalse(any(call[:2]==['virsh','autostart'] for call in self.calls))

    def test_autostart_ram_increase_cannot_overcommit_or_change_xml(self):
        future=[reservation(self.vm_id,2*GIB),reservation(OTHER,2*GIB)]
        with self.boot_context(future):
            with self.assertRaises(Error) as caught:self.host.op_vm_update(self.vm_id,2,6144)
        self.assertEqual(caught.exception.status,409)
        self.assertEqual(self.metadata.read_text(),self.xml)
        self.assertFalse(any(call[:2]==['virsh','define'] for call in self.calls))

    def test_allowed_autostart_ram_change_replaces_existing_commitment(self):
        future=[reservation(self.vm_id,2*GIB),reservation(OTHER,2*GIB)]
        with self.boot_context(future):self.host.op_vm_update(self.vm_id,2,3072)
        self.assertEqual(ET.fromstring(self.metadata.read_text()).findtext('memory'),str(3072*1024))
        self.assertIn(['virsh','define',str(self.metadata)],self.calls)

    def test_non_autostart_ram_edit_does_not_reserve_boot_ram(self):
        with self.boot_context([reservation(OTHER,6*GIB)]),patch('titan.app_memory.check_boot_memory') as check:
            self.host.op_vm_update(self.vm_id,2,6144)
        check.assert_not_called()
        self.assertEqual(ET.fromstring(self.metadata.read_text()).findtext('memory'),str(6144*1024))

    def test_snapshot_definition_cannot_bypass_autostart_memory_gate(self):
        record=self.host.managed_vm(self.vm_id)
        root=ET.fromstring(self.xml);root.find('memory').text=str(6144*1024)
        with self.boot_context([reservation(self.vm_id,2*GIB),reservation(OTHER,2*GIB)]):
            with self.assertRaises(Error):self.host.redefine_vm(record,root)
        self.assertEqual(self.metadata.read_text(),self.xml)
        self.assertFalse(any(call[:2]==['virsh','define'] for call in self.calls))

    def test_exact_bytes_are_used_for_future_admission(self):
        root=ET.fromstring(self.xml);root.find('memory').set('unit','bytes')
        root.find('memory').text=str(2*GIB+1)
        self.xml=ET.tostring(root,encoding='unicode')
        with self.boot_context(),patch('titan.app_memory.check_boot_memory',return_value={'allowed':True}) as check:
            self.host.op_vm_action(self.vm_id,'autostart')
        self.assertEqual(check.call_args.kwargs['vm_override']['assigned_bytes'],2*GIB+1)
        self.assertEqual(self.host.vm_memory_mb(root),2048)

    def test_start_and_resume_block_before_network_when_future_boot_budget_invalid(self):
        for action in ('start','resume'):
            with self.subTest(action=action),self.boot_context([reservation(OTHER,8*GIB)]), \
                    patch.object(self.host,'op_vms',return_value={'available':True}), \
                    patch.object(self.host,'prepare_vm_storage_access') as storage, \
                    patch('titan.app_memory.check_vm_start_memory') as current:
                self.calls.clear()
                with self.assertRaises(Error):self.host.op_vm_action(self.vm_id,action)
                current.assert_not_called();storage.assert_not_called()
                self.assertFalse(any(call[:2]==['virsh',action] for call in self.calls))

    def test_shared_admission_lock_blocks_parallel_vm_enable_until_released(self):
        started,queried,finished=threading.Event(),threading.Event(),threading.Event()
        failures=[]
        def inventory(*args,**kwargs):queried.set();return []
        def enable():
            started.set()
            try:self.host.op_vm_action(self.vm_id,'autostart')
            except Exception as error:failures.append(error)
            finally:finished.set()
        with self.boot_context(),patch('titan.app_memory.vm_boot_reservations',side_effect=inventory):
            with self.host.app_memory_lock:
                thread=threading.Thread(target=enable);thread.start()
                self.assertTrue(started.wait(2))
                self.assertFalse(queried.wait(.05))
            self.assertTrue(finished.wait(2));thread.join(2)
        self.assertEqual(failures,[])
        self.assertTrue(queried.is_set())


if __name__=='__main__':unittest.main()
