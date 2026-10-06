import unittest
from titan.core import Error
from test_lifecycle_http import HTTPFixture


class ContainerAdminBoundaryTests(HTTPFixture, unittest.TestCase):
    def allow_apps(self):
        self.app.store.set_config('identity', {'schema':1,'groups':[],
            'users':{'reader':{'applications':{'apps':True},'shares':{}}}})

    def test_delegated_apps_do_not_expose_manual_settings_or_guest_console(self):
        self.allow_apps()
        for operation,arguments in [('docker_container_exec',{'container':'a'*64,'command':'id'}),
                                    ('docker_container_settings',{'container':'a'*64,'changes':{'memory_mb':512}})]:
            self.assertEqual(self.request('/api/actions',{'operation':operation,'arguments':arguments},actor='reader')[0],403)
            with self.assertRaises(Error):self.app.admin_action('reader',operation,arguments)
        self.agent.call.assert_not_called()
        self.agent.call.return_value={'container':{'id':'a'*64},'settings':{'environment':{'EXAMPLE':'private'}},'logs':''}
        status,result,_=self.json_request('/api/docker-container?container='+'a'*64,actor='reader')
        self.assertEqual(status,200);self.assertNotIn('settings',result)
        status,result,_=self.json_request('/api/docker-container?container='+'a'*64)
        self.assertEqual(status,200);self.assertIn('settings',result)

    def test_delegated_template_low_ports_rejected_before_job_and_execution(self):
        self.allow_apps()
        for operation in ('app_install','app_settings'):
            arguments={'app':'example','port':8080}
            self.agent.call.return_value={'ports':[{'host':8080,'target':80,'protocol':'tcp'},
                                                    {'host':53,'target':53,'protocol':'udp'}]}
            self.assertEqual(self.request('/api/actions',{'operation':operation,'arguments':arguments},actor='reader')[0],403)
            with self.assertRaises(Error):self.app.admin_action('reader',operation,arguments)
        self.assertFalse(self.app.store.jobs())
        self.agent.call.assert_called_with('app_requested_ports',app='example',port=8080,options=None,network=None)

    def test_delegated_template_safe_ports_dispatch_after_fresh_check(self):
        self.allow_apps()
        self.agent.call.side_effect=lambda operation,**kw: {'ports':[{'host':8080,'target':80,'protocol':'tcp'}]} if operation=='app_requested_ports' else {'ok':True}
        status,result,_=self.json_request('/api/actions',{'operation':'app_install','arguments':{'app':'example','port':8080}},actor='reader')
        self.assertEqual(status,202);self.assertEqual(self.wait_job(result['job'])['status'],'completed')
        self.assertEqual(sum(call.args[0]=='app_requested_ports' for call in self.agent.call.call_args_list),2)
