import assert from 'node:assert/strict';
import { appURL, publicURL, tailnetHost } from '../src/lib/app-links.ts';
import { readService, writeService } from '../src/lib/compose/service.ts';
import { readTemplate, build } from '../src/lib/compose/template.ts';
import { parseDocument } from 'yaml';
assert.equal(appURL('https://[host]:3001/path?q=1', 'nas.tail.ts.net', true), 'https://nas.tail.ts.net:3001/path?q=1');
assert.equal(appURL('8081/admin/', 'fd7a:115c:a1e0::1', true), 'http://[fd7a:115c:a1e0::1]:8081/admin/');
assert.equal(appURL('https://vault.example.org', 'nas.tail.ts.net', true), '');
assert.equal(appURL('8080', '', true), '');
for (const value of ['javascript:alert(1)','//evil.example','https://user:pass@example.org','https://example.org/\nfoo']) assert.equal(publicURL(value), '');
assert.equal(tailnetHost({active:true,state:'Running',name:'nas.tail.ts.net.',ips:['100.64.1.2']}), 'nas.tail.ts.net');
assert.equal(tailnetHost({active:true,state:'Stopped',name:'nas.tail.ts.net',ips:[]}), '');
const compose = `x-titanos:
  main: web
  icon: https://example.org/icon.png
  ports:
    '8080': {webui: true}
services:
  web:
    image: example:v1
    ports: ['8090:8080']
    labels:
      keep: yes
      titanos.webui: https://[host]:8090/edit
`;
const doc=parseDocument(compose), form=readService(doc,'web');
form.publicUrl='https://cloud.example.org/app/';writeService(doc,'web',form);
assert.equal(readService(doc,'web').publicUrl, form.publicUrl);
assert.equal(doc.getIn(['services','web','labels','keep']), 'yes');
form.publicUrl='';writeService(doc,'web',form);
assert.equal(doc.getIn(['services','web','labels','titanos.public-url']),undefined);
const template={compose,appdata:'/mnt/data/appdata',timezone:'Europe/Berlin',uid:1000,gid:1000,listening:[]};
const install=readTemplate(template,'example',[],new Map());
install.publicUrl='https://cloud.example.org';install.ports[0].host='18090';
const result=parseDocument(build(template,install).compose);
assert.equal(result.getIn(['services','web','labels','titanos.webui']), 'https://[host]:18090/edit');
assert.equal(result.getIn(['services','web','labels','titanos.public-url']),install.publicUrl);
assert.equal(result.getIn(['services','web','labels','titanos.icon']),'https://example.org/icon.png');
