'use strict';
const assert=require('node:assert/strict');
const fields=require('../titan/web/template_fields.js');
const app={port:14333,dynamic_web_port:true,install_schema:[
 {key:'stack_env_0',label:'WEBUI_PORT',type:'text',default:'14333',controlled_by_web_port:true},
 {key:'stack_env_6',label:'BASIC_AUTH_PASS',type:'password',default:''}
],port_bindings:[{target:14333,published:14333,protocol:'tcp'}]};
const html=fields.fields(app);
assert.match(html,/name="option-stack_env_0"[^>]*readonly/);
assert.match(html,/name="option-stack_env_6" type="password"/);
const data=new FormData();data.set('port','18888');data.set('option-stack_env_0','14333');data.set('option-stack_env_6','my-private-password');
assert.equal(fields.argumentsFor(app,data).stack_env_0,'18888');
assert.equal(fields.argumentsFor(app,data).stack_env_6,'my-private-password');
data.set('network_mode','host');assert(fields.mappings(app,data).includes('NAS-Port 18888'));
data.set('network_mode','bridge');assert(fields.mappings(app,data).includes('18888 → 18888/tcp'));
console.log('Cloudflared web UI: password masking, one port control and accurate host/bridge preview passed.');
