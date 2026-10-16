import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { parse } from 'yaml';
import { readTemplate, build } from '../src/lib/compose/template.ts';
const template=(app)=>({compose:readFileSync(new URL(`../../backend/internal/catalog/builtin/${app}/compose.yaml`,import.meta.url),'utf8'),timezone:'Europe/Berlin',uid:1000,gid:100,listening:[],appdata:'/mnt/data/appdata',missingDevices:[]});
test('Immich install shares exactly one generated DB password and persists every required data directory',()=>{
 const tpl=template('immich');const form=readTemplate(tpl,'photos',[],new Map([['2283/tcp','existing-app']]));
 assert.equal(form.vars.filter(v=>v.secret).length,1);
 assert.equal(form.vars.some(v=>v.name==='POSTGRES_PASSWORD'),false);
 const built=build(tpl,form);const doc=parse(built.compose);const services=doc.services;
 assert.deepEqual(Object.keys(built.env),['DB_PASSWORD']);assert.match(built.env.DB_PASSWORD,/^[A-Za-z0-9]{32}$/);
 assert.equal(services['immich-server'].environment.DB_PASSWORD,'${DB_PASSWORD}');assert.equal(services.database.environment.POSTGRES_PASSWORD,'${DB_PASSWORD}');
 assert.equal(services['immich-server'].ports[0],'2284:2283');
 assert.equal(services['immich-server'].depends_on.database.condition,'service_healthy');
 assert.equal(services.database.ports,undefined);assert.equal(services.redis.ports,undefined);
 assert.ok(services.database.image.includes('vectorchord'));assert.ok(services['immich-machine-learning'].volumes.includes('./model-cache:/cache'));
 assert.ok(!built.compose.includes(built.env.DB_PASSWORD));
 for(const s of Object.values(services)){assert.equal(s.container_name,undefined);assert.equal(s.privileged,undefined)}
});
test('Ollama with Open WebUI has private API, auth, persistent model/chat storage and CPU default',()=>{
 const tpl=template('ollama');const form=readTemplate(tpl,'chat',[],new Map());const {compose,env}=build(tpl,form);const doc=parse(compose);
 assert.equal(form.main,'open-webui');assert.equal(doc.services['open-webui'].environment.OLLAMA_BASE_URL,'http://ollama:11434');
 assert.equal(doc.services['open-webui'].environment.WEBUI_AUTH,'true');assert.equal(doc.services.ollama.ports,undefined);
 assert.equal(doc.services.ollama.devices,undefined);assert.equal(doc.services.ollama.deploy,undefined);
 assert.ok(env.WEBUI_SECRET_KEY);assert.ok(doc.services.ollama.volumes.includes('./models:/root/.ollama'));assert.ok(doc.services['open-webui'].volumes.includes('./webui:/app/backend/data'));
 assert.notEqual(readTemplate(tpl,'chat-two',[],new Map()).vars.find(v=>v.secret).value,env.WEBUI_SECRET_KEY);
});
