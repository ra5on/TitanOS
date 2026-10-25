// Isolated API fixtures: no real NAS, host disks or user files are accessed.
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { fixtureFactory } from './test-ui-fixtures.mjs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless:true, executablePath:process.env.BROWSER_EXECUTABLE });
const base=process.env.UI_URL || 'http://127.0.0.1:5196';
const out=process.env.UI_OUTPUT || 'out/validation/ui-drive-transfers';
await mkdir(out,{recursive:true});
const errors=[], create=fixtureFactory(browser,errors), screenshots=[];
const makeJob=(id,state='done')=>({id,state,move:false,durable:true,blocked:false,from:'usb/Fotos-Beispiel',to:'share/Documents',dir:'Import',names:['Fotos'],files:85177,done:state==='done'?85177:24000,bytes:318e9,copied:state==='done'?318e9:90e9,current:state==='running'?'Fotos/Urlaub/IMG_24001.jpg':'',error:'',paused:false,priority:0,rate:0});
try {
 for (const [width,theme,locale] of [[1440,'graphit','de'],[390,'graphit','de'],[1440,'light','en']]) {
  const {ctx,page,data}=await create('owner',theme,width,locale);
  data.system.version='0.21.0-dev';
  let jobs=[makeJob('active','running'),makeJob('done1'),makeJob('done2')],fail=false,holdPoll=false,releasePoll=null;
  const deleted=[];
  await ctx.route('**/api/v1/files/jobs',async route=>{
   for(const job of jobs)if(job.state==='running')job.copied+=50e6;
   const snapshot=structuredClone(jobs);
   if(holdPoll)await new Promise(resolve=>releasePoll=resolve);
   await route.fulfill({json:snapshot});
  });
  await ctx.route('**/api/v1/files/transfers**',async route=>{
   assert.equal(route.request().method(),'DELETE');
   assert.equal(route.request().postData(),null,'removal must not request or send an ID confirmation');
   if(fail)return route.fulfill({status:500,json:{error:{code:'config_write_failed',message:'fixture failure'}}});
   const id=new URL(route.request().url()).pathname.split('/').at(-1);
   deleted.push(id);
   jobs=jobs.filter(job=>id==='transfers'?job.state==='running':job.id!==id);
   await route.fulfill({status:204});
  });
  await ctx.route('**/api/v1/usb',route=>route.fulfill({json:[
   {id:'sata-data',name:'Import HDD',transport:'sata',fsType:'ntfs',supported:true,mounted:false},
   {id:'nvme-data',name:'Import SSD',transport:'nvme',fsType:'ext4',supported:true,mounted:false},
   {id:'busy',name:'Belegtes Laufwerk',transport:'sas',fsType:'xfs',supported:true,mounted:false,busy:true}
  ]}));
  await page.goto(base+'/files?root=share%2FDocuments');
  // The jobs section has an accessible label; query its class to avoid coupling to a translation.
  const section=page.locator('section.jobs');
  await section.waitFor().catch(async error=>{
   console.error({width,theme,locale,errors,body:await page.locator('body').innerText()});
   await page.screenshot({path:out+'/failure.png',fullPage:true});
   throw error;
  });
  await page.waitForFunction(()=>document.querySelectorAll('section.jobs .job').length===3);
  if(width===390) await page.locator('.places-toggle').click();
  await page.getByText('Import HDD',{exact:false}).waitFor();
  assert.equal(await page.getByRole('dialog').count(),0);
  const removeName=locale==='de'?'Auftragseintrag entfernen':'Remove task record';
  const clearName=locale==='de'?'Alle beendeten Aufträge entfernen':'Remove all finished jobs';
  const single=section.getByRole('button',{name:removeName,exact:true});
  assert.equal(await single.count(),2);
  assert.equal(await section.getByRole('button',{name:clearName,exact:true}).count(),1);
  if(locale==='de') {
   await section.getByText(/Noch ungefähr \d+ Min\./).waitFor();
   await page.screenshot({path:`${out}/dateien-auftraege-${width}.png`,fullPage:true});
   screenshots.push(`dateien-auftraege-${width}.png`);
  }
  holdPoll=true;
  for(let i=0;i<100&&!releasePoll;i++)await new Promise(resolve=>setTimeout(resolve,25));
  assert.ok(releasePoll,'no pending poll to test stale responses');
  await single.first().click();
  holdPoll=false;releasePoll();
  await page.waitForFunction(()=>document.querySelectorAll('section.jobs .job').length===2);
  assert.deepEqual(deleted,['done1']);
  assert.equal(await page.getByRole('dialog').count(),0);
  // Include more than the former five visible finished jobs.
  jobs=[makeJob('active','running'),...Array.from({length:8},(_,i)=>makeJob('older'+i))];
  await page.waitForFunction(()=>document.querySelectorAll('section.jobs .job').length===9);
  fail=true;
  await section.getByRole('button',{name:clearName,exact:true}).click();
  await section.getByRole('alert').waitFor();
  assert.equal(jobs.length,9,'failed removal discarded jobs');
  fail=false;
  await section.getByRole('button',{name:clearName,exact:true}).click();
  await page.waitForFunction(()=>document.querySelectorAll('section.jobs .job').length===1);
  assert.equal(jobs[0].id,'active');
  await page.reload();
  await page.waitForFunction(()=>document.querySelectorAll('section.jobs .job').length===1);
  assert.equal(await section.getByRole('button',{name:clearName,exact:true}).count(),0);
  assert.equal(await section.getByRole('button',{name:removeName,exact:true}).count(),0);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'horizontal overflow');
  await ctx.close();
 }
 assert.deepEqual(errors,[]);
 const hash=async path=>createHash('sha256').update(await readFile(path)).digest('hex');
 const sources=['web/src/routes/files/JobsPanel.svelte','web/src/routes/files/Places.svelte','web/src/lib/i18n/de.json'];
 await writeFile(out+'/capture.json',JSON.stringify({date:'2026-10-25',source:'built development WebUI after 0.21.0',fixtures:'isolated API data only',sources:Object.fromEntries(await Promise.all(sources.map(async p=>[p,await hash(p)]))),screenshots:Object.fromEntries(await Promise.all(screenshots.map(async p=>[p,await hash(out+'/'+p)])))},null,2)+'\n');
 console.log('Single-click removal, all eight completed jobs, active job retained, reload, failure/retry, SATA/NVMe display, desktop/mobile and English passed');
} finally { await browser.close(); }
