// Browser checks against isolated API fixtures. No NAS operations or real disks.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-060.mjs
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.UI_URL || 'http://127.0.0.1:5179';
const out = process.env.UI_OUTPUT || 'out/validation/ui-060';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({headless:true, executablePath:process.env.BROWSER_EXECUTABLE});
const errors = [];
const time = '2026-10-20T09:00:00Z';
const system = { hostname:'titan-test',version:'0.6.0',kernel:'test',uptimeSeconds:1000,cpu:{model:'Test CPU',threads:8,usagePercent:12},memory:{totalBytes:16e9,usedBytes:3e9,freeBytes:13e9,availableBytes:13e9},temperatures:[],accelerators:[],image:{slot:'a',sizeBytes:1e9,overlayUsedBytes:0,overlayTotalBytes:512e6},stick:{present:true,label:'TITANOS'},network:null,networks:[],networkPendingSeconds:0,notificationsLatest:1};
const volume={name:'data',level:'raid1',filesystem:'ext4',mountPoint:'/mnt/data',mounted:true,state:'checking',sizeBytes:1e12,usedBytes:4e11,raidDisks:2,members:[],sync:{percent:32,speedBytesPerSecond:1e8,remainingSeconds:500},mismatches:0};
const vm={name:'linux',displayName:'Linux Server',description:'Test VM',template:'linux',memoryMiB:2048,vcpus:2,volume:'data',disks:[],iso:'',autostart:true,cpuPin:[],exclusive:false,folders:[],tpm:false,secureBoot:false,pci:[],usb:[],network:'user',job:null,state:'stopped',available:true,cpuPercent:4,memoryBytes:1e9,since:time};
const backup={job:{id:'daily',sources:['documents'],destination:{type:'local',volume:'data',folder:'backups'},enabled:true,every:'daily',weekday:0,time:'02:00',keep:{daily:7,weekly:2,monthly:1}},secretSet:{},state:{running:true,phase:'backup',percent:25,bytesDone:25,bytesTotal:100,lastRun:time,lastOK:time,lastError:'',warning:'',bytes:100,restoredTo:''},next:null};
let offline=false, failVM=false, failZone=false;
const requests=[];
async function context(role='owner',theme='graphit',width=1440) {
 const ctx=await browser.newContext({locale:'de-DE',viewport:{width,height:1000}});
 await ctx.addInitScript(({theme})=>{localStorage.setItem('titanos.locale','de');localStorage.setItem('titanos.appearance',JSON.stringify({theme}));},{theme});
 await ctx.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname.slice('/api/v1'.length); const method=route.request().method(); requests.push([role,method,path]);
  const json=async data=>route.fulfill({json:data});
  const fail=async()=>route.fulfill({status:500,json:{error:{code:'internal',message:'fixture failure'}}});
  if(path==='/session')return json({configAvailable:true,setupRequired:false,user:{name:'tester',role,twoFactor:false},hostname:'titan-test'});
  if(path==='/preferences')return json({appearance:{theme},dashboard:null,docker:null,vnc:null,sound:null});
  if(path==='/system'){if(offline)return route.abort();return json(system);}
  if(path==='/storage')return json({volumes:role==='user'?[]:[volume],disks:[],diskSleep:{idleMinutes:0,available:true,errors:[]}});
  if(path==='/notifications')return json({messages:[{id:1,time,severity:'warning',kind:'volume_full',subject:'data',title:'Volume',text:'Full'}]});
  if(path==='/terminal')return json({enabled:true,confirm:false});
  if(path==='/network')return json({timezone:'Europe/Berlin'});
  if(path==='/system/timezone'){await new Promise(r=>setTimeout(r,250));return failZone?fail():json({});}
  if(path==='/files/jobs')return json([{id:'copy1',move:false,state:'running',from:'share/documents',to:'volume/data',dir:'backup',names:['report.pdf'],files:1,done:0,bytes:200,copied:100,current:'report.pdf',error:''}]);
  if(path==='/backups')return json({smbTargets:[],jobs:[backup],shares:[{name:'documents',volume:'data'}],volumes:['data']});
  if(path==='/backups/daily/stop'){await new Promise(r=>setTimeout(r,500));backup.state.running=false;return json({smbTargets:[],jobs:[backup],shares:[],volumes:['data']});}
  if(path==='/backups/target')return json({target:{enabled:false,volume:'',port:0,sources:[],listening:[],problem:''},volumes:['data']});
  if(path==='/usb')return json([]);
  if(path==='/update')return json({progress:{phase:'download',percent:40,file:'filesystem.squashfs'}});
  if(path==='/vms')return json({kvm:true,bridge:true,host:{cpus:8,memoryBytes:16e9,usedMemoryBytes:3e9},vms:[vm]});
  if(path==='/vms/linux/start'){await new Promise(r=>setTimeout(r,700));return failVM?fail():json({});}
  if(path==='/vms/linux/snapshots')return json({snapshots:[],schedule:{enabled:false,time:'03:00',days:[],keep:3,memory:false,allowPause:false},pending:false,reason:'',job:null});
  if(path==='/plugins/tailscale')return json({active:true,state:'Running',name:'test.ts.net',ips:[]});
  if(path==='/docker')return json({settings:{enabled:true,volume:'data'},running:true,version:'26.0',containers:[{id:'media',name:'media',image:'media:latest',state:'running',since:time,ports:[{host:8096,container:8096,protocol:'tcp'}],app:'media',service:'media',managed:true,cpuPercent:4,memoryBytes:512e6,cpuHistory:[2,4,3,4],webui:'8096',publicUrl:'https://media.example.test',icon:'',updateAvailable:false}],apps:['media'],jobs:{},updatesChecked:time});
  return json({});
 });
 const page=await ctx.newPage();await page.clock.install({time:new Date('2026-10-20T12:00:00Z')});page.on('pageerror',e=>errors.push(String(e)));
 return {ctx,page};
}
try {
 if(process.env.DOCKER_ONLY){
  for(const theme of ['graphit','hell'])for(const width of [1440,390]){
   const {ctx,page}=await context('owner',theme,width);await page.goto(base+'/docker');await page.getByText('media:latest',{exact:true}).waitFor();await page.evaluate(async()=>{ await document.fonts.load('400 14px "IBM Plex Sans"'); await document.fonts.load('500 14px "IBM Plex Sans"'); await document.fonts.load('600 14px "IBM Plex Sans"'); await document.fonts.load('400 14px "IBM Plex Mono"'); await document.fonts.ready; });
   await writeFile(out+`/docker-${theme}-${width}.json`,JSON.stringify(await page.evaluate(()=>[...document.querySelectorAll('header, main, main *,header *')].map(e=>{const c=getComputedStyle(e);const b=e.getBoundingClientRect();return [e.tagName,e.textContent.trim().slice(0,40),c.fontFamily,c.fontSize,c.fontWeight,b.x,b.y,b.width,b.height]})),null,2));
   await page.screenshot({path:out+`/docker-${theme}-${width}.png`,fullPage:true,mask:[page.locator('footer')]});
   await ctx.close();
  }
  console.log('Docker comparison screenshots captured.');
 } else {
 const {ctx,page}=await context();
 await page.goto(base+'/settings');
 const search=page.getByRole('searchbox',{name:'Einstellungen suchen'});await search.waitFor();
 await search.fill('SSH');assert.equal(await page.locator('.areas > a').count(),1);assert.match(await page.locator('.areas').innerText(),/Zugang/);
 await search.fill('DNS');assert.match(await page.locator('.areas').innerText(),/Netzwerk/);
 await search.fill('Zeitzone');await page.locator('select').selectOption('UTC');await page.getByText('Gespeichert',{exact:true}).waitFor();
 failZone=true;await page.locator('select').selectOption('Europe/Berlin');await page.locator('[role=alert]').waitFor();assert.equal(await page.locator('select').inputValue(),'UTC');failZone=false;
 await search.fill('unknown-no-match');await page.getByText('Keine passenden Einstellungen gefunden.').waitFor();
 await search.fill('Zeitzone');await page.locator('select').selectOption('Europe/Berlin');await page.getByText('Gespeichert',{exact:true}).waitFor();await search.fill('');await page.screenshot({path:out+'/settings-desktop.png',fullPage:true});
 await page.getByRole('button',{name:/Laufende Aufgaben/}).click();await page.getByRole('dialog').waitFor();
 assert.equal(await page.locator('dialog progress').count(),4);await page.screenshot({path:out+'/tasks-desktop.png'});
 await page.keyboard.press('Escape');
 await page.locator('.notifications > button').click();await page.getByRole('link',{name:'Details öffnen',exact:true}).click();
 await page.locator('#volume-data').waitFor();assert.equal(new URL(page.url()).hash,'#volume-data');
 offline=true;await page.getByText('Verbindung zum NAS unterbrochen',{exact:true}).waitFor({timeout:15000});await page.getByText(/Angezeigte Werte sind möglicherweise veraltet/).waitFor();await page.screenshot({path:out+'/connection-lost.png'});offline=false;
 await page.getByRole('button',{name:'Erneut versuchen',exact:true}).click();await page.getByText('Verbindung zum NAS unterbrochen',{exact:true}).waitFor({state:'hidden'});
 await page.goto(base+'/vms');await page.getByRole('heading',{name:'Linux Server',exact:true}).waitFor();
 failVM=true;await page.getByRole('button',{name:'Starten',exact:true}).click();await page.getByText(/Linux Server: Wird gestartet/).waitFor();await page.getByRole('button',{name:'Erneut versuchen',exact:true}).waitFor();
 failVM=false;await page.getByRole('button',{name:'Erneut versuchen',exact:true}).click();await page.getByText(/Aktion für Linux Server angenommen/).waitFor();
 await page.getByText('Weitere Aktionen',{exact:false}).click();await page.keyboard.press('Escape');assert.equal(await page.locator('.actions-menu').evaluate(n=>n.open),false);
 await page.screenshot({path:out+'/vms-desktop.png',fullPage:true});
 await page.goto(base+'/settings/backups#backup-daily');await page.locator('#backup-daily').waitFor();await page.locator('#backup-daily').getByRole('button',{name:'Abbrechen',exact:true}).click();await page.getByText(/Auftrag für daily angenommen/).waitFor();
 await page.goto(base+'/docker');await page.getByRole('heading',{name:'Docker',exact:true}).waitFor();
 assert.equal(await page.locator('main.readable').count(),0);assert.equal(await page.getByRole('button',{name:/Laufende Aufgaben/}).count(),0);
 await page.screenshot({path:out+'/docker-desktop.png',fullPage:true});
 await ctx.close();
 for(const theme of ['graphit','hell']){
  const {ctx,page}=await context('owner',theme,390);await page.goto(base+'/settings');await page.getByRole('searchbox').waitFor();
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'settings mobile overflow');await page.screenshot({path:out+`/settings-${theme}-mobile.png`,fullPage:true});
  await page.goto(base+'/vms');await page.getByRole('heading',{name:'Linux Server',exact:true}).waitFor();
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'VM mobile overflow');await page.screenshot({path:out+`/vms-${theme}-mobile.png`,fullPage:true});await ctx.close();
 }
 const user=await context('user');await user.page.goto(base+'/settings');await user.page.getByRole('searchbox').waitFor();await user.page.getByRole('searchbox').fill('SSH');assert.equal(await user.page.locator('.areas > a').count(),0);
 await user.page.getByRole('button',{name:/Laufende Aufgaben/}).click();await user.page.getByRole('dialog').waitFor();assert.equal(await user.page.locator('dialog progress').count(),1);
 assert.ok(!requests.some(([role,,path])=>role==='user'&&['/backups','/update','/vms'].includes(path)));await user.ctx.close();
 assert.deepEqual(errors,[]);console.log('UI 0.6.0 browser checks passed (desktop, 390px, Graphit/Hell, roles, errors, tasks, Docker isolation).');
}
} finally { await browser.close(); }
