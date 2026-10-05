"""Real settings controller rejects unsafe edits and recovers failed recreation."""
import copy
import json
from unittest.mock import Mock, patch
from titan.core import Error
from titan.docker_settings import public_settings
from test_docker_engine import EngineTests, ID


class DockerSettingsTests(EngineTests):
    def configure(self):
        self.row = self.manual_stopped()
        self.row['Mounts'] = [{'Type':'bind','RW':True,'Source':'/var/srv/titan/custom/web/data','Destination':'/data'}]
        self.engine._engine_storage_ready = Mock(return_value={'id':'system','path':'/var/srv/titan'})
        self.engine.storage_locations = Mock()
        self.engine.storage_locations.resolve.return_value = {'id':'system','path':'/var/srv/titan'}
        self.row['Config']['Env'] = ['TZ=Europe/Berlin', 'APP_PASSWORD=private-password', 'API_TOKEN=private-token']
        self.engine.engine_container = Mock(side_effect=lambda container: copy.deepcopy(self.row))
        self.engine.op_app_devices = Mock(return_value={'devices': []})
        devices = patch('titan.app_devices.devices', return_value=[])
        devices.start(); self.addCleanup(devices.stop)

    def test_public_editor_omits_private_values(self):
        self.configure()
        result = public_settings(self.engine, self.row)
        self.assertEqual(result['environment'], {'TZ': 'Europe/Berlin'})
        self.assertEqual(result['preserved_secret_keys'], ['APP_PASSWORD', 'API_TOKEN'])
        self.assertNotIn('private-', json.dumps(result))
        self.assertEqual(result['ports'], [{'published': 8080, 'target': 80, 'protocol': 'tcp'}])

    def test_connection_credentials_are_preserved_and_hidden(self):
        self.configure()
        self.row['Config']['Env'] += ['DATABASE_URL=postgres://user:private@database/db',
                                   'ENDPOINT=https://user:private@example.invalid',
                                   'LINK=https://example.invalid?token=private', 'SESSION_COOKIE=private']
        result=public_settings(self.engine,self.row)
        self.assertEqual(result['environment'],{'TZ':'Europe/Berlin'})
        for key in ('DATABASE_URL','ENDPOINT','LINK','SESSION_COOKIE'):
            self.assertIn(key,result['preserved_secret_keys'])
        self.assertNotIn('private',json.dumps(result))
        with self.assertRaisesRegex(Error,'Geheime'):
            self.engine.op_docker_container_settings(ID,{'environment':{'ENDPOINT':'https://user:private@example.invalid'}})
        self.engine.engine_docker.assert_not_called()

    def test_validation_precedes_commit_and_rename(self):
        self.configure()
        for change in [{'image': 'foreign'}, {'privileged': True}, {'environment': {'APP_PASSWORD': 'replace'}}, {'memory_mb': 1}, {'ports': [{'published': 5000, 'target': 80, 'protocol': 'tcp'}]}]:
            self.engine.engine_docker.reset_mock()
            with self.assertRaises(Error):
                self.engine.op_docker_container_settings(ID, change)
            self.engine.engine_docker.assert_not_called()
        for change in [{'State': {'Running': True}}, {'Config': {'Labels': {}}}, {'HostConfig': {**self.row['HostConfig'], 'Privileged': True}}]:
            self.engine.engine_container = Mock(return_value={**self.row, **change})
            self.engine.engine_docker.reset_mock()
            with self.assertRaises(Error):
                self.engine.op_docker_container_settings(ID, {'memory_mb': 1024})
            self.engine.engine_docker.assert_not_called()

    def test_changed_state_rejected_before_snapshot(self):
        self.configure()
        fresh = copy.deepcopy(self.row); fresh['HostConfig']['Memory'] *= 2
        self.engine.engine_container.side_effect = [self.row, fresh]
        with self.assertRaisesRegex(Error, 'verändert'):
            self.engine.op_docker_container_settings(ID, {'memory_mb': 1024})
        self.engine.engine_docker.assert_not_called()

    def test_state_changed_during_commit_does_not_rename(self):
        self.configure()
        fresh=copy.deepcopy(self.row);fresh['State']['Running']=True
        self.engine.engine_container.side_effect=[self.row,self.row,fresh]
        self.engine.engine_docker.return_value='sha256:'+'c'*64
        with self.assertRaisesRegex(Error,'während der Sicherung'):
            self.engine.op_docker_container_settings(ID,{'memory_mb':768})
        self.assertFalse(any(call.args[0][0]=='rename' for call in self.engine.engine_docker.call_args_list))

    def test_no_mount_cannot_implicitly_add_default_storage(self):
        self.configure();self.row['Mounts']=[]
        with self.assertRaisesRegex(Error,'Datenordner'):
            self.engine.op_docker_container_settings(ID,{'memory_mb':768})
        self.engine.engine_docker.assert_not_called()

    def test_snapshot_keeps_secret_defaults_and_current_data(self):
        self.configure()
        self.engine.engine_docker.side_effect = lambda args, **kwargs: 'sha256:' + 'c' * 64 if args[0] == 'commit' else ''
        self.engine.op_docker_container_create = Mock(return_value={'ok': True, 'container': 'b' * 64})
        result = self.engine.op_docker_container_settings(ID, {'memory_mb': 768, 'environment': {'TZ': 'UTC'}})
        config = self.engine.op_docker_container_create.call_args.args[0]
        self.assertEqual(config['image'], 'sha256:' + 'c' * 64)
        self.assertEqual(config['environment'], {'TZ': 'UTC'})
        self.assertEqual(config['memory_mb'], 768)
        self.assertEqual(config['storage_id'],'system')
        self.assertEqual(config['target'],'/data')
        self.assertEqual(result['backup_container'], ID)
        self.assertNotIn('private-', json.dumps(result))
        calls = [call.args[0] for call in self.engine.engine_docker.call_args_list]
        self.assertIn(['rename', ID, 'titan-previous-' + ID[:20]], calls)
        self.assertFalse(any(call[0] == 'rm' for call in calls))

    def test_failure_before_replacement_restores_original_name(self):
        self.configure()
        self.engine.op_docker_container_create = Mock(side_effect=Error('create failed'))
        self.engine.engine_docker.side_effect = lambda args, **kwargs: 'sha256:' + 'c' * 64 if args[0] == 'commit' else ''
        with self.assertRaisesRegex(Error, 'wiederhergestellt'):
            self.engine.op_docker_container_settings(ID, {'memory_mb': 768})
        self.assertEqual(self.engine.engine_docker.call_args.args[0], ['rename', ID, 'titan-custom-web'])

    def test_unexpected_recreation_failure_also_restores_name(self):
        self.configure()
        self.engine.op_docker_container_create=Mock(side_effect=RuntimeError('private detail'))
        self.engine.engine_docker.side_effect=lambda args,**kwargs:'sha256:'+'c'*64 if args[0]=='commit' else ''
        with self.assertRaisesRegex(Error,'wiederhergestellt') as result:
            self.engine.op_docker_container_settings(ID,{'memory_mb':768})
        self.assertNotIn('private detail',str(result.exception))
        self.assertEqual(self.engine.engine_docker.call_args.args[0],['rename',ID,'titan-custom-web'])

    def test_failed_start_removes_only_own_replacement_without_volumes(self):
        self.configure()
        replacement = copy.deepcopy(self.row); replacement['Id'] = 'b' * 64
        self.engine.engine_container.side_effect = lambda container: copy.deepcopy(self.row if container == ID else replacement)
        self.engine.op_docker_container_create = Mock(side_effect=Error('start failed'))
        def docker(args, **kwargs):
            if args[0] == 'commit': return 'sha256:' + 'c' * 64
            if args[0] == 'ps': return 'b' * 64
            return ''
        self.engine.engine_docker.side_effect = docker
        with self.assertRaisesRegex(Error, 'wiederhergestellt'):
            self.engine.op_docker_container_settings(ID, {'memory_mb': 768})
        calls = [call.args[0] for call in self.engine.engine_docker.call_args_list]
        self.assertIn(['rm', 'b' * 64], calls)
        self.assertIn(['rename', ID, 'titan-custom-web'], calls)
        self.assertFalse(any('-v' in call or '--volumes' in call or '--force' in call for call in calls))

    def test_foreign_name_blocks_automatic_removal_and_retains_backup(self):
        self.configure()
        replacement = copy.deepcopy(self.row); replacement['Id'] = 'b' * 64; replacement['Name'] = '/other-container'
        self.engine.engine_container.side_effect = lambda container: copy.deepcopy(self.row if container == ID else replacement)
        self.engine.op_docker_container_create = Mock(side_effect=Error('start failed'))
        self.engine.engine_docker.side_effect = lambda args, **kwargs: 'sha256:' + 'c' * 64 if args[0] == 'commit' else 'b' * 64 if args[0] == 'ps' else ''
        with self.assertRaisesRegex(Error, 'Sicherung erhalten'):
            self.engine.op_docker_container_settings(ID, {'memory_mb': 768})
        self.assertFalse(any(call.args[0][0] == 'rm' for call in self.engine.engine_docker.call_args_list))
