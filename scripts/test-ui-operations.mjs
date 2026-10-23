// Isolated API fixtures only. Never talks to a NAS or writes real storage.
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const browser=await chromium.launch({headless:true,executablePath:process.env.BROWSER_EXECUTABLE});
const base=process.env.UI_URL||'http://127.0.0.1:5179',out=process.env.UI_OUTPUT||'out/validation/ui-operations';
await mkdir(out,{recursive:true});const errors=[];
const time='2026-10-23T16:00:00Z';
const volume={name:'Data',level:'raid1',filesystem:'ext4',mountPoint:'/mnt/Data',mounted:true,state:'ok',sizeBytes:1e12,usedBytes:4e11,raidDisks:2,members:[],sync:null,mismatches:0};
const system={hostname:'titan-test',version:'0.16.0',kernel:'test',uptimeSeconds:1000,cpu:{model:'Test CPU',threads:8,usagePercent:12},memory:{totalBytes:16e9,usedBytes:3e9,freeBytes:13e9,availableBytes:13e9},temperatures:[],accelerators:[],image:{slot:'a',sizeBytes:1e9,overlayUsedBytes:0,overlayTotalBytes:512e6},stick:{present:true,label:'TITANOS'},network:{ipv4:'192.168.1.20/24'},networks:[],networkPendingSeconds:0,notificationsLatest:0};
const share=(name,volume='Data')=>({name,volume,smb:true,nfs:{enabled:false,clients:[]},access:{alex:'rw'},guest:false,browseable:true,recycle:{enabled:true,days:30},timeMachine:{enabled:false,sizeGB:0},path:`/mnt/${volume}/${name}`,available:true});
const container=(service,state='running')=>({id:service,name:'photos-'+service+'-1',image:service+':test',state,since:time,ports:[],app:'photos',service,managed:true,cpuPercent:2,memoryBytes:128e6,cpuHistory:[],webui:service==='web'?'2283':'',publicUrl:service==='web'?'https://photos.example.test':'',icon:'',updateAvailable:false});
async function context(role='owner',theme='graphit',width=1440,locale='de'){
 const ctx=await browser.newContext({locale:'de-DE',viewport:{width,height:1000}});
 await ctx.addInitScript(({theme,locale})=>{localStorage.setItem('titanos.locale',locale);localStorage.setItem('titanos.appearance',JSON.stringify({theme}));},{theme,locale});
 const data={maintenance:{settings:{enabled:false,volume:"Data",timezone:"UTC",windowFrom:"00:00",windowTo:"00:00",jobs:[]},jobs:[],history:[],available:true,error:""},growStart:0,importCount:0,restoreCount:0,cloneCount:0,failRegister:false,requestsOps:[],shares:[share('Documents'),share('Media'),share('Projects','Fast')],prefs:{appearance:{theme},shares:null,docker:null,files:null},deleted:0,failDelete:false,failSave:false,failDocker:false,webMissing:false,requests:[],putShares:[],putDocker:[],files:[]};
 await ctx.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()),path=url.pathname.slice('/api/v1'.length),method=route.request().method();data.requests.push([method,path]);
  const json=value=>route.fulfill({json:value});const fail=()=>route.fulfill({status:500,json:{error:{code:'config_write_failed',message:'fixture error'}}});

  if(path==='/maintenance'){
   if(method==='PUT'){const settings=route.request().postDataJSON();settings.jobs=settings.jobs.map((j,i)=>({...j,id:j.id||'job-'+i}));data.maintenance.settings=settings;data.maintenance.jobs=settings.jobs.map(job=>({job,state:{running:false,blocked:false,error:'',lastOK:null},next:time}));}
   return json(data.maintenance);
  }
  if(path.startsWith('/maintenance/')){const id=path.split('/')[2];const view=data.maintenance.jobs.find(v=>v.job.id===id);view.state.lastOK=time;data.maintenance.history.push({id,name:view.job.name,kind:view.job.kind,target:view.job.target,started:time,finished:time,error:''});return json(data.maintenance);}
  if(path==='/monitoring'){if(method==='PUT')data.monitor=route.request().postDataJSON();return json({settings:data.monitor??{enabled:false,volume:'Data',days:7,maxMiB:8},last:time,error:''});}
  if(path==='/monitoring/history')return json(Array.from({length:6},(_,i)=>({at:new Date(Date.parse(time)+i*60000+(i>=3?600000:0)).toISOString(),gap:i===3,cpu:10+i,ramTotal:16e9,ramAvailable:12e9,disks:[{name:'vda',readBps:1e6,writeBps:2e6,latencyMs:3}],volumes:[{name:'Data',used:4e11,total:1e12}],networks:[{name:'eth0',receive:1e6,send:3e6}]})));
  if(path==='/storage/import'&&method==='GET')return json([{id:'md:fixture',filesystem:'ext4',filesystemUUID:'fs-fixture',arrayUUID:'md-fixture',level:'raid1',disks:['vde','vdf'],size:8e12,fingerprint:'fingerprint',reason:''}]);
  if(path==='/storage/import/preview')return json({candidate:{id:'md:fixture',fingerprint:'fingerprint',filesystemUUID:'fs-fixture'},folders:['Photos','Documents'],readOnly:true,truncated:false});
  if(path==='/storage/import'&&method==='POST'){data.importCount++;if(data.failRegister)return fail();return route.fulfill({status:201});}
  if(path==='/storage/growth')return json({pending:false,operation:{},error:''});
  if(path.endsWith('/growth/preview'))return json({volume:'Data',diskSerial:'TEST4',recoveryVolume:'Safe',originalDisks:3,targetDisks:4,phase:'preview'});
  if(path.endsWith('/growth')){data.growStart++;data.requestsOps.push(route.request().postDataJSON());return route.fulfill({status:202});}
  if(path.endsWith('/usage'))return json({at:time,entries:[{name:'Documents',bytes:1024,allocated:4096,files:2,incomplete:false,error:''}],scanned:3,incomplete:false});
  if(path==='/storage/volumes/Fast/snapshots')return json([{name:'before'}]);
  if(path==='/storage/volumes/Fast/snapshots/before/files')return json({entries:[{path:'photo.jpg',directory:false,bytes:100,modified:time}],next:0});
  if(path.endsWith('/files/restore')){data.restoreCount++;return json({folder:'restored/snapshot-fixture',files:1,bytes:100});}
  if(path==='/backups/application-recovery')return json({operation:null,active:false});
  if(path.startsWith('/vm-clones/'))return json([]);
  if(path==='/operations')return json({transfers:[],maintenance:data.maintenance,backups:[],docker:{},vms:[],diskTests:[],growth:{pending:false,operation:{}}});
  if(path.endsWith('/clone')){data.cloneCount++;return route.fulfill({status:202});}

  if(path==='/session')return json({configAvailable:true,setupRequired:false,user:{name:'alex',role,twoFactor:false},hostname:'titan-test'});
  if(path==='/preferences')return json(data.prefs);
  if(path==='/preferences/shares'){data.prefs.shares=route.request().postDataJSON();data.putShares.push(data.prefs.shares);return route.fulfill({status:204});}
  if(path==='/preferences/docker'){data.prefs.docker=route.request().postDataJSON();data.putDocker.push(data.prefs.docker);return route.fulfill({status:204});}
  if(path.startsWith('/preferences/'))return route.fulfill({status:204});
  if(path==='/system')return json(system);
  if(path==='/storage')return json({volumes:[{...volume,level:'raid5'},{...volume,name:'Fast',filesystem:'btrfs'},{...volume,name:'Safe'}],disks:[{identity:'disk-id',name:'vdd',serial:'TEST4',model:'Fixture disk',sizeBytes:8e12,role:'free',mounted:false,testing:false,smart:null}],diskSleep:{idleMinutes:0,available:true,errors:[]}});
 if(false)return json({volumes:role==='user'?[]:[volume,{...volume,name:'Fast'}],disks:[],diskSleep:{idleMinutes:0,available:true,errors:[]}});
  if(path==='/users')return json([{name:'alex',role:'owner'},{name:'sam',role:'user'}]);
  if(path==='/shares'){
   if(method==='POST'){if(data.failSave)return fail();const s=route.request().postDataJSON();data.shares.push({...s,path:`/mnt/${s.volume}/${s.name}`,available:true});return route.fulfill({status:201});}
   return json(data.shares);
  }
  if(path.startsWith('/shares/')){
   if(method==='DELETE'){data.deleted++;if(data.failDelete)return fail();data.shares=data.shares.filter(s=>s.name!==decodeURIComponent(path.split('/').at(-1)));return route.fulfill({status:204});}
   if(data.failSave)return fail();return route.fulfill({status:204});
  }
  if(path==='/docker'){if(data.failDocker)return fail();return json({settings:{enabled:true,volume:'Data'},running:true,version:'test',containers:data.webMissing?[container('db','exited')]:[container('web'),container('db','exited')],apps:['photos'],jobs:{},updatesChecked:time});}
  if(path==='/plugins/tailscale')return json({active:true,state:'Running',name:'nas.example.ts.net',ips:['100.64.1.2']});
  if(path==='/notifications')return json({messages:[]});
  if(path==='/terminal')return json({enabled:true});
  if(path==='/assistant')return json({available:false});
  if(path==='/files/roots')return json(data.shares.map(s=>({id:'share/'+s.name,name:s.name,kind:'share',writable:true,recycle:true,totalBytes:1e12,freeBytes:6e11})));
  if(path==='/files/list'){data.files.push(url.searchParams.get('root'));return json([]);}
  if(path==='/files/jobs'||path==='/files/uploads'||path==='/usb')return json([]);
  if(path==='/backups')return json({jobs:[],shares:[],volumes:[]});
  if(path.endsWith('/snapshots')&&path.startsWith('/vms/'))return json({snapshots:[],schedule:{enabled:false,time:'03:00',days:[],keep:7,memory:false,allowPause:false},job:null,pending:false,reason:''});
 if(path.endsWith('/addresses'))return json({addresses:[],source:''});
 if(path==='/hardware')return json({hardware:{pci:[],usb:[],iommu:false},usedBy:{}});
 if(path==='/vms/media')return json({media:[]});
 if(path==='/vms')return json({vms:[{name:'source',displayName:'Source VM',volume:'Data',state:'stopped',available:true,job:null,description:'',template:'linux',memoryMiB:512,vcpus:1,disks:[],iso:'',autostart:false,cpuPin:[],exclusive:false,folders:[],tpm:false,secureBoot:false,pci:[],usb:[],network:'user',mac:'52:54:00:00:00:01',cpuPercent:0,memoryBytes:0,since:''}],kvm:true});
  if(path==='/update/progress')return json({progress:{phase:'idle'}});
  return json({});
 });
 const page=await ctx.newPage();page.setDefaultTimeout(12000);page.on('pageerror',e=>errors.push(String(e)));page.on('console',m=>{if(m.type()==='error'&&/TypeError|ReferenceError|SyntaxError/.test(m.text()))errors.push(m.text());});
 return {ctx,page,data};
}
try{
 const {ctx,page,data}=await context();
 await page.goto(base+'/tools/maintenance');await page.getByRole('button',{name:'Zeitplan hinzufügen',exact:true}).click();
 let dialog=page.getByRole('dialog');await dialog.getByRole('textbox',{name:'Name',exact:true}).fill('Snapshot plan');await dialog.getByRole('combobox',{name:'Vorgang',exact:true}).selectOption('snapshot');await dialog.getByRole('combobox',{name:'Ziel',exact:true}).selectOption('Fast');await dialog.getByRole('checkbox',{name:/Ältere automatische Snapshots/}).check();await dialog.getByRole('button',{name:'Speichern',exact:true}).click();await dialog.waitFor({state:'hidden'});assert.equal(data.maintenance.settings.jobs[0].kind,'snapshot');
 await page.getByRole('button',{name:'Jetzt starten',exact:true}).click();await page.getByRole('heading',{name:'Letzte Ergebnisse',exact:true}).waitFor();assert.equal(data.maintenance.history.length,1);
 await page.goto(base+'/tools/monitoring');await page.getByRole('img',{name:'CPU (%)',exact:true}).waitFor();assert.equal(await page.locator('svg[aria-label="CPU (%)"] polyline').count(),2);await page.getByRole('checkbox',{name:'Aktiviert',exact:true}).check();await page.getByRole('button',{name:'Speichern',exact:true}).click();assert.equal(data.monitor.enabled,true);
 await page.goto(base+'/storage/import');await page.getByRole('button',{name:'Lesend prüfen',exact:true}).click();dialog=page.getByRole('dialog');await dialog.getByRole('textbox',{name:'Name',exact:true}).fill('Archive');await dialog.getByRole('textbox',{name:'Zur Bestätigung Archive eingeben',exact:true}).fill('Archive');data.failRegister=true;await dialog.getByRole('button',{name:'Volume registrieren',exact:true}).click();await dialog.locator('.notice-error').waitFor();assert.equal(await dialog.getByRole('textbox',{name:'Name',exact:true}).inputValue(),'Archive');data.failRegister=false;await dialog.getByRole('button',{name:'Volume registrieren',exact:true}).click();await dialog.waitFor({state:'hidden'});assert.equal(data.importCount,2);
 await page.goto(base+'/storage/manage?volume=Data&mode=grow');await page.getByRole('combobox',{name:'Neue Platte',exact:true}).selectOption('vdd');await page.getByRole('combobox',{name:'Separates Rettungs-Volume',exact:true}).selectOption('Safe');await page.getByRole('button',{name:'Lesend prüfen',exact:true}).click();dialog=page.getByRole('dialog');assert.equal(data.growStart,0);await dialog.getByRole('checkbox').check();await dialog.getByRole('textbox',{name:'Zur Bestätigung Data eingeben',exact:true}).fill('Data');await dialog.getByRole('button',{name:'Kapazität erweitern',exact:true}).click();await dialog.waitFor({state:'hidden'});assert.equal(data.growStart,1);assert.equal(data.requestsOps.at(-1).identity,'disk-id');
 await page.goto(base+'/storage/manage?volume=Fast&mode=snapshots');await page.getByRole('combobox',{name:'Snapshots',exact:true}).selectOption('before');await page.getByRole('checkbox').check();await page.getByRole('combobox',{name:'Ziel',exact:true}).selectOption('Safe');await page.getByRole('button',{name:/Auswahl wiederherstellen/}).click();await page.getByText(/Wiederhergestellt:/).waitFor();assert.equal(data.restoreCount,1);
 await page.goto(base+'/vms?vm=source');await page.getByRole('heading',{name:'Source VM',exact:true}).waitFor();await page.getByRole('button',{name:'Weitere Aktionen',exact:true}).click();await page.getByRole('menuitem',{name:'VM klonen',exact:true}).click();dialog=page.getByRole('dialog');await dialog.getByRole('textbox',{name:'Name',exact:true}).fill('copy');await dialog.getByRole('button',{name:'VM klonen',exact:true}).click();await dialog.waitFor({state:'hidden'});assert.equal(data.cloneCount,1);
 await ctx.close();
 for(const theme of ['graphit','hell'])for(const width of [1440,390]){
  const test=await context('owner',theme,width);
  for(const route of ['/tools/maintenance','/tools/monitoring','/tools/tasks','/storage/import','/storage/manage?volume=Data']){await test.page.goto(base+route);await test.page.getByRole('heading',{level:1}).waitFor();await test.page.waitForTimeout(150);assert.ok(await test.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),route+' overflow');await test.page.screenshot({path:out+`/${route.split('?')[0].replaceAll('/','-')}-${theme}-${width}.png`,fullPage:true});}
  await test.ctx.close();
 }
 const user=await context('user');await user.page.goto(base+'/tools/maintenance');await user.page.getByRole('alert').waitFor();assert.equal(user.data.requests.filter(([,p])=>p==='/maintenance').length,0);await user.ctx.close();
 assert.deepEqual(errors,[]);console.log('Operations UI: maintenance, monitoring gaps/settings, import errors, RAID confirmation, snapshot selection, clone, roles and both mobile/desktop themes passed.');
}finally{await browser.close();}
