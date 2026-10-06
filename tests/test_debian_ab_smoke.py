import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import Mock, patch


spec = importlib.util.spec_from_file_location('titan_ab_smoke', Path(__file__).resolve().parents[1] / 'scripts/smoke-debian-ab.py')
ab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ab)


class DebianABSmokeTests(unittest.TestCase):
    def test_image_and_system_baselines_have_distinct_provenance(self):
        image = ab.published_baseline_metadata('published-image', '0.4.6-alpha.1', 'a' * 64)
        self.assertEqual(image['baseline_source'], 'published-release')
        self.assertNotIn('baseline_bundle_sha256', image)
        system = ab.published_baseline_metadata('published-system', '0.5.2-alpha.1', 'a' * 64, 'b' * 64)
        self.assertEqual(system['baseline_source'], 'published-system-release')
        self.assertEqual(system['baseline_sha256'], 'a' * 64)
        self.assertEqual(system['baseline_bundle_sha256'], 'b' * 64)

    def test_incomplete_or_invalid_published_provenance_is_rejected(self):
        for args in (('synthetic', '0.5.2-alpha.1', 'a' * 64), ('published-image', 'latest', 'a' * 64), ('published-image', '0.5.2-alpha.1', 'no-hash'), ('published-image', '0.5.2-alpha.1', 'a' * 64, 'b' * 64), ('published-system', '0.5.2-alpha.1', 'a' * 64), ('published-system', '0.5.2-alpha.1', 'a' * 64, 'B' * 64)):
            with self.subTest(args=args):
                with self.assertRaises(ValueError): ab.published_baseline_metadata(*args)

    def test_system_baseline_cli_requires_image_and_sha_before_any_mutation(self):
        for extra in (['--baseline-kind', 'published-system'], ['--baseline-kind', 'published-system', '--baseline-image', '/tmp/baseline.img'], ['--baseline-kind', 'published-system', '--baseline-image', '/tmp/baseline.img', '--baseline-bundle-sha256', 'bad']):
            with patch.object(ab.os, 'environ', {'GITHUB_ACTIONS': 'true'}), patch('sys.argv', ['smoke-debian-ab.py', '/tmp/candidate.img', '/tmp/candidate.raucb', '--confirm-disposable-guest', *extra]), patch.object(ab, 'command') as command:
                with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as error: ab.main()
                self.assertEqual(error.exception.code, 2)
                command.assert_not_called()

    def test_disposable_cold_boots_change_only_real_qemu_memory(self):
        with tempfile.TemporaryDirectory() as temporary:
            work=Path(temporary)
            with patch.object(ab.subprocess,'Popen',return_value=Mock()) as launch:
                ab.launch_guest(work,'tcg',8192);first=launch.call_args.args[0]
                ab.launch_guest(work,'tcg',3072);second=launch.call_args.args[0]
            index=first.index('-m')+1
            self.assertEqual(first[index],'8192');self.assertEqual(second[index],'3072')
            self.assertEqual(first[:index]+first[index+1:],second[:index]+second[index+1:])
            self.assertIn('id=titan-system,if=virtio,format=qcow2,file='+str(work/'test.qcow2'),first)
            self.assertIn('virtserialport,chardev=qga,name=org.qemu.guest_agent.0',first)
            self.assertNotIn('meminfo',' '.join(first))

    def test_cold_boot_rejects_unsupported_ram_and_replaced_socket(self):
        with tempfile.TemporaryDirectory() as temporary:
            work=Path(temporary)
            with patch.object(ab.subprocess,'Popen') as launch:
                for memory in (0,4096,True,'3072'):
                    with self.subTest(memory=memory),self.assertRaises(ValueError):ab.launch_guest(work,'tcg',memory)
                (work/'qga.sock').write_text('unexpected replacement')
                with self.assertRaises(RuntimeError):ab.launch_guest(work,'tcg',3072)
            launch.assert_not_called()

    def test_private_test_image_uses_bounded_guest_elf_files_without_a_registry(self):
        ldd=subprocess.check_output(['/usr/bin/ldd','/bin/sleep'],text=True)
        inspect={'State':{'Running':False},'HostConfig':{'Memory':4*1024**3,'MemorySwap':4*1024**3,'RestartPolicy':{'Name':'always'}}}
        def output(args,**kwargs):
            if args[:2]==['docker','ps']:return ''
            if args[0]=='/usr/bin/ldd':return ldd
            if args[:2]==['docker','create']:return 'a'*64+'\n'
            if args[:2]==['docker','inspect']:return json.dumps([inspect])
            raise AssertionError('Unexpected command')
        with patch.object(subprocess,'check_output',side_effect=output),patch.object(subprocess,'run') as run,patch.object(ab.time,'sleep') as sleep,contextlib.redirect_stdout(io.StringIO()) as printed:
            exec(ab.GUEST_BOOT_MEMORY_SETUP,{})
        self.assertEqual(printed.getvalue(),'prepared\n')
        self.assertEqual(run.call_args_list[0].args[0],['docker','import','-','titan-ci-boot-memory:local'])
        self.assertEqual(run.call_args_list[1].args[0],['docker','start','a'*64])
        self.assertEqual(run.call_args_list[2].args[0],['docker','stop','--time','5','a'*64])
        sleep.assert_called_once_with(11)
        payload=run.call_args_list[0].kwargs['input'];self.assertLess(len(payload),17*1024**2)
        with tarfile.open(fileobj=io.BytesIO(payload)) as image:
            self.assertEqual(image.extractfile('bin/sleep').read(),Path('/bin/sleep').read_bytes())
            self.assertTrue(all(not member.name.startswith('/') and '..' not in Path(member.name).parts for member in image))


class BootMemoryLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.report={'ok':False,'reason':'insufficient_memory','total_bytes':3*1024**3-1024**2,
            'reserve_bytes':512*1024**2,'required_bytes':4*1024**3+512*1024**2}
        self.observation={'report':self.report,'services':{unit:{'active':False,'failed':True,'guard_failed':True}
            for unit in ('docker.service','libvirtd.service')}}
        self.agent=Mock();self.agent.ready.side_effect=[('three-gib-boot',{}),('eight-gib-boot',{})]
        def python(code,timeout=None):
            if code==ab.GUEST_BOOT_MEMORY_SETUP:return 'prepared\n'
            if code==ab.GUEST_BOOT_MEMORY_OBSERVE:return json.dumps(self.observation)
            if "print('recovered')" in code:return 'recovered\n'
            if "print('clean')" in code:return 'clean\n'
            raise AssertionError('Unexpected guest proof')
        self.agent.python.side_effect=python
        self.client=Mock();self.metrics=iter((8*1024**3-1024**2,self.report['total_bytes'],8*1024**3-1024**2))
        self.listing={'entries':[{'name':'rollback-sentinel'}]}
        def request(path,**kwargs):
            if path=='/api/status':return {'demo':False,'memory_total':next(self.metrics)}
            if path=='/api/session':return {'user':{'role':'admin'}}
            if path.startswith('/api/files'):return self.listing
            if path=='/api/components':return {'components':{'docker':{'daemon':True},'vms':{'daemon':True}}}
            raise AssertionError('Unexpected guest API')
        self.client.request.side_effect=request
        self.restart=Mock()

    def test_cold_boot_proof_requires_real_resource_changes_recovery_and_cleanup(self):
        boot,proof=ab.boot_memory_lifecycle(self.agent,self.client,self.restart,'B','3.0.0','previous-boot')
        self.assertEqual(boot,'eight-gib-boot')
        self.assertEqual(proof,{key:True for key in ab.BOOT_MEMORY_FIELDS})
        self.assertEqual([call.args for call in self.restart.call_args_list],[(3072,),(8192,)])
        self.assertEqual(self.agent.ready.call_args_list[0].kwargs,{'require_health':False})
        self.assertEqual(self.agent.ready.call_args_list[1].args,('B','3.0.0','three-gib-boot'))
        self.assertEqual(self.agent.ready.call_args_list[1].kwargs,{})
        self.assertEqual([call.args[0] for call in self.agent.execute.call_args_list],
            [['/usr/bin/docker','rm','--force','titan-ci-boot-memory'],['/usr/bin/docker','image','rm','titan-ci-boot-memory:local']])
        self.assertNotIn('previous-boot',json.dumps(proof));self.assertNotIn('/var',json.dumps(proof))

    def test_unavailable_file_manager_prevents_success_and_ram_recovery_claim(self):
        self.listing.clear()
        with self.assertRaisesRegex(RuntimeError,'file manager'):ab.boot_memory_lifecycle(self.agent,self.client,self.restart,'B','3.0.0','previous-boot')
        self.assertEqual([call.args for call in self.restart.call_args_list],[(3072,)])

    def test_initial_small_or_simulated_guest_cannot_claim_real_reduced_ram_boot(self):
        for value in ({'demo':True,'memory_total':8*1024**3},{'demo':False,'memory_total':3*1024**3}):
            with self.subTest(value=value):
                self.client.request.side_effect=None;self.client.request.return_value=value
                with self.assertRaises(RuntimeError):ab.boot_memory_lifecycle(self.agent,self.client,self.restart,'B','3.0.0','old')
        self.agent.python.assert_not_called();self.restart.assert_not_called()

    def test_daemon_failure_or_guard_reason_cannot_be_invented_by_overall_success(self):
        self.assertEqual(ab.blocked_boot_observation(self.observation),self.report)
        for field,value in (('ok',True),('ok',0),('reason','invalid_state'),('total_bytes',8*1024**3),
                            ('reserve_bytes',True),('required_bytes',4*1024**3)):
            with self.subTest(field=field,value=value):
                changed={'report':{**self.report,field:value},'services':self.observation['services']}
                self.assertIsNone(ab.blocked_boot_observation(changed))
        for unit in ('docker.service','libvirtd.service'):
            for key,value in (('active',True),('active',0),('failed',False),('failed',1),('guard_failed',False),('guard_failed',1)):
                changed=json.loads(json.dumps(self.observation));changed['services'][unit][key]=value
                with self.subTest(unit=unit,key=key,value=value):self.assertIsNone(ab.blocked_boot_observation(changed))

    def test_image_baseline_cannot_claim_system_bundle_provenance(self):
        with patch('sys.argv', ['smoke-debian-ab.py', '/tmp/candidate.img', '/tmp/candidate.raucb', '--baseline-bundle-sha256', 'b' * 64]), patch.object(ab, 'command') as command:
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as error: ab.main()
            self.assertEqual(error.exception.code, 2)
            command.assert_not_called()


class BootMemoryUnitObservationTests(unittest.TestCase):
    def observe(self, result='exit-code', status='1', command=None, state='failed'):
        report={'ok':False,'reason':'insufficient_memory','total_bytes':3*1024**3,
                'reserve_bytes':512*1024**2,'required_bytes':4*1024**3+512*1024**2}
        command=command or '/usr/bin/python3 /usr/share/titan/boot-memory-guard.py --component all'
        unit=(f'ActiveState={state}\nResult={result}\nExecStartPre={{ path=/usr/bin/python3 ; '
              f'argv[]={command} ; ignore_errors=no ; code=exited ; status={status} }}\n')
        with tempfile.TemporaryDirectory() as temporary:
            path=Path(temporary)/'report.json';path.write_text(json.dumps(report));path.chmod(0o644)
            original=os.fstat
            def owned(fd):
                info=original(fd)
                return SimpleNamespace(st_mode=info.st_mode,st_uid=0,st_size=info.st_size)
            script=ab.GUEST_BOOT_MEMORY_OBSERVE.replace('/run/titan-boot-memory.json',str(path))
            with patch.object(os,'fstat',side_effect=owned),patch.object(subprocess,'check_output',return_value=unit),contextlib.redirect_stdout(io.StringIO()) as output:
                exec(script,{})
        return json.loads(output.getvalue())

    def test_guard_restart_limit_is_an_observed_block_not_a_daemon_timeout(self):
        for result in ('exit-code','start-limit-hit'):
            with self.subTest(result=result):
                self.assertIsNotNone(ab.blocked_boot_observation(self.observe(result=result)))
        for result in ('timeout','signal','success','watchdog'):
            with self.subTest(result=result):
                self.assertIsNone(ab.blocked_boot_observation(self.observe(result=result)))
        self.assertIsNone(ab.blocked_boot_observation(self.observe(state='activating')))

    def test_similar_command_or_exit_status_does_not_claim_the_guard_ran(self):
        for changes in ({'status':'12'},{'status':'0'},
                        {'command':'/usr/bin/python3 /tmp/boot-memory-guard.py --component all'},
                        {'command':'/usr/bin/python3 /usr/share/titan/boot-memory-guard.py --component all --extra'},
                        {'command':'/usr/bin/python3 /usr/share/titan/boot-memory-guard.py --component docker'}):
            with self.subTest(changes=changes):
                self.assertIsNone(ab.blocked_boot_observation(self.observe(**changes)))


if __name__ == '__main__':
    unittest.main()
