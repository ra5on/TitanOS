// Browser checks against isolated API fixtures. No NAS operations or real disks.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-060.mjs
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.UI_URL || 'http://127.0.0.1:5179';
const out = process.env.UI_OUTPUT || 'out/validation/ui-061';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({headless:true, executablePath:process.env.BROWSER_EXECUTABLE});
const errors = [];
const time = '2026-10-20T09:00:00Z';
const system = { hostname:'titan-test',version:'0.6.0',kernel:'test',uptimeSeconds:1000,cpu:{model:'Test CPU',threads:8,usagePercent:12},memory:{totalBytes:16e9,usedBytes:3e9,freeBytes:13e9,availableBytes:13e9},temperatures:[],accelerators:[],image:{slot:'a',sizeBytes:1e9,overlayUsedBytes:0,overlayTotalBytes:512e6},stick:{present:true,label:'TITANOS'},network:null,networks:[],networkPendingSeconds:0,notificationsLatest:1};
const volume={name:'data',level:'raid1',filesystem:'ext4',mountPoint:'/mnt/data',mounted:true,state:'checking',sizeBytes:1e12,usedBytes:4e11,raidDisks:2,members:[],sync:{percent:32,speedBytesPerSecond:1e8,remainingSeconds:500},mismatches:0};
const vm={name:'linux',displayName:'Linux Server',description:'Test VM',template:'linux',memoryMiB:2048,vcpus:2,volume:'data',disks:[],iso:'',autostart:true,cpuPin:[],exclusive:false,folders:[],tpm:false,secureBoot:false,pci:[],usb:[],network:'user',job:null,state:'stopped',available:true,cpuPercent:4,memoryBytes:1e9,since:time};
const backup={job:{id:'daily',sources:['documents'],destination:{type:'local',volume:'data',folder:'backups'},enabled:true,every:'daily',weekday:0,time:'02:00',keep:{daily:7,weekly:2,monthly:1}},secretSet:{},state:{running:true,phase:'backup',percent:25,bytesDone:25,bytesTotal:100,lastRun:time,lastOK:time,lastError:'',warning:'',bytes:100,restoredTo:''},next:'2026-10-21T05:30:00+03:00'};
let failLoad=false, failTerminal=false, failConfig=false;
let terminalValue={enabled:true,confirm:false};
let offline=false, failVM=false, failZone=false, failTasks=false;
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
  if(path==='/terminal') { if(failLoad || (method==='PUT' && failTerminal))return fail(); if(method==='PUT') {await new Promise(r=>setTimeout(r,200));terminalValue={...terminalValue,...route.request().postDataJSON()};} return json(terminalValue); }
  if(path==='/system/ssh')return json({enabled:false,running:false,fingerprint:'',accounts:[]});
  if(path==='/system/certificate')return json({kind:'self-signed',names:['nas'],issuer:'nas',notAfter:'2027-10-20T00:00:00Z',fingerprint:'abc'});
  if(path==='/config-backup') { if(failConfig)return fail(); return json({backup:{settings:{enabled:true,volume:'data',keep:7,schedule:'daily',time:'03:00',weekday:0},target:'data',lastCheck:time,lastError:'',backups:[{name:'test.tar.gz',volume:'data',time,sizeBytes:1234,hostname:'nas',version:'0.6.0'}],next:'2026-10-21T03:00:00Z'},volumes:['data']}); }
  if(path==='/backups/daily/snapshots')return json({snapshots:[{id:'abc123',time,paths:['documents'],bytes:100}]});
  if(path==='/network') { if(failLoad)return fail(); return json({timezone:'Europe/Berlin'}); }
  if(path==='/system/timezone'){await new Promise(r=>setTimeout(r,250));return failZone?fail():json({});}
  if(path==='/files/jobs')return json([{id:'copy1',move:false,state:'running',from:'share/documents',to:'volume/data',dir:'backup',names:['report.pdf'],files:1,done:0,bytes:200,copied:100,current:'report.pdf',error:''}]);
  if(path==='/backups' && failTasks)return fail();
  if(path==='/backups')return json({smbTargets:[],jobs:[backup],shares:[{name:'documents',volume:'data'}],volumes:['data']});
  if(path==='/backups/daily/stop'){await new Promise(r=>setTimeout(r,500));backup.state.running=false;return json({smbTargets:[],jobs:[backup],shares:[],volumes:['data']});}
  if(path==='/backups/target')return json({target:{enabled:false,volume:'',port:0,sources:[],listening:[],problem:''},volumes:['data']});
  if(path==='/usb')return json([]);
  if(path==='/update/progress')return json({progress:{phase:'download',percent:40,file:'filesystem.squashfs'}});
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
 const {ctx,page}=await context();
 await page.goto(base+'/settings');
 const search=page.getByRole('searchbox');await search.waitFor();
 assert.equal(await page.locator('main select, main input[type=checkbox]').count(),0);
 assert.equal(await page.locator('.areas > a').count(),14);
 for(const [query,href] of [['SSH','/settings/access#access-ssh'],['Zertifikat','/settings/access#access-cert'],['Terminal','/settings/access#access-terminal'],['DNS','/settings/network#network-dns'],['Zeitzone','/settings/timezone']]) {
  await search.fill(query);assert.equal(await page.locator('.areas > a').count(),1);assert.equal(await page.locator('.areas > a').getAttribute('href'),href);
 }
 await search.fill('Backup');assert.ok(await page.locator('.areas a[href="/settings/backups/data"]').count());
 await search.fill('');await page.screenshot({path:out+'/settings-desktop.png',fullPage:true});
 failLoad=true;await page.goto(base+'/settings/timezone');await page.getByRole('button',{name:'Erneut versuchen',exact:true}).waitFor();
 failLoad=false;await page.getByRole('button',{name:'Erneut versuchen',exact:true}).click();await page.locator('select').selectOption('UTC');await page.getByText('Gespeichert',{exact:true}).waitFor();
 await page.clock.runFor(5100);assert.equal(await page.getByText('Gespeichert',{exact:true}).count(),0);
 failZone=true;await page.locator('select').selectOption('Europe/Berlin');await page.locator('[role=alert]').waitFor();assert.equal(await page.locator('select').inputValue(),'UTC');failZone=false;
 failLoad=true;await page.goto(base+'/settings/access#access-terminal');await page.locator('#access-terminal').getByRole('button',{name:'Erneut versuchen'}).waitFor();
 failLoad=false;await page.locator('#access-terminal').getByRole('button',{name:'Erneut versuchen'}).click();
 const enabled=page.getByRole('checkbox',{name:'Terminal und Konsolen erlauben',exact:true});await enabled.waitFor();
 failTerminal=true;await enabled.uncheck();await page.locator('#access-terminal [role=alert]').waitFor();assert.equal(await enabled.isChecked(),true);
 failTerminal=false;await enabled.uncheck();await page.locator('#access-terminal').getByText('Gespeichert',{exact:true}).waitFor();assert.equal(await enabled.isChecked(),false);
 await page.clock.runFor(5100);assert.equal(await page.locator('#access-terminal').getByText('Gespeichert',{exact:true}).count(),0);
 await page.getByRole('checkbox',{name:'Passwort vor VM-Konsolen erneut abfragen',exact:true}).check();await page.locator('#access-terminal').getByText('Gespeichert',{exact:true}).waitFor();
 await page.goto(base+'/settings/backups');await page.locator('.groups').waitFor();await page.getByText('Noch keine Sicherung',{exact:true}).waitFor({state:'hidden'});await page.getByText('Nicht verfügbar',{exact:true}).waitFor({state:'hidden'});
 const expectedNext=await page.evaluate(()=>new Intl.DateTimeFormat('de',{dateStyle:'medium',timeStyle:'short'}).format(new Date('2026-10-21T02:30:00Z')));
 assert.equal(await page.locator('.summary > div').nth(2).locator('strong').innerText(),expectedNext,'next backup compares instants across time zone offsets');
 await page.screenshot({path:out+'/backups-desktop.png',fullPage:true});
 const configReads=requests.filter(([,method,path])=>method==='GET'&&path==='/config-backup').length;
 await page.clock.runFor(61000);assert.equal(requests.filter(([,method,path])=>method==='GET'&&path==='/config-backup').length,configReads,'hub must not poll backup archives');
 failConfig=true;await page.getByRole('button',{name:'Status aktualisieren'}).click();await page.getByRole('button',{name:'Erneut versuchen'}).waitFor();assert.ok(await page.getByText('Nicht verfügbar',{exact:true}).count());
 failConfig=false;await page.getByRole('button',{name:'Erneut versuchen'}).click();await page.getByRole('button',{name:'Erneut versuchen'}).waitFor({state:'hidden'});
 backup.state.running=false;backup.state.lastError='fixture backup error';await page.getByRole('button',{name:'Status aktualisieren'}).click();await page.getByText(/Auftrag\/Aufträge mit Fehlern/).waitFor();backup.state.lastError='';
 await page.goto(base+'/settings/stick');await page.waitForURL('**/settings/backups/configuration');await page.getByRole('heading',{name:'Systemkonfiguration',exact:true}).waitFor();
 await page.goto(base+'/settings/backups#backup-daily');await page.waitForURL('**/settings/backups/data#backup-daily');await page.locator('#backup-daily').waitFor();
 await page.goto(base+'/settings/backups/restore');await page.locator('#backup-daily').waitFor();assert.equal(await page.getByRole('button',{name:'Jetzt sichern',exact:true}).count(),0);
 await page.locator('#backup-daily').getByRole('button',{name:'Wiederherstellen …',exact:true}).click();await page.getByRole('dialog').waitFor();await page.keyboard.press('Escape');
 await page.locator('#stick-replace').waitFor();await page.screenshot({path:out+'/recovery-desktop.png',fullPage:true});
 await ctx.close();
 for(const theme of ['graphit','hell'])for(const width of [1440,390]) {
  const {ctx,page}=await context('owner',theme,width);
  for(const path of ['settings','settings/backups','settings/backups/configuration','settings/backups/restore','settings/access','settings/timezone']) {
   await page.goto(base+'/'+path);await page.locator('h1').waitFor();await page.evaluate(()=>document.fonts.ready);
   if(path==='settings/backups')await page.getByRole('button',{name:'Status aktualisieren'}).waitFor();
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),path+' overflow');
   await page.screenshot({path:out+'/'+path.replaceAll('/','-')+'-'+theme+'-'+width+'.png',fullPage:true});
  }
  await ctx.close();
 }
 const user=await context('user');await user.page.goto(base+'/settings');await user.page.getByRole('searchbox').waitFor();assert.equal(await user.page.locator('.areas > a').count(),3);
 for(const path of ['backups','backups/data','backups/configuration','backups/restore','timezone','access']) { await user.page.goto(base+'/settings/'+path);await user.page.getByText('Dafür fehlen diesem Konto die Rechte.',{exact:true}).waitFor(); }
 assert.ok(!requests.some(([role,,path])=>role==='user'&&['/backups','/config-backup','/network','/system/ssh','/system/certificate'].includes(path)));
 await user.ctx.close();assert.deepEqual(errors,[]);console.log('UI 0.6.1 checks passed: grouping, deep links, legacy links, restore dialog, errors/retry, local feedback, no archive polling, roles and responsive themes.');
} finally { await browser.close(); }
