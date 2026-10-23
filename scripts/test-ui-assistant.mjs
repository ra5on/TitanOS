// Isolated browser fixtures. PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5187 node scripts/test-ui-assistant.mjs
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const browser=await chromium.launch({headless:true,executablePath:process.env.BROWSER_EXECUTABLE});
const base=process.env.UI_URL||'http://127.0.0.1:5187';
const out=process.env.UI_OUTPUT||'out/validation/assistant';await mkdir(out,{recursive:true});
const errors=[];
const stamp='2026-10-22T10:00:00Z';
let settings={enabled:true,execution:'confirm',ollama:'http://127.0.0.1:11434',model:'test:8b',users:false,web:false,webUsers:false,volume:'',prompt:'',numCtx:8192,temperature:null,maxSteps:0,areas:{},systemPrompt:'Test system prompt',promptHistory:[],profiles:[],memory:'',retentionDays:0,ephemeral:false};
const chat={id:'0123456789abcdef',title:'Prüfung',created:stamp,updated:stamp,model:'test:8b',messages:[{role:'user',content:'Prüfe Docker',time:stamp},{role:'tool',tool:'docker_list',content:'{"updateState":"failed","error":"registry unavailable"}',ok:false,time:stamp}],run:{state:'running',started:stamp}};
let busy=true,controls=[];let automations=[];let knowledge=[];let schedule;
const ctx=await browser.newContext({locale:'de-DE',viewport:{width:1440,height:1000}});
await ctx.addInitScript(()=>localStorage.setItem('titanos.locale','de'));
await ctx.route('**/api/v1/**',async route=>{
 const path=new URL(route.request().url()).pathname.slice(7),method=route.request().method();const json=(data,status=200)=>route.fulfill({status,json:data});
 if(path==='/session')return json({configAvailable:true,setupRequired:false,user:{name:'tester',role:'owner',twoFactor:false},hostname:'test-nas',version:'0.12.0'});
 if(path==='/preferences')return json({appearance:null,dashboard:null,docker:null,vnc:null,sound:null,assistant:null});
 if(path==='/system')return json({hostname:'test-nas',version:'0.12.0',kernel:'test',uptimeSeconds:1000,cpu:{model:'Test',threads:8,usagePercent:0},memory:{totalBytes:16e9,usedBytes:3e9,freeBytes:13e9,availableBytes:13e9},temperatures:[],accelerators:[],image:{slot:'a'},stick:{present:true,label:'TITANOS'},networks:[],notificationsLatest:0});
 if(path==='/storage')return json({volumes:[],disks:[],diskSleep:{idleMinutes:0,available:false,errors:[]}});
 if(path==='/notifications')return json({messages:[]});
 if(path==='/files/roots')return json([{id:'share/Docs',name:'Docs',kind:'share'}]);
 if(path==='/assistant/knowledge'){if(method==='POST'){const entry=route.request().postDataJSON();entry.id||='knowledge1';entry.revision=(entry.revision??0)+1;knowledge=[entry];return json(entry)}return json({entries:knowledge})}
 if(path==='/assistant/knowledge/delete'){knowledge=[];return route.fulfill({status:204})}
 if(path===`/assistant/chats/${chat.id}/schedule`){schedule=route.request().postDataJSON();Object.assign(chat,schedule);return route.fulfill({status:204})}
 if(path==='/assistant/automations/preview')return json({conditionsMet:true,next:stamp});
 if(path==='/assistant/automations') {if(method==='POST'){const a=route.request().postDataJSON();a.id ||= 'fedcba9876543210';a.revision=(a.revision??0)+1;a.runs??=[];automations=automations.filter(v=>v.id!==a.id);automations.push(a);return json(a);}return json({automations});}
 if(path==='/assistant/automations/fedcba9876543210'&&method==='DELETE'){automations=[];return route.fulfill({status:204});}
 if(path==='/assistant/automations/fedcba9876543210/run')return route.fulfill({status:202});
 if(path==='/assistant'){if(method==='PUT')settings=route.request().postDataJSON();return json(settings)}
 if(path==='/assistant/status')return json({available:true,enabled:true,model:'test:8b',web:false,persistent:true});
 if(path==='/assistant/models')return json({models:[{name:'test:8b',sizeBytes:5e9,capabilities:['tools'],contextLength:32768}]});
 if(path==='/assistant/ollama-apps')return json({apps:[]});
 if(path==='/assistant/prompt')return json({defaultPrompt:'Default NAS system prompt',version:'test-template',context:'Saved test context',nasVersion:'0.12.0',historyBytes:2048,persistent:true});
 if(path==='/assistant/probe')return json({toolCalled:true,answer:'Update status unknown because the registry failed.',note:'synthetic'});
 if(path==='/assistant/chats')return json({chats:[{...chat,messages:2}],persistent:true});
 if(path===`/assistant/chats/${chat.id}`)return json({chat,busy});
 if(path===`/assistant/chats/${chat.id}/control`){const action=route.request().postDataJSON().action;controls.push(action);busy=false;chat.run.state=action==='stop'?'stopped':'paused';return route.fulfill({status:204})}
 return json({});
});
const page=await ctx.newPage();page.on('pageerror',e=>errors.push(String(e)));
try{
 await page.goto(base+'/settings/assistant');
 await page.getByLabel('Wie soll der Agent arbeiten?').selectOption('direct');
 await page.getByRole('button',{name:'Direktmodus auswählen',exact:true}).click();
 await page.getByLabel('Systemprompt',{exact:true}).fill('Edited system prompt');
 await page.getByRole('button',{name:'Speichern',exact:true}).click();
 await page.waitForFunction(()=>document.body.innerText.includes('Gespeichert'));
 assert.equal(settings.execution,'direct');assert.equal(settings.systemPrompt,'Edited system prompt');assert.equal(settings.maxSteps,0);
 await page.getByRole('button',{name:'Test starten',exact:true}).click();
 await page.getByText('Update status unknown because the registry failed.',{exact:false}).waitFor();
 await page.screenshot({path:out+'/settings-desktop.png',fullPage:true});
 await page.goto(base+'/assistant');await page.getByRole('button',{name:'Pausieren',exact:true}).click();
 await page.getByRole('button',{name:'Fortsetzen',exact:true}).waitFor();assert.deepEqual(controls,['pause']);
 assert.equal(await page.getByLabel('Probelauf ohne Änderungen').isDisabled(),true);
 await page.screenshot({path:out+'/chat-desktop.png',fullPage:true});
 await page.setViewportSize({width:390,height:844});
 await page.screenshot({path:out+'/chat-mobile.png',fullPage:true});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),'mobile horizontal overflow');
 await page.setViewportSize({width:1440,height:1000});
 await page.getByRole('button',{name:'Neuer Chat',exact:true}).click();
 await page.getByLabel('Probelauf ohne Änderungen').check();
 await page.setViewportSize({width:390,height:844});
 await page.goto(base+'/settings/assistant');await page.getByLabel('Systemprompt',{exact:true}).waitFor();
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),'settings mobile overflow');
 await page.screenshot({path:out+'/settings-mobile.png',fullPage:true});
 await page.goto(base+'/assistant/automations');
 await page.getByRole('button',{name:'Automation erstellen',exact:true}).click();
 await page.getByLabel('Name',{exact:true}).fill('Wochenbericht');
 await page.getByLabel('Auftrag und Zielorte').fill('Erstelle eine Übersicht in share/Docs.');
 await page.getByLabel('Auslöser').selectOption('weekly');
 await page.getByLabel('Wochentag').selectOption('1');
 await page.getByRole('combobox',{name:/^Ausführung/}).selectOption('direct');
 await page.getByRole('button',{name:'Speichern',exact:true}).click();
 await page.getByRole('heading',{name:'Wochenbericht'}).waitFor();
 assert.equal(automations[0].trigger.weekday,1);assert.equal(automations[0].execution,'direct');
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),'automation mobile overflow');
 await page.screenshot({path:out+'/automations-mobile.png',fullPage:true});
 await page.setViewportSize({width:1440,height:1000});
 await page.getByRole('button',{name:'Bearbeiten',exact:true}).click();
 await page.screenshot({path:out+'/automation-edit-desktop.png',fullPage:true});
 await page.getByRole('button',{name:'Abbrechen',exact:true}).click();
 await page.getByRole('button',{name:'Zeitplan pausieren',exact:true}).click();
 await page.getByRole('button',{name:'Aktivieren',exact:true}).waitFor();assert.equal(automations[0].enabled,false);
 await page.getByRole('button',{name:'Jetzt starten',exact:true}).click();
 await page.getByRole('button',{name:'Löschen',exact:true}).click();
 await page.getByRole('dialog').getByRole('button',{name:'Löschen',exact:true}).click();
 await page.getByText('Noch keine Automationen',{exact:true}).waitFor();
 await page.goto(base+'/assistant/knowledge');
 await page.getByRole('button',{name:'Eintrag hinzufügen',exact:true}).click();
 await page.getByLabel('Titel',{exact:true}).fill('Berichtsformat');
 await page.getByLabel('Inhalt',{exact:true}).fill('Bitte Berichte auf Deutsch schreiben.');
 await page.getByLabel('Herkunft / Begründung',{exact:true}).fill('Explizite Testpräferenz');
 await page.getByRole('button',{name:'Speichern',exact:true}).click();
 await page.getByRole('heading',{name:'Berichtsformat'}).waitFor();assert.equal(knowledge[0].kind,'fact');
 await page.screenshot({path:out+'/knowledge-desktop.png',fullPage:true});
 await page.setViewportSize({width:390,height:844});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),'knowledge mobile overflow');
 await page.screenshot({path:out+'/knowledge-mobile.png',fullPage:true});
 await page.getByRole('button',{name:'Vergessen',exact:true}).click();
 await page.getByRole('dialog').getByRole('button',{name:'Vergessen',exact:true}).click();
 await page.getByText('Noch kein Wissen gespeichert',{exact:true}).waitFor();
 await page.goto(base+'/assistant/tasks');
 await page.getByRole('button',{name:'Reihenfolge und Voraussetzungen',exact:true}).click();
 await page.getByRole('dialog').getByRole('combobox',{name:/^Priorität/}).selectOption('1');
 await page.getByRole('dialog').getByRole('button',{name:'Speichern',exact:true}).click();
 await page.getByText('Priorität: Hoch',{exact:true}).waitFor();assert.equal(schedule.priority,1);
 await page.screenshot({path:out+'/tasks-mobile.png',fullPage:true});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2),'tasks mobile overflow');
 assert.deepEqual(errors,[]);console.log('Assistant UI checks passed: settings, direct mode, prompt, probe, pause, dry run, mobile.');
}finally{await browser.close()}
