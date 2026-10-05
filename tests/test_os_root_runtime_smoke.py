"""Reject unsafe mount layouts; fixture tests do not replace the booted-guest gate."""
import base64
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import unittest
from unittest.mock import Mock, patch

spec=importlib.util.spec_from_file_location('os_root_runtime_smoke',Path(__file__).resolve().parents[1]/'scripts/smoke-runtime.py')
smoke=importlib.util.module_from_spec(spec);spec.loader.exec_module(smoke)

BOOT_ID='11111111-2222-3333-4444-555555555555'
STATE_PATHS=('/var/lib/titan','/var/lib/titan-agent','/var/lib/titan-proxy','/var/lib/docker',
    '/var/lib/containerd','/var/lib/libvirt','/var/lib/samba','/var/lib/systemd',
    '/var/lib/private','/var/lib/dbus','/var/lib/wtmpdb',
    '/var/cache','/var/log','/var/tmp','/var/spool','/var/srv/titan','/home')


def layout():
    rows=[{'path':'/','root':'/','device':'8:3','type':'ext4','options':'ro,relatime','super':'ro'},
          {'path':'/var/lib/titan-system','root':'/','device':'8:5','type':'ext4','options':'rw,relatime','super':'rw'},
          {'path':'/etc','root':'/','device':'0:63','type':'overlay','options':'rw,relatime',
           'super':'rw,lowerdir=/root/etc,upperdir=/root/var/lib/titan-system/persistent/etc,workdir=/root/var/lib/titan-system/etc-work'},
          {'path':'/tmp','root':'/','device':'0:64','type':'tmpfs','options':'rw,nosuid,nodev,relatime','super':'rw,size=2097152k'}]
    rows += [{'path':path,'root':'/persistent'+path,'device':'8:5','type':'ext4','options':'rw,relatime','super':'rw'} for path in STATE_PATHS]
    return rows


def mountinfo(rows):
    return '\n'.join(f"{index+20} 1 {row['device']} {row['root']} {row['path']} {row['options']} - {row['type']} /dev/vda {row['super']}"
                     for index,row in enumerate(rows))+'\n'


def guest_probe(rows=None, text=None):
    content=mountinfo(layout() if rows is None else rows) if text is None else text
    def read(path,*args,**kwargs):
        if str(path)=='/proc/1/mountinfo':return content
        if str(path)=='/proc/sys/kernel/random/boot_id':return BOOT_ID+'\n'
        raise AssertionError('Probe read an unexpected path')
    output=io.StringIO()
    with patch.object(Path,'read_text',autospec=True,side_effect=read) as reads,contextlib.redirect_stdout(output):
        exec(smoke.GUEST_OS_ROOT_PROTECTION,{})
    assert str(reads.call_args_list[0].args[0])=='/proc/1/mountinfo'
    return json.loads(output.getvalue().removeprefix('TITAN_OS_ROOT_PROTECTION:'))


class OsRootGuestProbeTests(unittest.TestCase):
    def test_actual_pid1_mount_format_accepts_expected_ab_layout(self):
        value=guest_probe()
        self.assertEqual(set(value),smoke.OS_ROOT_PROTECTION_FIELDS|{'format','boot_id'})
        self.assertTrue(all(value[field] is True for field in smoke.OS_ROOT_PROTECTION_FIELDS))
        self.assertEqual(value['boot_id'],BOOT_ID)
        self.assertNotIn('/var/',json.dumps(value))

    def test_per_mount_readonly_cannot_hide_writable_superblock(self):
        for options,super_options in (('rw','rw'),('ro','rw'),('rw','ro')):
            rows=layout();rows[0].update(options=options,super=super_options)
            with self.subTest(options=options,super=super_options):
                value=guest_probe(rows)
                self.assertFalse(value['root_readonly'])
                self.assertFalse(value['root_has_no_writable_alias'])

    def test_writable_alias_of_root_slot_is_rejected(self):
        rows=layout();rows.append({'path':'/mnt/slot','root':'/','device':'8:3','type':'ext4','options':'rw','super':'ro'})
        value=guest_probe(rows)
        self.assertTrue(value['root_readonly'])
        self.assertFalse(value['root_has_no_writable_alias'])

    def test_package_state_and_usr_must_remain_in_readonly_slot(self):
        for path,field in (('/var/lib/dpkg','dpkg_slot_readonly'),('/var/lib/apt','apt_slot_readonly'),('/usr','usr_slot_readonly')):
            rows=layout();rows.append({'path':path,'root':'/persistent'+path,'device':'8:5','type':'ext4','options':'rw','super':'rw'})
            with self.subTest(path=path):self.assertFalse(guest_probe(rows)[field])

    def test_data_must_be_separate_writable_filesystem(self):
        for change in ({'device':'8:3'},{'options':'ro'},{'super':'ro'},{'root':'/persistent'},{'type':'tmpfs'}):
            rows=layout();rows[1].update(change)
            with self.subTest(change=change):
                self.assertFalse(guest_probe(rows)['data_partition_writable'])

    def test_etc_requires_writable_overlay_upper_inside_persistent_data(self):
        for change in ({'options':'ro'},{'super':'ro'}, {'type':'ext4'},
                       {'super':'rw,upperdir=/etc'},{'super':'rw,upperdir=/var/lib/titan-system/persistent/etc-new'}):
            rows=layout();rows[2].update(change)
            with self.subTest(change=change):self.assertFalse(guest_probe(rows)['etc_overlay_on_data_writable'])
        rows=layout();rows[2]['super']=rows[2]['super'].replace('/root/var/lib/titan-system/','/var/lib/titan-system/')
        self.assertTrue(guest_probe(rows)['etc_overlay_on_data_writable'])

    def test_every_service_state_mount_requires_correct_data_source(self):
        for path in STATE_PATHS:
            for change in ({'options':'ro'},{'super':'ro'},{'device':'8:6'},{'root':'/other-state'},{'type':'tmpfs'}):
                rows=layout();next(row for row in rows if row['path']==path).update(change)
                with self.subTest(path=path,change=change):
                    self.assertFalse(guest_probe(rows)['persistent_state_mounts_writable'])
            rows=[row for row in layout() if row['path']!=path]
            with self.subTest(missing=path):self.assertFalse(guest_probe(rows)['persistent_state_mounts_writable'])

    def test_tmp_requires_writable_restricted_tmpfs(self):
        for change in ({'type':'ext4'},{'options':'ro,nosuid,nodev'},{'super':'ro'},{'options':'rw,nosuid'},{'root':'/persistent/tmp'}):
            rows=layout();rows[3].update(change)
            with self.subTest(change=change):self.assertFalse(guest_probe(rows)['tmpfs_writable'])

    def test_duplicate_mounts_fail_closed(self):
        for index,field in ((0,'root_readonly'),(1,'data_partition_writable'),(2,'etc_overlay_on_data_writable'),
                            (3,'tmpfs_writable'),(4,'persistent_state_mounts_writable')):
            rows=layout();rows.append(rows[index].copy())
            with self.subTest(index=index):self.assertFalse(guest_probe(rows)[field])

    def test_malformed_and_oversized_mountinfo_does_not_produce_proof(self):
        for text in ('','invalid mount information\n','x'*(1024*1024+1),mountinfo(layout())*240):
            with self.subTest(length=len(text)),self.assertRaises(RuntimeError):guest_probe(text=text)


class OsRootRuntimeEvidenceTests(unittest.TestCase):
    def test_runtime_returns_only_fixed_boolean_evidence(self):
        runner=smoke.RuntimeSmoke(Mock(os_root_protection=Mock(return_value=guest_probe())))
        value=runner.os_root_protection()
        self.assertEqual(value,{field:True for field in smoke.OS_ROOT_PROTECTION_FIELDS})
        self.assertNotIn(BOOT_ID,json.dumps(value))

    def test_nonboolean_incomplete_or_unknown_guest_proof_fails(self):
        original=guest_probe()
        variants=[{**original,field:value} for field in smoke.OS_ROOT_PROTECTION_FIELDS for value in (False,1,'true',None)]
        variants += [None,{}, {**original,'paths':'private host path'},{**original,'boot_id':'stale'},
                     {**original,'format':'unexpected'}]
        for value in variants:
            runner=smoke.RuntimeSmoke(Mock(os_root_protection=Mock(return_value=value)))
            with self.subTest(value=value),self.assertRaises(smoke.SmokeFailure):runner.os_root_protection()

    def transport(self,body,status=200):
        client=smoke.GuestClient();client.cookie='titan_session=runtime-secret'
        client.request=Mock(side_effect=lambda path,request=None:{'id':'a'*64} if request and request.get('action')=='create' else {'ok':True})
        connection=Mock();response=connection.getresponse.return_value;response.status=status
        encoded=base64.b64encode(body).decode()
        response.readline.side_effect=[b'data: '+json.dumps({'data':encoded}).encode()+b'\n',b'']
        return client,connection

    def test_guest_transport_uses_only_fixed_authenticated_loopback_and_closes_pty(self):
        value=guest_probe()
        body=b'\r\nTITAN_OS_ROOT_PROTECTION:'+json.dumps(value).encode()+b'\r\n'
        client,connection=self.transport(body)
        with patch.object(smoke.http.client,'HTTPSConnection',return_value=connection) as https:
            self.assertEqual(client.os_root_protection(),value)
        https.assert_called_once_with('127.0.0.1',15000,timeout=25,context=client.context)
        connection.request.assert_called_once_with('GET','/api/terminal/output?id='+'a'*64,
            headers={'Host':client.HOST,'Origin':client.ORIGIN,'Cookie':client.cookie})
        command=base64.b64decode(client.request.call_args_list[1].args[1]['data']).decode()
        self.assertIn('/proc/1/mountinfo',command)
        self.assertNotIn('/proc/self/mountinfo',command)
        self.assertNotIn('runtime-secret',command)
        # The shell accepts this quoted multiline command in canonical PTY mode;
        # Linux's per-line input bound must not silently truncate a proof line.
        self.assertLess(max(len(line.encode()) for line in command.splitlines()),4096)
        self.assertEqual(client.request.call_args_list[-1].args[1],{'action':'close','id':'a'*64})
        connection.close.assert_called_once_with()

    def test_guest_transport_rejects_error_or_invalid_output_and_always_closes(self):
        for body,status in ((b'private traceback',200),(b'\nTITAN_OS_ROOT_PROTECTION:{bad}\n',200),(b'x'*17000,200),(b'',401)):
            client,connection=self.transport(body,status)
            with self.subTest(status=status,length=len(body)),patch.object(smoke.http.client,'HTTPSConnection',return_value=connection):
                with self.assertRaises(smoke.SmokeFailure) as error:client.os_root_protection()
            self.assertNotIn('private traceback',str(error.exception))
            connection.close.assert_called_once_with()
            self.assertEqual(client.request.call_args_list[-1].args[1]['action'],'close')


if __name__=='__main__':unittest.main()
