// Render through the same install-form code as the UI, including generated secrets.
import { readFileSync } from 'node:fs';
import { readTemplate, build } from '../web/src/lib/compose/template.ts';
const app=process.argv[2];
if(!['immich','ollama'].includes(app))throw Error('unknown test app');
const tpl={compose:readFileSync(new URL(`../backend/internal/catalog/builtin/${app}/compose.yaml`,import.meta.url),'utf8'),timezone:'UTC',uid:1000,gid:100,appdata:'/mnt/testdata/appdata',listening:[],missingDevices:[]};
process.stdout.write(JSON.stringify(build(tpl,readTemplate(tpl,app,[],new Map()))));
