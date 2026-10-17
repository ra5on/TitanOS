// Render through the same install-form code as the UI, including generated secrets.
// Runs in the TitanOS-Apps repository; TITANOS_DIR points at a TitanOS checkout.
import { readFileSync } from 'node:fs';
const titanos=process.env.TITANOS_DIR;
if(!titanos)throw Error('TITANOS_DIR is not set');
const { readTemplate, build } = await import(`${titanos}/web/src/lib/compose/template.ts`);
const app=process.argv[2];
if(!['immich','ollama'].includes(app))throw Error('unknown test app');
const tpl={compose:readFileSync(new URL(`../${app}/compose.yaml`,import.meta.url),'utf8'),timezone:'UTC',uid:1000,gid:100,appdata:'/mnt/testdata/appdata',listening:[],missingDevices:[]};
process.stdout.write(JSON.stringify(build(tpl,readTemplate(tpl,app,[],new Map()))));
