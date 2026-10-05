import copy
import unittest
from unittest.mock import patch

from titan.core import Error
from titan.docker_engine import DockerEngineMixin


class ContainerConsoleTests(unittest.TestCase):
    def setUp(self):
        self.row = {'Id':'a'*64,'Name':'/titan-custom-test',
                    'Config':{'Labels':{'io.titan.manual':'true'}},
                    'State':{'Running':True,'Status':'running'},'HostConfig':{},'Mounts':[]}
        row = self.row
        class Engine(DockerEngineMixin):
            def engine_action_target(self, container, action):
                self.requested = (container, action)
                return row, None
        self.engine = Engine()

    def test_only_verified_running_own_container_gets_a_guest_command(self):
        with patch('titan.docker_console.execute',return_value={'output':'guest','exit_code':0}) as execute:
            self.assertEqual(self.engine.op_docker_container_exec('a'*64,'pwd')['output'],'guest')
            execute.assert_called_once_with('a'*64,'pwd')
            self.assertEqual(self.engine.requested,('a'*64,'start'))

    def test_external_privileged_stopped_or_host_namespace_container_cannot_run_commands(self):
        for mutation in ('external','stopped','privileged','host-pid','host-ipc','sys-admin'):
            with self.subTest(mutation=mutation):
                self.setUp()
                if mutation=='external':self.row['Config']['Labels']={}
                if mutation=='stopped':self.row['State']={'Running':False,'Status':'exited'}
                if mutation=='privileged':self.row['HostConfig']['Privileged']=True
                if mutation=='host-pid':self.row['HostConfig']['PidMode']='host'
                if mutation=='host-ipc':self.row['HostConfig']['IpcMode']='host'
                if mutation=='sys-admin':self.row['HostConfig']['CapAdd']=['SYS_ADMIN']
                with patch('titan.docker_console.execute') as execute:
                    with self.assertRaises(Error):self.engine.op_docker_container_exec('a'*64,'pwd')
                    execute.assert_not_called()

    def test_unknown_storage_never_reaches_exec(self):
        self.row['Mounts']=[{'Type':'socket','Source':'/run/docker.sock'}]
        with patch('titan.docker_console.execute') as execute:
            with self.assertRaises(Error):self.engine.op_docker_container_exec('a'*64,'pwd')
            execute.assert_not_called()
