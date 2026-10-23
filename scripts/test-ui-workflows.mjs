// Isolated API fixtures only. Never talks to a NAS or writes real storage.
import assert from 'node:assert/strict';
import {mkdir} from 'node:fs/promises';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const browser=await chromium.launch({headless:true,executablePath:process.env.BROWSER_EXECUTABLE});
const base=process.env.UI_URL||'http://127.0.0.1:5179',out=process.env.UI_OUTPUT||'out/validation/ui-workflows';
await mkdir(out,{recursive:true});const errors=[];
const time='2026-10-23T16:00:00Z';
const volume={name:'Data',level:'raid1',filesystem:'ext4',mountPoint:'/mnt/Data',mounted:true,state:'ok',sizeBytes:1e12,usedBytes:4e11,raidDisks:2,members:[],sync:null,mismatches:0};
const system={hostname:'titan-test',version:'0.16.0',kernel:'test',uptimeSeconds:1000,cpu:{model:'Test CPU',threads:8,usagePercent:12},memory:{totalBytes:16e9,usedBytes:3e9,freeBytes:13e9,availableBytes:13e9},temperatures:[],accelerators:[],image:{slot:'a',sizeBytes:1e9,overlayUsedBytes:0,overlayTotalBytes:512e6},stick:{present:true,label:'TITANOS'},network:{ipv4:'192.168.1.20/24'},networks:[],networkPendingSeconds:0,notificationsLatest:0};
const share=(name,volume='Data')=>({name,volume,smb:true,nfs:{enabled:false,clients:[]},access:{alex:'rw'},guest:false,browseable:true,recycle:{enabled:true,days:30},timeMachine:{enabled:false,sizeGB:0},path:`/mnt/${volume}/${name}`,available:true});
const container=(service,state='running')=>({id:service,name:'photos-'+service+'-1',image:service+':test',state,since:time,ports:[],app:'photos',service,managed:true,cpuPercent:2,memoryBytes:128e6,cpuHistory:[],webui:service==='web'?'2283':'',publicUrl:service==='web'?'https://photos.example.test':'',icon:'',updateAvailable:false});
async function context(role='owner',theme='graphit',width=1440,locale='de'){
 const ctx=await browser.newContext({locale:'de-DE',viewport:{width,height:1000}});
 await ctx.addInitScript(({theme,locale})=>{localStorage.setItem('titanos.locale',locale);localStorage.setItem('titanos.appearance',JSON.stringify({theme}));},{theme,locale});
 const data={shares:[share('Documents'),share('Media'),share('Projects','Fast')],prefs:{appearance:{theme},shares:null,docker:null,files:null},deleted:0,failDelete:false,failSave:false,failDocker:false,webMissing:false,requests:[],putShares:[],putDocker:[],files:[],filePaths:[]};
 await ctx.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()),path=url.pathname.slice('/api/v1'.length),method=route.request().method();data.requests.push([method,path]);
  const json=value=>route.fulfill({json:value});const fail=()=>route.fulfill({status:500,json:{error:{code:'config_write_failed',message:'fixture error'}}});
  if(path==='/session')return json({configAvailable:true,setupRequired:false,user:{name:'alex',role,twoFactor:false},hostname:'titan-test'});
  if(path==='/preferences')return json(data.prefs);
  if(path==='/preferences/shares'){data.prefs.shares=route.request().postDataJSON();data.putShares.push(data.prefs.shares);return route.fulfill({status:204});}
  if(path==='/preferences/docker'){data.prefs.docker=route.request().postDataJSON();data.putDocker.push(data.prefs.docker);return route.fulfill({status:204});}
  if(path.startsWith('/preferences/'))return route.fulfill({status:204});
  if(path==='/system')return json(system);
  if(path==='/storage')return json({volumes:role==='user'?[]:[volume,{...volume,name:'Fast'}],disks:[],diskSleep:{idleMinutes:0,available:true,errors:[]}});
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
  if(path==='/files/list'){data.files.push(url.searchParams.get('root'));data.filePaths.push(url.searchParams.get('path'));return json([]);}
  if(path==='/files/jobs'||path==='/files/uploads'||path==='/usb')return json([]);
  if(path==='/backups')return json({jobs:[],shares:[],volumes:[]});
  if(path==='/vms')return json({vms:[],kvm:true});
  if(path==='/update/progress')return json({progress:{phase:'idle'}});
  return json({});
 });
 const page=await ctx.newPage();page.setDefaultTimeout(12000);page.on('pageerror',e=>errors.push(String(e)));page.on('console',m=>{if(m.type()==='error'&&/TypeError|ReferenceError|SyntaxError/.test(m.text()))errors.push(m.text());});
 return {ctx,page,data};
}
try{
 const {ctx,page,data}=await context();
 await page.goto(base+'/shares');await page.getByRole('button',{name:'Documents',exact:true}).waitFor();
 assert.equal(await page.locator('tbody tr').count(),3);
 await page.getByRole('button',{name:'Documents',exact:true}).click();await page.getByRole('button',{name:'Verbinden',exact:true}).click();
 assert.equal(await page.locator('dialog code').first().innerText(),'\\\\192.168.1.20\\Documents');await page.keyboard.press('Escape');
 await page.getByRole('link',{name:'In Dateien öffnen',exact:true}).click();await page.waitForURL(/\/files\?root=share/);await page.waitForFunction(()=>document.body.textContent.includes('Documents'));
 await page.waitForTimeout(300);assert.ok(data.files.includes('share/Documents'));
 await page.goto(base+'/files?root=share%2FDocuments&path=archive');await page.getByRole('button',{name:'archive',exact:true}).waitFor();assert.ok(data.filePaths.includes('archive'));
 await page.goto(base+'/shares');await page.getByRole('button',{name:'Neue Freigabe',exact:true}).click();
 const dialog=page.getByRole('dialog');await dialog.locator('input[type=text]').fill('New Share');await dialog.getByRole('tab',{name:'Zugriff',exact:true}).click();await dialog.getByRole('tab',{name:'Allgemein',exact:true}).click();assert.equal(await dialog.locator('input[type=text]').inputValue(),'New Share');
 await page.keyboard.press('Escape');await page.getByText('Es gibt ungespeicherte Änderungen.',{exact:true}).waitFor();await page.getByRole('button',{name:'Weiterbearbeiten',exact:true}).click();assert.equal(await dialog.locator('input[type=text]').inputValue(),'New Share');
 data.failSave=true;await dialog.getByRole('button',{name:'Freigabe anlegen',exact:true}).click();await dialog.locator('[role=alert]').waitFor();assert.equal(await dialog.locator('input[type=text]').inputValue(),'New Share');data.failSave=false;
 await dialog.getByRole('button',{name:'Freigabe anlegen',exact:true}).click();await dialog.waitFor({state:'hidden'});await page.getByRole('button',{name:'New Share',exact:true}).waitFor();
 await page.getByRole('button',{name:'Aktionen für Media',exact:true}).click();await page.getByRole('menuitem',{name:'Freigabe löschen',exact:true}).click();data.failDelete=true;
 await page.keyboard.press('Enter');assert.equal(data.deleted,0);if(!await page.getByRole('dialog').count()){await page.getByRole('button',{name:'Aktionen für Media',exact:true}).click();await page.getByRole('menuitem',{name:'Freigabe löschen',exact:true}).click();}
 await page.getByRole('dialog').getByRole('button',{name:'Freigabe löschen',exact:true}).click();await page.getByRole('dialog').locator('[role=alert]').waitFor();assert.equal(await page.getByRole('dialog').count(),1);
 data.failDelete=false;await page.getByRole('dialog').getByRole('button',{name:'Freigabe löschen',exact:true}).click();await page.getByRole('dialog').waitFor({state:'hidden'});
 await page.getByRole('combobox',{name:'Volume',exact:true}).selectOption('Fast');await page.waitForTimeout(150);assert.equal(data.putShares.at(-1).volume,'Fast');await page.reload();await page.getByRole('button',{name:'Projects',exact:true}).waitFor();assert.equal(await page.locator('tbody tr').count(),1);
 await page.goto(base+'/docker');const stack=page.locator('.stack').first();await stack.waitFor();assert.equal(await stack.locator('.members').count(),0);await stack.getByRole('link',{name:'WebUI',exact:true}).waitFor();assert.equal(await stack.getByRole('link',{name:'WebUI',exact:true}).getAttribute('href'),'http://192.168.1.20:2283/');assert.equal(await stack.getByRole('link',{name:'Öffentlich',exact:true}).count(),1);
 await stack.getByRole('button',{name:'Verknüpfungen',exact:true}).click();await page.getByRole('dialog').locator('select').selectOption('web');await page.getByRole('dialog').getByRole('button',{name:'Speichern',exact:true}).click();assert.equal(data.putDocker.at(-1).primaryServices['app:photos'],'web');await page.getByRole('dialog').waitFor({state:'hidden'});
 await page.getByRole('searchbox',{name:'Container suchen …',exact:true}).fill('db');await stack.locator('.members').waitFor();assert.equal(await stack.locator('.row').count(),1);
 await page.getByRole('searchbox',{name:'Container suchen …',exact:true}).fill('');await stack.getByRole('button',{name:'1 Dienste prüfen',exact:true}).click();assert.equal(await stack.locator('.row').count(),2);
 data.failDocker=true;await page.getByText(/Angezeigte Werte können veraltet sein/).waitFor({timeout:15000});assert.equal(await stack.locator('.row').count(),2);data.failDocker=false;await page.getByText(/Angezeigte Werte können veraltet sein/).waitFor({state:'hidden',timeout:15000});
 data.webMissing=true;await page.getByText('Der zugeordnete Dienst web fehlt.',{exact:true}).waitFor({timeout:15000});
 // Command palette loads only authorized roots and navigates, never runs a NAS action.
 await page.keyboard.press('Control+k');await page.getByRole('dialog').waitFor();await page.getByRole('dialog').getByRole('searchbox').fill('SSH');await page.getByRole('button',{name:/Zugang › SSH/}).click();await page.waitForURL(/settings\/access#access-ssh/);
 await page.goto(base+'/shares');await page.getByRole('button',{name:'Projects',exact:true}).waitFor();await page.screenshot({path:out+'/shares-desktop.png',fullPage:true});await ctx.close();
 for(const theme of ['graphit','hell'])for(const width of [1440,390]){
  const test=await context('owner',theme,width);await test.page.goto(base+'/shares');await test.page.getByRole('button',{name:'Documents',exact:true}).waitFor();await test.page.screenshot({path:out+`/shares-${theme}-${width}.png`,fullPage:true});
  assert.ok(await test.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
  await test.page.goto(base+'/docker');await test.page.locator('.stack-toggle').waitFor();assert.equal(await test.page.locator('.members').count(),0);await test.page.screenshot({path:out+`/docker-${theme}-${width}.png`,fullPage:true});assert.ok(await test.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));await test.ctx.close();
 }
 const user=await context('user');await user.page.goto(base+'/files');await user.page.getByRole('heading',{name:'Dateien',exact:true}).waitFor();await user.page.keyboard.press('Control+k');await user.page.getByRole('dialog').waitFor();assert.equal(await user.page.locator('#palette-results').getByRole('button',{name:/Docker/}).count(),0);assert.equal(user.data.requests.filter(([,p])=>p==='/docker'||p==='/vms').length,0);await user.ctx.close();
 assert.deepEqual(errors,[]);console.log('UI workflows: shares, form state, delete failure, preferences, stack links/search/stale status, palette roles and desktop/mobile passed.');
}catch(error){console.error('Browser errors:',errors);for(const context of browser.contexts())for(const page of context.pages())await page.screenshot({path:out+'/failure.png',fullPage:true}).catch(()=>{});throw error;}finally{await browser.close();}
