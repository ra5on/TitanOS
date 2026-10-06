"""Future autostart commitments cannot bypass current RAM admission."""
import copy
import json
import threading
import unittest
from unittest.mock import Mock, patch

from titan.app_memory import (GIB, MIB, check_boot_memory, check_container_start_memory,
    check_install_memory, check_start_memory, vm_boot_reservations, vm_overhead)
from titan.core import Error
from titan.docker_engine import DockerEngineMixin


def ram():
    return {'memory_total':8*GIB,'memory_available':7*GIB,'swap_total':100*GIB,
            'memory_pressure':{'some_avg10':0,'full_avg10':0}}


def vm(identifier='11111111-1111-4111-8111-111111111111', assigned=6*GIB):
    overhead=vm_overhead(assigned)
    return {'id':identifier,'assigned_bytes':assigned,'overhead_bytes':overhead,'limit_bytes':assigned+overhead}


def row(identifier='a'*64, memory=5*GIB, restart='always', state='exited', name='titan-custom-first'):
    return {'Id':identifier,'Name':'/'+name,'Config':{'Image':'nginx:stable','Labels':{'io.titan.manual':'true'},'Env':[]},
            'State':{'Status':state,'Running':state=='running'},
            'HostConfig':{'Memory':memory,'RestartPolicy':{'Name':restart},'NanoCpus':2*1000000000,
                          'NetworkMode':'bridge','PortBindings':{}},'NetworkSettings':{'Networks':{'bridge':{}}},
            'Mounts':[{'Type':'volume','Name':'titan-data','Destination':'/data','RW':True}]}


class BootBudgetTests(unittest.TestCase):
    def test_stopped_always_counts_and_swap_does_not_add_capacity(self):
        with self.assertRaisesRegex(Error,'Autostart-Budget') as failure:
            check_boot_memory([row(),row('b'*64,name='titan-custom-second')],vms=[],telemetry=ram())
        self.assertEqual(failure.exception.status,409)
        self.assertIn('Swap zählt nicht',str(failure.exception))

    def test_capacity_preview_is_structured_but_invalid_inventory_still_raises(self):
        rows = [row(), row('b'*64,name='titan-custom-second')]
        result = check_boot_memory(rows,vms=[],telemetry=ram(),raise_on_denial=False)
        self.assertFalse(result['allowed'])
        self.assertEqual(result['boot_container_limit_bytes'],10*GIB)
        self.assertIn('Autostart-Budget',result['reason'])
        with self.assertRaises(Error):
            check_boot_memory([{**row(),'HostConfig':{'Memory':GIB,'RestartPolicy':{'Name':'unknown'}}}],vms=[],telemetry=ram(),raise_on_denial=False)
        with self.assertRaises(Error):
            check_boot_memory([row(memory=0)],vms=[],telemetry=ram(),raise_on_denial=False)

    def test_unless_stopped_only_active_or_paused_and_on_failure_is_conservative(self):
        rows=[row(memory=GIB,restart='unless-stopped',state=state,identifier=letter*64,name=letter)
              for letter,state in zip('abcd',('exited','running','paused','restarting'))]
        rows += [row('e'*64,GIB,'no',name='e'),row('f'*64,GIB,'on-failure',name='f')]
        result=check_boot_memory(rows,vms=[],telemetry=ram())
        self.assertEqual(result['boot_container_limit_bytes'],4*GIB)

    def test_stopped_autostart_vms_count_full_memory_and_overhead(self):
        with self.assertRaisesRegex(Error,'Autostart-Budget'):
            check_boot_memory([],vms=[vm(),vm('22222222-2222-4222-8222-222222222222')],telemetry=ram())
        result=check_boot_memory([],vms=[vm()],telemetry=ram())
        self.assertEqual(result['boot_vm_limit_bytes'],6*GIB+vm_overhead(6*GIB))

    def test_vm_candidate_replaces_exact_uuid_and_can_disable_it(self):
        identifier=vm()['id']
        result=check_boot_memory([],vms=[vm()],telemetry=ram(),vm_override={'id':identifier,'assigned_bytes':2*GIB,'autostart':True})
        self.assertEqual(result['boot_vm_limit_bytes'],2*GIB+vm_overhead(2*GIB))
        result=check_boot_memory([],vms=[vm()],telemetry=ram(),vm_override={'id':identifier,'assigned_bytes':2*GIB,'autostart':False})
        self.assertEqual(result['boot_vm_limit_bytes'],0)

    def test_container_candidate_replaces_exact_id_or_new_definition_name_once(self):
        target=row()
        for identifier in (target['Id'],None):
            override={'id':identifier,'name':'titan-custom-first','limit_bytes':6*GIB,'restart':'always','active':True}
            result=check_boot_memory([target],vms=[],telemetry=ram(),container_overrides=[override])
            self.assertEqual(result['boot_container_limit_bytes'],6*GIB)
        with self.assertRaises(Error):
            check_boot_memory([target],vms=[],telemetry=ram(),container_overrides=[{**override,'id':'b'*64}])

    def test_unknown_policy_inventory_and_unbounded_future_limits_fail_closed(self):
        for change in ({'HostConfig':{'Memory':GIB}}, {'State':None}, {'HostConfig':{'Memory':0,'RestartPolicy':{'Name':'always'}}},
                       {'HostConfig':{'Memory':GIB,'RestartPolicy':{'Name':'unknown'}}}):
            with self.subTest(change=change),self.assertRaises(Error):
                check_boot_memory([{**row(),**change}],vms=[],telemetry=ram())
        result=check_boot_memory([row(memory=0,restart='unless-stopped')],vms=[],telemetry=ram())
        self.assertEqual(result['boot_container_limit_bytes'],0)

    def test_native_and_compose_start_include_stopped_future_commitments(self):
        existing=row(memory=6*GIB)
        with patch('titan.app_memory.vm_memory_reservations',return_value=[]):
            with self.assertRaisesRegex(Error,'Autostart-Budget'):
                check_container_start_memory(2*GIB,[existing],vms=[],telemetry=ram(),name='titan-custom-second',restart='unless-stopped')
            definition={'services':{'second':{'container_name':'titan-second','mem_limit':'2g','restart':'unless-stopped'}}}
            with self.assertRaisesRegex(Error,'Autostart-Budget'):
                check_start_memory('second',{},definition,[existing],vms=[],telemetry=ram())

    def test_small_template_install_is_blocked_by_a_stopped_future_vm_before_pull(self):
        with patch('titan.app_memory.vm_memory_reservations',return_value=[]),patch('titan.app_memory.vm_boot_reservations',return_value=[vm(assigned=7*GIB)]):
            with self.assertRaisesRegex(Error,'Autostart-Budget'):
                check_install_memory('titan-adguard',containers=[],vms=[],telemetry=ram())

    def test_autostart_vm_reader_includes_shut_off_guests_and_fails_closed(self):
        identifier=vm()['id']
        xml=f'<domain><uuid>{identifier}</uuid><memory unit="GiB">6</memory><currentMemory unit="MiB">256</currentMemory></domain>'
        run=Mock(side_effect=[identifier,xml])
        result=vm_boot_reservations(run,tool_present=True)
        self.assertEqual(run.call_args_list[0].args[0],['virsh','--connect','qemu:///system','list','--all','--autostart','--uuid'])
        self.assertEqual(result[0]['assigned_bytes'],6*GIB)
        with self.assertRaises(Error):vm_boot_reservations(Mock(side_effect=Error('offline')),tool_present=True)


class Engine(DockerEngineMixin):
    pass


class DockerBootAdmissionTests(unittest.TestCase):
    def setUp(self):
        self.engine=Engine();self.engine.app_memory_lock=threading.RLock();self.engine.telemetry=ram()
        self.rows={};self.calls=[];self.counter=0;self.before_create=None
        self.engine._app_inspected_containers=lambda:copy.deepcopy(list(self.rows.values()))
        self.engine.engine_container=lambda identifier:copy.deepcopy(self.rows[identifier])
        self.engine.engine_docker=self.docker
        self.engine.op_app_devices=Mock(return_value={'devices':[]})
        active=patch('titan.app_memory.vm_memory_reservations',return_value=[]);active.start();self.addCleanup(active.stop)
        future=patch('titan.app_memory.vm_boot_reservations',return_value=[]);self.future=future.start();self.addCleanup(future.stop)
        hardware=patch('titan.app_devices.devices',return_value=[]);hardware.start();self.addCleanup(hardware.stop)

    def docker(self,args,**kwargs):
        self.calls.append(list(args))
        if args[0]=='create':
            if self.before_create:self.before_create()
            self.counter+=1;identifier=f'{self.counter:064x}'
            name=args[args.index('--name')+1];memory=int(args[args.index('--memory')+1][:-1])*MIB
            policy=args[args.index('--restart')+1]
            self.rows[identifier]=row(identifier,memory,policy,'created',name)
            return identifier
        if args[0]=='start':self.rows[args[1]]['State']={'Status':'running','Running':True}
        if args[0]=='update':self.rows[args[-1]]['HostConfig']['RestartPolicy']['Name']=args[2]
        if args[0]=='rename':self.rows[args[1]]['Name']='/'+args[2]
        if args[0]=='commit':return 'sha256:'+'c'*64
        if args[:2]==['volume','inspect']:return json.dumps([{'Driver':'local','Options':{}}])
        if args[0]=='ps':
            match=args[-1].removeprefix('name=^/').removesuffix('$') if '--filter' in args else None
            return '\n'.join(identifier for identifier,item in self.rows.items() if match is None or item['Name']=='/'+match)
        return ''

    def mutations(self):
        return [args[0] for args in self.calls if args[0] in ('create','start','commit','rename','update','rm')]

    def test_create_does_not_add_a_second_five_gb_autoboot_container(self):
        self.rows['a'*64]=row()
        with self.assertRaisesRegex(Error,'Autostart-Budget'):
            self.engine.op_docker_container_create({'name':'second','image':'nginx:stable','memory_mb':5120,'restart':'always'})
        self.assertEqual(self.calls,[])

    def test_future_budget_changed_during_image_create_leaves_container_stopped(self):
        self.future.side_effect=[[],[vm()]]
        with self.assertRaisesRegex(Error,'Autostart-Budget'):
            self.engine.op_docker_container_create({'name':'second','image':'nginx:stable','memory_mb':2048})
        self.assertEqual([args[0] for args in self.calls],['create'])
        self.assertEqual(next(iter(self.rows.values()))['State']['Status'],'created')

    def test_parallel_creates_cannot_both_pass_before_either_start(self):
        started=threading.Event();release=threading.Event();outcomes=[]
        def wait():started.set();self.assertTrue(release.wait(3))
        self.before_create=wait
        def create(name):
            try:self.engine.op_docker_container_create({'name':name,'image':'nginx:stable','memory_mb':5120});outcomes.append('ok')
            except Error:outcomes.append('blocked')
        first=threading.Thread(target=create,args=('first',));second=threading.Thread(target=create,args=('second',))
        first.start();self.assertTrue(started.wait(1));second.start()
        self.assertEqual([call[0] for call in self.calls],['create'])
        release.set();first.join(3);second.join(3)
        self.assertFalse(first.is_alive() or second.is_alive())
        self.assertCountEqual(outcomes,['ok','blocked'])
        self.assertEqual(sum(call[0]=='create' for call in self.calls),1)

    def test_policy_change_is_checked_before_snapshot_and_after_long_snapshot(self):
        self.rows['a'*64]=row(restart='no');self.rows['b'*64]=row('b'*64,3*GIB,'always',name='titan-custom-other')
        with self.assertRaisesRegex(Error,'Autostart-Budget'):
            self.engine.op_docker_container_settings('a'*64,{'restart':'always'})
        self.assertEqual(self.mutations(),[])
        self.rows.pop('b'*64);self.future.side_effect=[[],[vm()]];self.calls=[]
        with self.assertRaisesRegex(Error,'Autostart-Budget'):
            self.engine.op_docker_container_settings('a'*64,{'restart':'always'})
        self.assertEqual(self.mutations(),['commit'])

    def test_recreation_counts_replacement_once_and_backup_never_autostarts(self):
        for action in ('settings','hardware'):
            with self.subTest(action=action):
                self.rows={'a'*64:row(),'b'*64:row('b'*64,2*GIB,name='titan-custom-other')};self.calls=[]
                if action=='settings':result=self.engine.op_docker_container_settings('a'*64,{'memory_mb':5120})
                else:result=self.engine.op_docker_container_hardware('a'*64,[])
                self.assertEqual(self.rows['a'*64]['HostConfig']['RestartPolicy']['Name'],'no')
                self.assertEqual(self.rows[result['container']]['HostConfig']['RestartPolicy']['Name'],'always')
                self.assertIn('deaktiviertem Autostart',result['message'])
                self.assertEqual(check_boot_memory(list(self.rows.values()),vms=[],telemetry=ram())['boot_container_limit_bytes'],7*GIB)

    def test_failed_replacement_restores_original_policy_and_name(self):
        self.rows['a'*64]=row()
        self.before_create=lambda:(_ for _ in ()).throw(Error('create failed'))
        with self.assertRaisesRegex(Error,'wiederhergestellt'):
            self.engine.op_docker_container_settings('a'*64,{'memory_mb':5120})
        self.assertEqual(self.rows['a'*64]['Name'],'/titan-custom-first')
        self.assertEqual(self.rows['a'*64]['HostConfig']['RestartPolicy']['Name'],'always')


if __name__=='__main__':unittest.main()
