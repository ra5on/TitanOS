// Browser checks against isolated API fixtures. No NAS operations or real disks.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-files-ui.mjs
import assert from 'node:assert/strict';
import {resolve as resolvePath} from 'node:path';
import { mkdir, writeFile } from 'node:fs/promises';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.UI_URL || 'http://127.0.0.1:5179';
const out = process.env.UI_OUTPUT || 'out/validation/files-names-trash';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({headless:true, executablePath:process.env.BROWSER_EXECUTABLE});
const errors = [];
const time = '2026-10-20T09:00:00Z';
const system = { hostname:'titan-test',version:'0.7.1',kernel:'test',uptimeSeconds:1000,cpu:{model:'Test CPU',threads:8,usagePercent:12},memory:{totalBytes:16e9,usedBytes:3e9,freeBytes:13e9,availableBytes:13e9},temperatures:[],accelerators:[],image:{slot:'a',sizeBytes:1e9,overlayUsedBytes:0,overlayTotalBytes:512e6},stick:{present:true,label:'TITANOS'},network:null,networks:[],networkPendingSeconds:0,notificationsLatest:1};
const volume={name:'data',level:'raid1',filesystem:'ext4',mountPoint:'/mnt/data',mounted:true,state:'checking',sizeBytes:1e12,usedBytes:4e11,raidDisks:2,members:[],sync:{percent:32,speedBytesPerSecond:1e8,remainingSeconds:500},mismatches:0};
const vm={name:'linux',displayName:'Linux Server',description:'Test VM',template:'linux',memoryMiB:2048,vcpus:2,volume:'data',disks:[],iso:'',autostart:true,cpuPin:[],exclusive:false,folders:[],tpm:false,secureBoot:false,pci:[],usb:[],network:'user',job:null,state:'stopped',available:true,cpuPercent:4,memoryBytes:1e9,since:time};
const backup={job:{id:'daily',sources:['documents'],destination:{type:'local',volume:'data',folder:'backups'},enabled:true,every:'daily',weekday:0,time:'02:00',keep:{daily:7,weekly:2,monthly:1}},secretSet:{},state:{running:true,phase:'backup',percent:25,bytesDone:25,bytesTotal:100,lastRun:time,lastOK:time,lastError:'',warning:'',bytes:100,restoredTo:''},next:'2026-10-21T05:30:00+03:00'};
let mediaFeatures=false,progressCopied=10;
let largeFolder=false;
let textContent='Aktuelle Fassung',previousContent='Vorherige Fassung',textVersion='current-v1';const textSaves=[],duplicateCalls=[],createdFiles=[];
let batchUpload=false,releaseUpload,onUploadHeld,heldUpload=false,failedBatchOnce=false;const uploadData={},batchChunks=[],batchFinishes=[];
let externalEntry=false;const controls=[],usageCalls=[];let jobControl={paused:false,priority:0,rate:0};
let toolsFeature=false;const searches=[],comparisons=[],batchRenames=[],extractions=[],undoCalls=[];let undoRecords=[];
let newFeatures=false,retryFailed=true,freeFixture=1e9;const retries=[],trashRestores=[],uploadStarts=[];
let hiddenFixture=false, conflictFixture=false, jobFinished=false;
let failLoad=false, failTerminal=false, failConfig=false;
let terminalValue={enabled:true,confirm:false};
let offline=false, failVM=false, failZone=false, failTasks=false;
const requests=[];
let usbConnected=false, usbMounted=false, usbFolderMissing=false;const usbMounts=[],usbListings=[];
const restoreFolders=[];let rejectRestoreFolder=false;
const usbBackup={state:{running:false,percent:25,bytesDone:25,bytesTotal:100,lastOK:time,lastError:'',warning:''},job:{id:'USB-Sicherung',destination:{type:'usb-copy',deviceId:'disk-uuid',folder:'Sicherungen/Backup'}}};
let recycleEnabled=true;const deletions=[];const filePrefs={};const copies=[];let failPreferences=false;
async function context(role='owner',theme='graphit',width=1440) {
 const ctx=await browser.newContext({locale:'de-DE',viewport:{width,height:1000}});
 await ctx.addInitScript(({theme})=>{localStorage.setItem('titanos.locale','de');localStorage.setItem('titanos.appearance',JSON.stringify({theme}));},{theme});
 await ctx.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname.slice('/api/v1'.length); const method=route.request().method(); requests.push([role,method,path]);
  const json=async data=>route.fulfill({json:data});
  const fail=async()=>route.fulfill({status:500,json:{error:{code:'internal',message:'fixture failure'}}});
  if(path==='/files/create'){createdFiles.push(route.request().postDataJSON());return json({});}
  if(path==='/files/text/previous')return json({content:previousContent,version:'previous-v1'});
  if(path==='/files/text'){if(method==='PUT'){const req=route.request().postDataJSON();textSaves.push(req);if(req.version!==textVersion)return route.fulfill({status:409,json:{error:{code:'text_conflict'}}});previousContent=textContent;textContent=req.content;textVersion='current-v2';}return json({content:textContent,version:textVersion});}
  if(path==='/files/duplicates'){duplicateCalls.push(route.request().postDataJSON());return json({scanned:4,hashed:2,groups:[{sha256:'a'.repeat(64),size:10,paths:['Datei2.txt','Ordner/Kopie.txt'],versions:{'Datei2.txt':'dup-a','Ordner/Kopie.txt':'dup-b'}}]});}
  if(batchUpload&&path==='/files/uploads'&&method==='POST'){const req=route.request().postDataJSON(),id='batch-'+Object.keys(uploadData).length;uploadData[id]={id,name:req.name,dir:req.dir,size:req.size,offset:0};return json(uploadData[id]);}
  if(batchUpload&&path==='/files/uploads'&&method==='GET')return json(Object.values(uploadData));
  if(batchUpload&&path.startsWith('/files/uploads/batch-')){const id=path.split('/')[3],data=uploadData[id];if(path.endsWith('/finish')){batchFinishes.push(id);return json(data);}if(method==='GET')return json(data);if(method==='PATCH'){batchChunks.push({id,offset:new URL(route.request().url()).searchParams.get('offset')});if(!heldUpload){heldUpload=true;await new Promise(resolve=>{releaseUpload=resolve;onUploadHeld?.();});}if(id==='batch-1'&&!failedBatchOnce){failedBatchOnce=true;return route.fulfill({status:500,json:{error:{code:'internal'}}});}data.offset+=route.request().postDataBuffer().length;return json(data);}}
  if(path==='/files/roots')return json([{id:'share/Backup',name:'Backup',kind:'share',writable:role==='owner',recycle:recycleEnabled,freeBytes:freeFixture,totalBytes:2e9},...(role==='owner' && usbMounted?[{id:'usb/Backup-abc123',name:'Backup USB',kind:'usb',writable:true,recycle:false,freeBytes:freeFixture,totalBytes:2e9}]:[]),...(role==='owner'?[{id:'volume/backup',name:'backup',kind:'volume',writable:true,recycle:false,freeBytes:freeFixture,totalBytes:2e9}]:[])]);
  if(path==='/files/list' && new URL(route.request().url()).searchParams.get('root')==='usb/Backup-abc123'){
   const folder=new URL(route.request().url()).searchParams.get('path');usbListings.push(folder);
   if(usbFolderMissing)return route.fulfill({status:404,json:{error:{code:'file_not_found',message:'missing'}}});
   return json([{name:'USB-Inhalt.txt',dir:false,size:20,modified:time}]);
  }
  if(mediaFeatures&&path==='/files/download'&&new URL(route.request().url()).searchParams.get('path')==='Dokument.pdf'){const objects=['<< /Type /Catalog /Pages 2 0 R >>','<< /Type /Pages /Kids [3 0 R] /Count 1 >>','<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R >>','<< /Length 0 >>\nstream\n\nendstream'];let pdf='%PDF-1.4\n',offsets=[0];objects.forEach((o,i)=>{offsets.push(pdf.length);pdf+=`${i+1} 0 obj\n${o}\nendobj\n`;});const start=pdf.length;pdf+='xref\n0 5\n0000000000 65535 f \n'+offsets.slice(1).map(o=>String(o).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n${start}\n%%EOF`;return route.fulfill({body:pdf,contentType:'application/octet-stream'});}
  if(mediaFeatures&&path==='/files/download'&&new URL(route.request().url()).searchParams.get('path')==='Ton.wav'){const data=Buffer.alloc(16044);data.write('RIFF',0);data.writeUInt32LE(16036,4);data.write('WAVEfmt ',8);data.writeUInt32LE(16,16);data.writeUInt16LE(1,20);data.writeUInt16LE(1,22);data.writeUInt32LE(8000,24);data.writeUInt32LE(16000,28);data.writeUInt16LE(2,32);data.writeUInt16LE(16,34);data.write('data',36);data.writeUInt32LE(16000,40);return route.fulfill({body:data,contentType:'audio/wav'});}
  if(path==='/files/download')return route.fulfill({body:new URL(route.request().url()).searchParams.get('path')?.endsWith('.JPG')?Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a5X8AAAAASUVORK5CYII=','base64'):Buffer.from('<script>window.previewExecuted=true</script>\nNotiz'),contentType:'application/octet-stream'});
  if(path==='/files/list' && new URL(route.request().url()).searchParams.get('path')==='Ordner')return json([{name:'notiz.txt',dir:false,size:100,modified:time}]);
  if(path==='/files/list'&&largeFolder)return json(Array.from({length:5000},(_,i)=>({name:`Datei${i}.txt`,dir:false,size:i,modified:time})));
  if(path==='/files/list')return json([...(mediaFeatures?[{name:'Zweites.JPG',size:1024,dir:false,modified:time},{name:'Ton.wav',size:16044,dir:false,modified:time},{name:'Film.mp4',size:100,dir:false,modified:time},{name:'Dokument.pdf',size:100,dir:false,modified:time}]:[]),...(externalEntry?[{name:'Extern.txt',dir:false,size:40,modified:time}]:[]),{name:'Ordner',dir:true,size:0,modified:time},{name:'Urlaub.JPG',dir:false,size:1024,modified:time},{name:'Datei10.txt',dir:false,size:20,modified:time},{name:'Datei2.txt',dir:false,size:10,modified:time},...(hiddenFixture?[{name:'.env',dir:false,size:5,modified:time}]:[]),...(toolsFeature?[{name:'Backup.zip',dir:false,size:200,modified:time}]:[])]);
  if(path==='/files/undo')return json(undoRecords);
  if(path.startsWith('/files/undo/')){undoCalls.push(path);undoRecords=[];return json({});}
  if(path==='/files/search'){searches.push(route.request().postDataJSON());return json({entries:[{name:'notiz.txt',path:'Ordner/notiz.txt',dir:false,size:100,modified:time}],scanned:5,truncated:false});}
  if(path==='/files/compare'){comparisons.push(route.request().postDataJSON());return json({entries:[{path:'Datei2.txt',leftVersion:'left-v1',rightVersion:'right-v1',state:'different',Left:{name:'Datei2.txt',size:10,dir:false,modified:time},Right:{name:'Datei2.txt',size:10,dir:false,modified:time}},{path:'Ordner/notiz.txt',leftVersion:'nested-v1',state:'left',Left:{name:'notiz.txt',size:100,dir:false,modified:time}}],same:2,checksum:route.request().postDataJSON().checksum});}
  if(path==='/files/rename-preview'){const req=route.request().postDataJSON();return json(req.names.map((name,i)=>({from:name,to:req.prefix+name,version:'version-'+i,problem:req.prefix==='Konflikt'?'exists':''})));}
  if(path==='/files/rename-batch'){const req=route.request().postDataJSON();batchRenames.push(req);undoRecords=[{id:'undo-batch',kind:'rename',count:req.changes.length,roots:['share/Backup'],busy:false}];return json({done:req.changes.length,undo:'undo-batch',error:''});}
  if(path==='/files/archive')return json({entries:[{path:'Fotos/a.txt',size:10,dir:false},{path:'b.txt',size:20,dir:false}],bytes:30,version:'zip-version'});
  if(path==='/files/extract'){extractions.push(route.request().postDataJSON());return json({id:'extract1'});}
  if(path==='/files/usage'){usageCalls.push(route.request().postDataJSON());return json({bytes:100,files:2,folders:1,children:[{path:'Ordner',dir:true,bytes:100,files:2}],largest:[{path:'Ordner/notiz.txt',bytes:100,files:1,dir:false}]});}
  if(path==='/files/jobs/copy1/control'){jobControl=route.request().postDataJSON();controls.push(jobControl);return json({});}
  if(path==='/files/zip')return route.fulfill({body:'zip fixture',headers:{'Content-Type':'application/zip','Content-Disposition':'attachment; filename=Meine-Sicherung.zip'}});
  if(path==='/files/properties')return json({access:{read:true,write:role==='owner',delete:role==='owner',reason:role==='owner'?'':'share'},kind:'text/plain',size:10,modified:time,permissions:'-rw-rw----',uid:1000,gid:1000,...(new URL(route.request().url()).searchParams.has('checksum')?{sha256:'a'.repeat(64)}:{})});
  if(newFeatures && path==='/files/jobs/copy1/retry'){retries.push(path);retryFailed=false;return json({});}
  if(newFeatures && path==='/files/trash')return json(new URL(route.request().url()).searchParams.get('flat')==='1'?[{name:'a.txt',size:20,allocated:4096,modified:time,path:'tester/Fotos/a.txt',original:'Fotos/a.txt'},{name:'b.txt',size:5,allocated:4096,modified:'2025-01-01T00:00:00Z',path:'tester/Archiv/b.txt',original:'Archiv/b.txt'}]:[{name:'tester',dir:true,size:0,modified:time}]);
  if(path==='/files/trash/preview')return json(route.request().postDataJSON().paths.map(path=>({path,target:path.split('/').slice(1).join('/'),conflict:true})));
  if(path==='/files/trash/restore'){trashRestores.push(route.request().postDataJSON());return json({});}
  if(path==='/files/uploads' && method==='POST'){uploadStarts.push(route.request().postDataJSON());return json({id:'upload1',name:'Datei2.txt',offset:0});}
  if(path.startsWith('/files/uploads/upload1'))return json({});
  if(path==='/files/uploads' || path==='/files/trash')return json([]);
  if(path==='/files/delete'){deletions.push(route.request().postDataJSON());return json({});}
  if(path==='/session')return json({configAvailable:true,setupRequired:false,user:{name:'tester',role,twoFactor:false},hostname:'titan-test'});
  if(path==='/preferences/files'){if(failPreferences)return fail();filePrefs[role]=route.request().postDataJSON();return route.fulfill({status:204});}
  if(path==='/files/mkdir'){restoreFolders.push(route.request().postDataJSON());if(rejectRestoreFolder)return route.fulfill({status:409,json:{error:{code:'file_exists',message:'exists'}}});return json({});}
  if(path==='/backups/progress')return json({'USB-Sicherung':usbBackup.state});
  if(path==='/files/inspect')return json({files:1,bytes:123,conflicts:conflictFixture?[{path:'Datei2.txt',sourcePath:'Datei2.txt',sourceVersion:'source-v1',targetVersion:'target-v1',source:{name:'Datei2.txt',dir:false,size:10,modified:time},target:{name:'Datei2.txt',dir:false,size:30,modified:'2025-01-01T00:00:00Z'}}]:[],truncated:false});
  if(path==='/files/copy'){copies.push(route.request().postDataJSON());return json({id:'new-copy'});}
  if(path==='/preferences')return json({appearance:{theme},dashboard:null,docker:null,vnc:null,sound:null,files:filePrefs[role]??null});
  if(path==='/system'){if(offline)return route.abort();return json(system);}
  if(path==='/storage')return json({volumes:role==='user'?[]:[volume],disks:[],diskSleep:{idleMinutes:0,available:true,errors:[]}});
  if(path==='/notifications')return json({messages:[{id:1,time,severity:'warning',kind:'volume_full',subject:'data',title:'Volume',text:'Full'}]});
  if(path==='/terminal') { if(failLoad || (method==='PUT' && failTerminal))return fail(); if(method==='PUT') {await new Promise(r=>setTimeout(r,200));terminalValue={...terminalValue,...route.request().postDataJSON()};} return json(terminalValue); }
  if(path==='/system/ssh')return json({enabled:false,running:false,fingerprint:'',accounts:[]});
  if(path==='/system/certificate')return json({kind:'self-signed',names:['nas'],issuer:'nas',notAfter:'2027-10-20T00:00:00Z',fingerprint:'abc'});
  if(path==='/config-backup') { if(failConfig)return fail(); return json({backup:{settings:{enabled:true,volume:'data',keep:7,schedule:'daily',time:'03:00',weekday:0},target:'data',lastCheck:time,lastError:'',backups:[{name:'test.tar.gz',volume:'data',time,sizeBytes:1234,hostname:'nas',version:'0.7.0'}],next:'2026-10-21T03:00:00Z'},volumes:['data']}); }
  if(path==='/backups/daily/snapshots')return json({snapshots:[{id:'abc123',time,paths:['documents'],bytes:100}]});
  if(path==='/network') { if(failLoad)return fail(); return json({timezone:'Europe/Berlin'}); }
  if(path==='/system/timezone'){await new Promise(r=>setTimeout(r,250));return failZone?fail():json({});}
  if(newFeatures && path==='/files/jobs')return json([{...jobControl,id:'copy1',move:false,state:retryFailed?'failed':'running',retryable:retryFailed,from:'share/Backup',to:'volume/backup',dir:'',names:['Datei2.txt'],files:2,done:1,bytes:30,copied:progressCopied,current:'Datei10.txt',error:retryFailed?'connection interrupted':''}]);
  if(path==='/files/jobs')return json([{id:'copy1',move:false,state:jobFinished?'done':'running',from:'share/documents',to:'volume/backup',dir:'backup',names:['report.pdf'],files:1,done:0,bytes:200,copied:100,current:'report.pdf',error:''}]);
  if(path==='/backups' && failTasks)return fail();
  if(path==='/backups')return json({smbTargets:[],jobs:[backup,usbBackup],shares:[{name:'documents',volume:'data'}],volumes:['data']});
  if(path==='/backups/daily/stop'){await new Promise(r=>setTimeout(r,500));backup.state.running=false;return json({smbTargets:[],jobs:[backup,usbBackup],shares:[],volumes:['data']});}
  if(path==='/backups/target')return json({target:{enabled:false,volume:'',port:0,sources:[],listening:[],problem:''},volumes:['data']});
  if(path==='/usb/disk-uuid/mount'){usbMounts.push(path);usbMounted=true;}
  if(path==='/usb' || path==='/usb/disk-uuid/mount')return json(usbConnected?[{id:'disk-uuid',name:'Backup USB',fsType:'ext4',freeBytes:1000000000,supported:true,mounted:usbMounted,mount:usbMounted?'Backup-abc123':'',root:usbMounted?'usb/Backup-abc123':''}]:[]);
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
async function pathField(p){const field=p.getByLabel('Ordnerpfad',{exact:true});if(!(await field.count()))await p.getByRole('button',{name:'Pfad bearbeiten',exact:true}).click();return field;}
async function shownPath(p){const field=await pathField(p);const value=await field.inputValue();await field.press('Escape');return value;}
try {
 const {ctx,page}=await context();
 await page.goto(base+'/files');await page.getByText('Urlaub.JPG',{exact:true}).waitFor();
 assert.equal(await page.locator('details.advanced').evaluate(el=>el.open),false);
 await page.setViewportSize({width:1440,height:500});
 await page.evaluate(()=>window.scrollTo(0,200));
 const scroll=await page.evaluate(()=>window.scrollY);assert.ok(scroll>0);
 await page.getByRole('button',{name:'Ordner',exact:true}).click();await page.getByText('notiz.txt',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Ordner merken',exact:true}).click();
 await page.getByRole('button',{name:'Aktionen für notiz.txt',exact:true}).click();
 await page.getByRole('menuitem',{name:'Vorschau',exact:true}).click();await page.locator('dialog pre').waitFor();
 assert.match(await page.locator('dialog pre').innerText(),/window.previewExecuted/);assert.equal(await page.evaluate(()=>window.previewExecuted),undefined);
 await page.getByRole('button',{name:'Schließen',exact:true}).click();
 await page.getByRole('button',{name:'Zurück',exact:true}).click();await page.getByText('Urlaub.JPG',{exact:true}).waitFor();
 // The back button can scroll into view before navigation; verify the saved location is restored.
 await page.waitForFunction(()=>window.scrollY>0);
 await page.getByRole('button',{name:'Vorwärts',exact:true}).click();await page.getByText('notiz.txt',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Eine Ebene höher',exact:true}).click();await page.getByText('Urlaub.JPG',{exact:true}).waitFor();
 await page.reload();await page.locator('.favorite-row').getByRole('button',{name:'Ordner',exact:true}).click();await page.getByText('notiz.txt',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Eine Ebene höher',exact:true}).click();await page.getByText('Urlaub.JPG',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Aktionen für Urlaub.JPG',exact:true}).click();await page.getByRole('menuitem',{name:'Vorschau',exact:true}).click();
 await page.waitForFunction(()=>document.querySelector('dialog img')?.naturalWidth>0);await page.keyboard.press('Escape');await page.clock.runFor(50);await page.getByRole('dialog').waitFor({state:'hidden'});
 await page.setViewportSize({width:1440,height:1000});

 const search=page.getByRole('searchbox',{name:'In diesem Ordner suchen',exact:true});
 await search.fill('DATEI');
 assert.deepEqual(await page.locator('.row:not(.header) .entry').allTextContents(),['Datei2.txt','Datei10.txt']);
 await page.getByRole('button',{name:'Name, aufsteigend sortiert',exact:true}).click();
 assert.deepEqual(await page.locator('.row:not(.header) .entry').allTextContents(),['Datei10.txt','Datei2.txt']);
 await page.getByText('Datei10.txt',{exact:true}).click();
 await page.getByText('Datei2.txt',{exact:true}).click({modifiers:['Shift']});
 assert.equal(await page.locator('.row:not(.header) input:checked').count(),2);
 await page.getByRole('checkbox',{name:'Alle auswählen',exact:true}).check();
 assert.equal(await page.locator('.row:not(.header) input:checked').count(),2);
 await page.getByRole('checkbox',{name:'Alle auswählen',exact:true}).uncheck();
 await page.getByRole('checkbox',{name:/Datei10.txt/}).check();
 await page.getByRole('checkbox',{name:/Datei2.txt/}).click({modifiers:['Shift']});
 assert.equal(await page.locator('.row:not(.header) input:checked').count(),2);
 await search.fill('Urlaub');
 assert.equal(await page.locator('.row:not(.header) input:checked').count(),0);
 await search.fill('nothing-matches');await page.getByText('Keine passenden Einträge in diesem Ordner.',{exact:true}).waitFor();
 await search.fill('');

 await page.getByRole('button',{name:'Papierkorb',exact:true}).click();
 await page.getByText('Der Papierkorb ist leer.',{exact:true}).waitFor();
 await page.getByRole('button',{name:'Zurück zu den Dateien',exact:true}).click();
 async function openDelete(){
  await page.getByRole('checkbox',{name:/Urlaub.JPG/}).check();
  await page.getByRole('button',{name:'Löschen',exact:true}).click();
  await page.getByRole('dialog').waitFor();
 }
 await openDelete();
 await page.getByRole('button',{name:'In den Papierkorb',exact:true}).click();
 await page.getByRole('dialog').waitFor({state:'hidden'});
 assert.deepEqual(deletions.pop(),{root:'share/Backup',paths:['Urlaub.JPG'],permanent:false});
 await openDelete();
 await page.locator('dialog select').selectOption({label:'Endgültig löschen'});
 const destroy=page.getByRole('button',{name:'Endgültig löschen',exact:true});
 assert.equal(await destroy.isDisabled(),true);
 await page.keyboard.press('Enter');assert.equal(deletions.length,0);
 await page.getByRole('checkbox',{name:'Ja, endgültig löschen',exact:true}).check();
 assert.equal(await destroy.isEnabled(),true);
 await page.screenshot({path:out+'/delete-confirmation.png',fullPage:true});
 await destroy.click();await page.getByRole('dialog').waitFor({state:'hidden'});
 assert.deepEqual(deletions.pop(),{root:'share/Backup',paths:['Urlaub.JPG'],permanent:true});

 // Account persistence must survive a completely new browser context.
 await page.getByRole('button',{name:'Große Symbole',exact:true}).click();
 await page.getByText('Einstellungen werden im Benutzerkonto gespeichert …',{exact:true}).waitFor({state:'hidden'});
 await page.waitForFunction(()=>document.querySelector('.file-entries img')?.naturalWidth>0);
 await page.screenshot({path:out+'/large-icons.png',fullPage:true});
 const second=await context();await second.page.goto(base+'/files');
 await second.page.locator('.file-entries.large').waitFor();
 assert.equal(await second.page.getByRole('button',{name:'Name, absteigend sortiert',exact:true}).count(),1);
 assert.equal(await second.page.locator('.favorite-row').count(),1);
 await second.ctx.close();
 failPreferences=true;
 await page.getByRole('button',{name:'Kleine Symbole',exact:true}).click();
 await page.getByText('Die Änderungen sind noch nicht im Benutzerkonto gespeichert. Bitte erneut versuchen.',{exact:false}).waitFor();
 failPreferences=false;await page.getByRole('button',{name:'Erneut versuchen',exact:true}).click();
 await page.waitForFunction(()=>!document.querySelector('[role="alert"]'));
 await page.screenshot({path:out+'/small-icons.png',fullPage:true});
 await page.getByRole('button',{name:'Details',exact:true}).click();
 await page.getByRole('checkbox',{name:/Datei10.txt/}).check();
 await page.getByRole('checkbox',{name:/Datei2.txt/}).check();
 await page.getByText('Datei2.txt',{exact:true}).click({button:'right'});
 assert.equal(await page.locator('.row:not(.header) input:checked').count(),2);
 await page.getByRole('menu').waitFor();assert.equal(await page.getByRole('menu').getByRole('menuitem',{name:'Umbenennen',exact:true}).count(),0);
 await page.keyboard.press('Escape');await page.getByRole('menu').waitFor({state:'hidden'});await page.keyboard.press('Control+c');
 await page.getByRole('button',{name:'Ordner',exact:true}).last().click();
 await page.getByText('notiz.txt',{exact:true}).waitFor();
 await page.keyboard.press('Control+v');await page.getByRole('dialog').waitFor();
 await page.getByRole('button',{name:'Hierher kopieren',exact:true}).click();
 await page.getByRole('dialog').waitFor({state:'hidden'});
 assert.deepEqual(copies.pop(),{root:'share/Backup',paths:['Datei10.txt','Datei2.txt'],toRoot:'share/Backup',toDir:'Ordner',move:false,conflict:'rename'});
 await page.locator('.file-entries').click({button:'right',position:{x:10,y:110}});
 await page.getByRole('menu',{name:'Aktionen im aktuellen Ordner'}).waitFor();
 await page.getByRole('menu').getByRole('menuitem',{name:'Neuer Ordner',exact:true}).click();
 await page.getByRole('dialog').getByRole('textbox').waitFor();await page.keyboard.press('Escape');await page.clock.runFor(50);await page.getByRole('dialog').waitFor({state:'hidden'});
 await (await pathField(page)).fill('/');
 await page.getByRole('button',{name:'Öffnen',exact:true}).click();await page.getByText('Urlaub.JPG',{exact:true}).waitFor();
 await (await pathField(page)).fill('../outside');
 await page.getByRole('button',{name:'Öffnen',exact:true}).click();await page.getByText('Bitte einen Ordnerpfad ohne . oder .. eingeben.',{exact:true}).waitFor();
 // Text inputs keep normal select-all/copy/editing semantics.
 await (await pathField(page)).focus();await page.keyboard.press('Control+a');assert.equal(await page.locator('.row:not(.header) input:checked').count(),0);
 await page.getByRole('checkbox',{name:/Datei2.txt/}).check();await page.keyboard.press('F2');await page.getByRole('dialog').getByRole('textbox').waitFor();await page.keyboard.press('Escape');await page.clock.runFor(50);await page.getByRole('dialog').waitFor({state:'hidden'});
 await page.keyboard.press('Delete');await page.getByRole('dialog').waitFor();assert.equal(deletions.length,0);await page.keyboard.press('Escape');await page.clock.runFor(50);await page.getByRole('dialog').waitFor({state:'hidden'});
 await page.keyboard.press('Control+x');
 await (await pathField(page)).fill('/Ordner');await page.getByRole('button',{name:'Öffnen',exact:true}).click();await page.getByText('notiz.txt',{exact:true}).waitFor();
 await page.keyboard.press('Control+v');await page.getByRole('dialog').waitFor();await page.getByRole('button',{name:'Hierher verschieben',exact:true}).click();await page.getByRole('dialog').waitFor({state:'hidden'});
 assert.deepEqual(copies.pop(),{root:'share/Backup',paths:['Datei2.txt'],toRoot:'share/Backup',toDir:'Ordner',move:true,conflict:'rename'});
 assert.equal(await page.getByRole('button',{name:'Einfügen …',exact:true}).count(),0);
 await page.locator('details.advanced summary').click();
 await page.getByRole('button',{name:'backup',exact:true}).click();
 await page.getByText('Datei2.txt',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'Papierkorb',exact:true}).count(),0);
 await page.screenshot({path:out+'/volume-trash.png',fullPage:true});await ctx.close();
 const usbView=await context();await usbView.page.goto(base+'/files');
 await usbView.page.getByText('Laufwerk nicht angeschlossen',{exact:true}).waitFor();
 assert.equal(await usbView.page.getByRole('button',{name:'USB-Sicherung',exact:true}).isDisabled(),true);
 assert.equal(usbMounts.length,0);
 usbConnected=true;await usbView.page.getByRole('button',{name:'USB-Backups aktualisieren',exact:true}).click();
 await usbView.page.getByRole('button',{name:'USB-Sicherung',exact:true}).click();
 await usbView.page.getByText('USB-Inhalt.txt',{exact:true}).waitFor();
 assert.equal(usbMounts.length,1);assert.ok(usbListings.includes('Sicherungen/Backup'));
 assert.equal(await shownPath(usbView.page),'/Sicherungen/Backup');
 await usbView.page.getByText('USB-Backup: geschützt durchsehen',{exact:true}).waitFor();
 await usbView.page.getByRole('checkbox',{name:/USB-Inhalt.txt/}).check();await usbView.page.keyboard.press('Delete');
 assert.equal(await usbView.page.getByRole('dialog').count(),0);
 assert.equal(await usbView.page.getByRole('button',{name:'Löschen',exact:true}).count(),0);
 await usbView.page.getByRole('button',{name:'Änderungen freischalten',exact:true}).click();
 await usbView.page.getByRole('dialog').getByRole('button',{name:'Änderungen freischalten',exact:true}).click();
 await usbView.page.getByRole('button',{name:'Löschen',exact:true}).waitFor();
 await usbView.page.getByRole('button',{name:'Wieder schützen',exact:true}).click();
 assert.equal(await usbView.page.getByRole('button',{name:'Löschen',exact:true}).count(),0);


 await usbView.page.getByText('Zuletzt erfolgreich:',{exact:false}).waitFor();
 await usbView.page.getByText('1 GB frei',{exact:true}).waitFor();
 usbBackup.state.running=true;await usbView.page.clock.runFor(5100);
 await usbView.page.getByText('Sicherung läuft',{exact:true}).waitFor();
 assert.equal(await usbView.page.getByRole('button',{name:'Backup wiederherstellen …',exact:true}).isDisabled(),true);
 assert.equal(await usbView.page.getByRole('progressbar',{name:'Sicherung läuft'}).getAttribute('value'),'25');
 usbBackup.state.running=false;await usbView.page.clock.runFor(5100);
 await usbView.page.getByRole('button',{name:'Backup wiederherstellen …',exact:true}).click();
 await usbView.page.getByRole('dialog',{name:'Aus USB-Backup wiederherstellen'}).waitFor();
 await usbView.page.getByLabel('Neuer Wiederherstellungsordner').fill('Restore-Backup');
 rejectRestoreFolder=true;const beforeCopies=copies.length;
 await usbView.page.getByRole('button',{name:'Wiederherstellung starten',exact:true}).click();
 await usbView.page.locator('dialog [role="alert"]').waitFor();assert.equal(copies.length,beforeCopies);
 rejectRestoreFolder=false;await usbView.page.getByLabel('Neuer Wiederherstellungsordner').fill('Restore-Backup-Neu');
 await usbView.page.getByRole('button',{name:'Wiederherstellung starten',exact:true}).click();await usbView.page.getByRole('dialog').waitFor({state:'hidden'});
 assert.deepEqual(restoreFolders.at(-1),{root:'share/Backup',path:'Restore-Backup-Neu'});
 assert.deepEqual(copies.pop(),{root:'usb/Backup-abc123',paths:['Sicherungen/Backup'],toRoot:'share/Backup',toDir:'Restore-Backup-Neu',move:false,conflict:'rename'});
 await usbView.page.getByRole('checkbox',{name:/USB-Inhalt.txt/}).check();
 await usbView.page.getByRole('button',{name:'Auswahl wiederherstellen …',exact:true}).click();
 await usbView.page.getByLabel('Neuer Wiederherstellungsordner').fill('Restore-Auswahl');
 await usbView.page.getByRole('button',{name:'Wiederherstellung starten',exact:true}).click();await usbView.page.getByRole('dialog').waitFor({state:'hidden'});
 assert.deepEqual(copies.pop(),{root:'usb/Backup-abc123',paths:['Sicherungen/Backup/USB-Inhalt.txt'],toRoot:'share/Backup',toDir:'Restore-Auswahl',move:false,conflict:'rename'});
 await usbView.page.screenshot({path:out+'/usb-backup-sidebar.png',fullPage:true});
 usbFolderMissing=true;await usbView.page.getByRole('button',{name:'USB-Sicherung',exact:true}).click();
 await usbView.page.getByText('Der Backup-Ordner ist noch nicht vorhanden.',{exact:false}).waitFor();
 await usbView.ctx.close();
 recycleEnabled=false;
 const user=await context('user','hell',390);await user.page.goto(base+'/files');await user.page.getByText('Urlaub.JPG',{exact:true}).waitFor();
 assert.equal(await user.page.locator('details.advanced').count(),0);
 assert.equal(await user.page.getByText('USB-Backups',{exact:true}).count(),0);
 await user.page.getByRole('button',{name:'Aktionen für Datei2.txt',exact:true}).click();
 await user.page.getByRole('menu').waitFor();assert.equal(await user.page.getByRole('menuitem',{name:'Löschen',exact:true}).count(),0);
 assert.equal(await user.page.getByRole('menuitem',{name:'Umbenennen',exact:true}).count(),0);
 await user.page.getByRole('menuitem',{name:'Vorschau',exact:true}).click();await user.page.locator('dialog pre').waitFor();await user.page.keyboard.press('Escape');await user.page.clock.runFor(50);await user.page.getByRole('dialog').waitFor({state:'hidden'});
 assert.ok(await user.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));


 assert.equal(await user.page.getByRole('button',{name:'Details',exact:true}).getAttribute('aria-pressed'),'true');
 assert.equal(await user.page.locator('.favorite-row').count(),0);
 await user.page.getByRole('checkbox',{name:/Datei2.txt/}).check();await user.page.keyboard.press('F2');await user.page.keyboard.press('Delete');
 assert.equal(await user.page.getByRole('dialog').count(),0);
 for(const mode of ['Kleine Symbole','Große Symbole']){
  await user.page.getByRole('button',{name:mode,exact:true}).click();
  assert.ok(await user.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 }
 await user.page.screenshot({path:out+'/large-icons-mobile.png',fullPage:true});
 await user.page.getByRole('button',{name:'Papierkorb',exact:true}).click();
 await user.page.getByText('Für diese Freigabe ist der Papierkorb ausgeschaltet.',{exact:false}).waitFor();
 assert.equal(await user.page.getByRole('link',{name:'Freigaben verwalten'}).count(),0);
 await user.page.screenshot({path:out+'/disabled-trash-mobile.png',fullPage:true});await user.ctx.close();

 hiddenFixture=true;const feature=await context();const fp=feature.page;await fp.goto(base+'/files');
 await fp.getByRole('button',{name:'Backup',exact:true}).click();await fp.getByText('Datei2.txt',{exact:true}).waitFor();
 assert.equal(await fp.getByText('.env',{exact:true}).count(),0);
 await fp.getByRole('button',{name:'Ansicht',exact:true}).click();await fp.getByLabel('Versteckte Dateien anzeigen',{exact:true}).check();await fp.getByText('.env',{exact:true}).waitFor();
 await fp.getByText('Einstellungen werden im Benutzerkonto gespeichert …',{exact:true}).waitFor({state:'hidden'});
 await fp.reload();await fp.getByText('.env',{exact:true}).waitFor();
 await fp.getByRole('checkbox',{name:'Ordner auswählen',exact:true}).check();
 await fp.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();
 await fp.getByRole('button',{name:'Ordnergrößen berechnen',exact:true}).click();
 await fp.locator('.actions .count').filter({hasText:'123 B'}).waitFor();
 await fp.getByRole('checkbox',{name:'Ordner auswählen',exact:true}).uncheck();
 conflictFixture=true;const initialCopies=copies.length;
 await fp.getByRole('button',{name:'Kopieren nach …',exact:true}).click();
 await fp.getByLabel('Gibt es den Namen schon',{exact:true}).selectOption('replace');
 await fp.getByRole('columnheader',{name:'Vorhandene Datei',exact:true}).waitFor();await fp.screenshot({path:out+'/conflict-comparison.png',fullPage:true});assert.equal(copies.length,initialCopies);
 await fp.locator('dialog select').last().selectOption('replace');
 assert.equal(await fp.getByRole('button',{name:'Hierher kopieren',exact:true}).isDisabled(),true);
 await fp.getByLabel('Ich bestätige das Ersetzen vorhandener Dateien.',{exact:true}).check();
 await fp.getByRole('button',{name:'Hierher kopieren',exact:true}).click();await fp.getByRole('dialog').waitFor({state:'hidden'});
 assert.equal(copies.at(-1).conflict,'replace');conflictFixture=false;
 jobFinished=true;await fp.clock.runFor(1200);await fp.getByText('Dateiauftrag abgeschlossen',{exact:true}).waitFor();
 await fp.locator('.notice').getByRole('button',{name:'Zielordner öffnen',exact:true}).click();
 await fp.waitForFunction(()=>[...document.querySelectorAll('.crumbs button:not(.edit-path)')].map(b=>b.textContent).join('/')==='backup/backup');
 await feature.ctx.close();

 // The five additional features, using isolated API fixtures.
 newFeatures=true;usbFolderMissing=false;recycleEnabled=true;filePrefs.owner={};usbMounted=true;usbConnected=true;
 const next=await context();const np=next.page;await np.goto(base+'/files');await np.getByText('Datei2.txt',{exact:true}).waitFor();
 await np.getByRole('button',{name:'Offene Dateien erneut versuchen',exact:true}).click();assert.equal(retries.length,1);
 await np.getByRole('button',{name:'Aktionen für Datei2.txt',exact:true}).click();await np.getByRole('menuitem',{name:'Eigenschaften',exact:true}).click();
 await np.getByText('-rw-rw---- · UID 1000 · GID 1000',{exact:true}).waitFor();
 await np.getByRole('button',{name:'SHA-256 berechnen',exact:true}).click();await np.locator('dialog textarea').waitFor();
 await np.getByLabel('Vergleichswert (SHA-256)',{exact:true}).fill('a'.repeat(64));await np.getByText('Die Prüfsummen stimmen überein.',{exact:true}).waitFor();
 await np.getByLabel('Vergleichswert (SHA-256)',{exact:true}).fill('b'.repeat(64));await np.getByText('Die Prüfsummen unterscheiden sich.',{exact:true}).waitFor();
 await np.screenshot({path:out+'/properties-checksum.png',fullPage:true});await np.getByRole('button',{name:'Schließen',exact:true}).click();
 await np.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();
 const secondary=np.getByRole('region',{name:'Rechter Ordner',exact:true});
 await secondary.getByRole('combobox').selectOption('volume/backup');await secondary.getByText('Datei2.txt',{exact:true}).waitFor();
 await np.getByRole('button',{name:'Nach rechts kopieren →',exact:true}).click();await np.getByRole('dialog').waitFor();
 assert.equal(await np.locator('dialog select').first().inputValue(),'volume/backup');
 await np.getByRole('button',{name:'Hierher kopieren',exact:true}).click();await np.getByRole('dialog').waitFor({state:'hidden'});
 assert.equal(copies.at(-1).toRoot,'volume/backup');
 await secondary.getByRole('checkbox',{name:'Datei10.txt auswählen',exact:true}).check();
 await np.getByRole('button',{name:'← Nach links verschieben',exact:true}).click();await np.getByRole('button',{name:'Hierher verschieben',exact:true}).click();await np.getByRole('dialog').waitFor({state:'hidden'});
 assert.equal(copies.at(-1).root,'volume/backup');assert.equal(copies.at(-1).toRoot,'share/Backup');assert.equal(copies.at(-1).move,true);
 await secondary.getByRole('combobox').selectOption('usb/Backup-abc123');await secondary.getByText('USB-Inhalt.txt',{exact:true}).waitFor();
 assert.equal(await np.getByRole('button',{name:'Nach rechts kopieren →',exact:true}).count(),0);await np.locator('.browser').getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();assert.equal(await np.getByRole('button',{name:'Nach rechts kopieren →',exact:true}).isDisabled(),true);
 await secondary.getByRole('checkbox',{name:'USB-Inhalt.txt auswählen',exact:true}).check();assert.equal(await np.getByRole('button',{name:'← Nach links verschieben',exact:true}).isDisabled(),true);
 assert.equal(await np.getByRole('button',{name:'← Nach links kopieren',exact:true}).isEnabled(),true);
 await np.screenshot({path:out+'/two-folders.png',fullPage:true});assert.ok(await np.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await np.getByText('Einstellungen werden im Benutzerkonto gespeichert …',{exact:true}).waitFor({state:'hidden'});await np.reload();await np.getByRole('region',{name:'Rechter Ordner',exact:true}).waitFor();
 await np.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();
 await np.getByRole('button',{name:'Papierkorb',exact:true}).click();await np.getByLabel('Alle gelöschten Dateien',{exact:true}).check();
 await np.getByText('Backup/Fotos/a.txt',{exact:true}).waitFor();await np.getByLabel('Sortieren nach',{exact:true}).selectOption('size');
 assert.match((await np.locator('.browser .row:not(.header) .entry').allTextContents())[0],/a.txt/);
 await np.getByRole('checkbox',{name:'a.txt auswählen',exact:true}).check();await np.getByRole('button',{name:'Wiederherstellen',exact:true}).click();
 await np.getByRole('dialog',{name:'Wiederherstellung prüfen'}).waitFor();assert.equal(trashRestores.length,0);
 await np.getByText('Name bereits vorhanden – Wiederherstellung mit Zeitstempel im Namen.',{exact:true}).waitFor();await np.screenshot({path:out+'/trash-restore-preview.png',fullPage:true});
 await np.locator('dialog').getByRole('button',{name:'Wiederherstellen',exact:true}).click();await np.getByRole('dialog').waitFor({state:'hidden'});assert.deepEqual(trashRestores.at(-1).paths,['tester/Fotos/a.txt']);
 await np.getByRole('button',{name:'Zurück zu den Dateien',exact:true}).click();
 const input=np.locator('input[type=file]:not([webkitdirectory])').first();const uploadFile={name:'Datei2.txt',mimeType:'text/plain',buffer:Buffer.from('new file')};
 await input.setInputFiles(uploadFile);await np.getByRole('dialog',{name:'Upload prüfen'}).waitFor();await np.getByText('Ziel, Speicherplatz und Namenskonflikte werden geprüft …',{exact:true}).waitFor({state:'hidden'});
 assert.equal(uploadStarts.length,0);await np.locator('dialog select').selectOption('replace');assert.equal(await np.getByRole('button',{name:'Upload starten',exact:true}).isDisabled(),true);
 await np.getByLabel('Ich bestätige das Ersetzen der ausgewählten vorhandenen Dateien.',{exact:true}).check();await np.screenshot({path:out+'/upload-review.png',fullPage:true});
 await np.getByRole('button',{name:'Upload starten',exact:true}).click();await np.getByRole('dialog').waitFor({state:'hidden'});await np.waitForTimeout(100);
 assert.equal(uploadStarts.at(-1).conflict,'replace');assert.equal(uploadStarts.at(-1).root,'share/Backup');
 freeFixture=1;await input.setInputFiles(uploadFile);await np.getByText('Für diese Auswahl ist nicht genug freier Speicher verfügbar.',{exact:true}).waitFor();assert.equal(await np.getByRole('button',{name:'Upload starten',exact:true}).isDisabled(),true);await np.locator('dialog').getByRole('button',{name:'Abbrechen',exact:true}).click();freeFixture=1e9;
 await input.setInputFiles(uploadFile);await np.locator('dialog select').waitFor();await np.locator('dialog select').selectOption('skip');assert.equal(await np.getByRole('button',{name:'Upload starten',exact:true}).isDisabled(),true);await np.locator('dialog').getByRole('button',{name:'Abbrechen',exact:true}).click();
 await next.ctx.close();

 toolsFeature=true;filePrefs.owner={};const toolsContext=await context();const tp=toolsContext.page;await tp.goto(base+'/files');await tp.getByText('Backup.zip',{exact:true}).waitFor();
 const searchBefore=searches.length;await tp.getByRole('button',{name:'Werkzeuge',exact:true}).click();await tp.getByRole('menuitem',{name:'Unterordner durchsuchen',exact:true}).click();assert.equal(searches.length,searchBefore);
 await tp.getByLabel('Dateiname enthält',{exact:true}).fill('notiz');await tp.getByLabel('Dateityp / Endung',{exact:true}).fill('txt');await tp.getByLabel('Mindestens (MiB)',{exact:true}).fill('0.00001');
 await tp.getByRole('button',{name:'Suche starten',exact:true}).click();await tp.getByText('Backup/Ordner/notiz.txt',{exact:true}).waitFor();assert.equal(searches.at(-1).query,'notiz');assert.equal(searches.at(-1).extension,'txt');assert.ok(searches.at(-1).minSize>0);
 await tp.screenshot({path:out+'/recursive-search.png',fullPage:true});await tp.locator('dialog').getByRole('button',{name:'Ordner öffnen',exact:true}).click();await tp.getByText('notiz.txt',{exact:true}).waitFor();
 await tp.getByRole('button',{name:'Eine Ebene höher',exact:true}).click();await tp.getByText('Datei2.txt',{exact:true}).waitFor();
 await tp.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();await tp.getByRole('checkbox',{name:'Datei10.txt auswählen',exact:true}).check();await tp.getByRole('button',{name:'Mehrere umbenennen',exact:true}).click();
 await tp.getByLabel('Vorne ergänzen',{exact:true}).fill('Konflikt');await tp.getByRole('button',{name:'Namensvorschau erstellen',exact:true}).click();await tp.getByRole('cell',{name:'Bereits vorhanden',exact:true}).first().waitFor();assert.equal(await tp.getByRole('button',{name:'Namen ändern',exact:true}).isDisabled(),true);
 await tp.getByLabel('Vorne ergänzen',{exact:true}).fill('Meine-');await tp.getByRole('button',{name:'Namensvorschau erstellen',exact:true}).click();await tp.getByRole('cell',{name:'Meine-Datei2.txt',exact:true}).waitFor();await tp.screenshot({path:out+'/batch-rename-preview.png',fullPage:true});
 await tp.getByRole('button',{name:'Namen ändern',exact:true}).click();await tp.getByRole('dialog').waitFor({state:'hidden'});assert.equal(batchRenames.at(-1).changes[0].to,'Meine-Datei2.txt');
 await tp.getByRole('button',{name:'Rückgängig',exact:true}).click();await tp.getByRole('button',{name:'Rückgängig',exact:true}).waitFor({state:'hidden'});assert.equal(undoCalls.at(-1),'/files/undo/undo-batch');
 await tp.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();const rightPane=tp.getByRole('region',{name:'Rechter Ordner',exact:true});await rightPane.getByRole('combobox').selectOption('volume/backup');await rightPane.getByText('Datei2.txt',{exact:true}).waitFor();
 await tp.getByRole('button',{name:'Werkzeuge',exact:true}).click();await tp.getByRole('menuitem',{name:'Ordner vergleichen',exact:true}).click();await tp.getByLabel('Dateiinhalte mit SHA-256 vergleichen',{exact:true}).check();await tp.getByRole('button',{name:'Vergleich starten',exact:true}).click();await tp.getByText('2 Unterschiede · 2 übereinstimmende Einträge',{exact:true}).waitFor();
 assert.equal(comparisons.at(-1).checksum,true);assert.equal(comparisons.at(-1).toRoot,'volume/backup');await tp.screenshot({path:out+'/folder-comparison.png',fullPage:true});await tp.locator('dialog').getByRole('button',{name:'Schließen',exact:true}).click();
 await tp.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();await tp.getByRole('button',{name:'Aktionen für Backup.zip',exact:true}).click();await tp.getByRole('menuitem',{name:'ZIP öffnen',exact:true}).click();await tp.getByRole('checkbox',{name:'Fotos/a.txt',exact:true}).waitFor();
 await tp.getByRole('checkbox',{name:'b.txt',exact:true}).uncheck();await tp.getByLabel('Neuer Ordner für den Inhalt',{exact:true}).fill('Ordner');await tp.getByRole('button',{name:'Entpackziel prüfen',exact:true}).click();await tp.getByText('Dieser Zielname ist bereits vorhanden. Bitte einen anderen Ordnernamen wählen.',{exact:true}).waitFor();assert.equal(await tp.getByRole('button',{name:'Auswahl entpacken',exact:true}).isDisabled(),true);
 await tp.getByLabel('Neuer Ordner für den Inhalt',{exact:true}).fill('Entpackt');await tp.getByRole('button',{name:'Entpackziel prüfen',exact:true}).click();await tp.screenshot({path:out+'/archive-preview.png',fullPage:true});await tp.getByRole('button',{name:'Auswahl entpacken',exact:true}).click();await tp.getByRole('dialog').waitFor({state:'hidden'});assert.deepEqual(extractions.at(-1).paths,['Fotos/a.txt']);assert.equal(extractions.at(-1).name,'Entpackt');
 await toolsContext.ctx.close();

 filePrefs.owner={};const newest=await context();const qp=newest.page;await qp.goto(base+'/files');await qp.getByText('Datei2.txt',{exact:true}).waitFor();
 await qp.getByRole('button',{name:'Pausieren',exact:true}).click();await qp.getByText('Pausiert',{exact:true}).waitFor();assert.equal(controls.at(-1).paused,true);
 await Promise.all([qp.waitForResponse(r=>r.url().endsWith('/files/jobs')&&r.status()===200),qp.getByLabel('Priorität',{exact:true}).selectOption('2')]);
 assert.equal(controls.at(-1).priority,2);assert.equal(await qp.getByLabel('Priorität',{exact:true}).inputValue(),'2');await qp.getByLabel('Limit in MiB/s',{exact:true}).fill('12');await Promise.all([qp.waitForResponse(r=>r.url().endsWith('/files/jobs')&&r.status()===200),qp.getByLabel('Limit in MiB/s',{exact:true}).press('Tab')]);
 await qp.getByRole('button',{name:'Fortsetzen',exact:true}).click();await qp.getByRole('button',{name:'Pausieren',exact:true}).waitFor();assert.equal(controls.at(-1).paused,false);assert.equal(controls.at(-1).rate,12*1048576);
 await qp.screenshot({path:out+'/transfer-controls.png',fullPage:true});
 const scans=usageCalls.length;await qp.getByRole('button',{name:'Werkzeuge',exact:true}).click();await qp.getByRole('menuitem',{name:'Speicherverbrauch analysieren',exact:true}).click();assert.equal(usageCalls.length,scans);await qp.getByRole('button',{name:'Analyse starten',exact:true}).click();await qp.locator('dialog').getByText('Ordner/notiz.txt',{exact:true}).waitFor();assert.equal(usageCalls.at(-1).root,'share/Backup');await qp.screenshot({path:out+'/storage-analysis.png',fullPage:true});await qp.locator('dialog').getByRole('button',{name:'Schließen',exact:true}).click();
 await qp.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();await qp.getByRole('checkbox',{name:'Ordner auswählen',exact:true}).check();await qp.getByRole('button',{name:'Herunterladen',exact:true}).click();await qp.getByLabel('Archivname (.zip)',{exact:true}).fill('../bad.zip');assert.equal(await qp.locator('dialog').getByRole('button',{name:'Herunterladen',exact:true}).isDisabled(),true);await qp.getByLabel('Archivname (.zip)',{exact:true}).fill('Meine-Sicherung.zip');await qp.screenshot({path:out+'/download-preview.png',fullPage:true});
 const zipDownload=qp.waitForEvent('download');await qp.locator('dialog').getByRole('button',{name:'Herunterladen',exact:true}).click();const download=await zipDownload;assert.equal(download.suggestedFilename(),'Meine-Sicherung.zip');assert.equal(new URL(download.url()).searchParams.get('name'),'Meine-Sicherung.zip');
 await qp.getByRole('checkbox',{name:'Ordner auswählen',exact:true}).uncheck();await qp.getByRole('button',{name:'Aktionen für Datei2.txt',exact:true}).click();await qp.getByRole('menuitem',{name:'Eigenschaften',exact:true}).click();await qp.getByRole('heading',{name:'Rechte im Dateimanager',exact:true}).waitFor();await qp.screenshot({path:out+'/effective-permissions.png',fullPage:true});
 externalEntry=true;await qp.clock.runFor(10100);assert.equal(await qp.getByText('Extern.txt',{exact:true}).count(),0);await qp.locator('dialog').getByRole('button',{name:'Schließen',exact:true}).click();await qp.clock.runFor(10100);await qp.getByText('Extern.txt',{exact:true}).waitFor();assert.equal(await qp.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).isChecked(),true);
 await qp.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();const pane=qp.getByRole('region',{name:'Rechter Ordner',exact:true});await pane.getByText('Extern.txt',{exact:true}).waitFor();await pane.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();externalEntry=false;await qp.getByRole('button',{name:'Werkzeuge',exact:true}).focus();await qp.clock.runFor(10100);await pane.getByText('Extern.txt',{exact:true}).waitFor({state:'hidden'});assert.equal(await pane.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).isChecked(),true);
 await newest.ctx.close();

 filePrefs.owner={};const extra=await context();const ep=extra.page;await ep.goto(base+'/files');await ep.getByText('Datei2.txt',{exact:true}).waitFor();
 await ep.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();await ep.getByRole('button',{name:'Bearbeiten',exact:true}).click();await ep.getByRole('button',{name:'Vorherige Fassung',exact:true}).click();await ep.getByRole('heading',{name:'Fassungen vergleichen',exact:true}).waitFor();assert.equal(await ep.getByRole('button',{name:'Fassung wiederherstellen',exact:true}).isDisabled(),true);
 await ep.getByRole('checkbox',{name:'Ich möchte die aktuelle Datei durch diese vorherige Fassung ersetzen.',exact:true}).check();await ep.screenshot({path:out+'/previous-version.png',fullPage:true});await ep.getByRole('button',{name:'Fassung wiederherstellen',exact:true}).click();await ep.getByText('Datei2.txt gespeichert.',{exact:true}).waitFor();assert.equal(textSaves.at(-1).content,'Vorherige Fassung');assert.equal(textSaves.at(-1).version,'current-v1');await ep.getByRole('button',{name:'Schließen',exact:true}).click();
 const dupBefore=duplicateCalls.length;await ep.getByRole('button',{name:'Werkzeuge',exact:true}).click();await ep.getByRole('menuitem',{name:'Doppelte Dateien finden',exact:true}).click();assert.equal(duplicateCalls.length,dupBefore);await ep.getByRole('button',{name:'Duplikatsuche starten',exact:true}).click();await ep.locator('dialog').getByRole('checkbox',{name:'Ordner/Kopie.txt',exact:true}).check();await ep.getByRole('button',{name:'Auswahl zum Löschen prüfen',exact:true}).click();assert.equal(await ep.getByRole('button',{name:'In den Papierkorb',exact:true}).isDisabled(),true);await ep.getByRole('checkbox',{name:'Ich bestätige das Löschen der aufgeführten Dateien.',exact:true}).check();await ep.screenshot({path:out+'/duplicate-review.png',fullPage:true});await ep.getByRole('button',{name:'In den Papierkorb',exact:true}).click();await ep.getByRole('button',{name:'Schließen',exact:true}).click();assert.deepEqual(deletions.at(-1).paths,['Ordner/Kopie.txt']);assert.equal(deletions.at(-1).versions['Ordner/Kopie.txt'],'dup-b');assert.equal(deletions.at(-1).permanent,false);
 conflictFixture=true;await ep.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();await ep.getByRole('button',{name:'Kopieren nach …',exact:true}).click();await ep.getByLabel('Gibt es den Namen schon',{exact:true}).selectOption('individual');await ep.getByLabel('Entscheidung für Datei2.txt',{exact:true}).selectOption('rename');await ep.getByLabel('Neuer Name für Datei2.txt',{exact:true}).fill('Meine-Datei.TXT');await ep.screenshot({path:out+'/individual-conflict.png',fullPage:true});await ep.getByRole('button',{name:'Hierher kopieren',exact:true}).click();await ep.getByRole('dialog').waitFor({state:'hidden'});assert.equal(copies.at(-1).choices['Datei2.txt'].name,'Meine-Datei.TXT');assert.equal(copies.at(-1).choices['Datei2.txt'].targetVersion,'absent');conflictFixture=false;
 await ep.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();await ep.getByRole('region',{name:'Rechter Ordner',exact:true}).getByRole('combobox').selectOption('volume/backup');await ep.getByRole('button',{name:'Werkzeuge',exact:true}).click();await ep.getByRole('menuitem',{name:'Ordner vergleichen',exact:true}).click();await ep.getByRole('button',{name:'Vergleich starten',exact:true}).click();await ep.locator('dialog').getByRole('checkbox',{name:'Ordner/notiz.txt',exact:true}).check();await ep.locator('dialog').getByRole('checkbox',{name:'Datei2.txt',exact:true}).check();await ep.getByRole('button',{name:'Auswahl zum Kopieren prüfen',exact:true}).click();assert.equal(await ep.getByRole('button',{name:'Ausgewählte Dateien kopieren',exact:true}).isDisabled(),true);await ep.getByRole('checkbox',{name:'Ich bestätige das Ersetzen vorhandener Dateien.',exact:true}).check();await ep.screenshot({path:out+'/comparison-copy-review.png',fullPage:true});await ep.getByRole('button',{name:'Ausgewählte Dateien kopieren',exact:true}).click();await ep.getByRole('dialog').waitFor({state:'hidden'});assert.equal(copies.at(-1).choices['Ordner/notiz.txt'].target,'Ordner/notiz.txt');assert.equal(copies.at(-1).choices['Datei2.txt'].targetVersion,'right-v1');
 batchUpload=true;const uploadHeld=new Promise(resolve=>onUploadHeld=resolve);
 const folderUpload=resolvePath(out,'queue-folder');await mkdir(folderUpload,{recursive:true});await writeFile(folderUpload+'/first.txt',Buffer.alloc(9*1024*1024));await writeFile(folderUpload+'/second.txt','x');
 await ep.locator('input[webkitdirectory]').setInputFiles(folderUpload);await ep.getByRole('dialog',{name:'Upload prüfen',exact:true}).waitFor();await ep.getByRole('button',{name:'Upload starten',exact:true}).click();await ep.getByRole('dialog').waitFor({state:'hidden'});
 await uploadHeld;await ep.getByRole('button',{name:'Alle Uploads pausieren',exact:true}).click();assert.equal(Object.keys(uploadData).length,1);releaseUpload();await ep.getByRole('button',{name:'Alle Uploads fortsetzen',exact:true}).click();await ep.locator('.panel .item .state.error').waitFor();await ep.screenshot({path:out+'/upload-queue.png',fullPage:true});await ep.getByRole('button',{name:'Fehlgeschlagene wiederholen',exact:true}).click();await ep.locator('.panel .item .state.ok').nth(1).waitFor();assert.equal(Object.keys(uploadData).length,2);assert.deepEqual(batchFinishes,['batch-0','batch-1']);assert.equal(batchChunks.filter(c=>c.id==='batch-0').length,Math.ceil(uploadData['batch-0'].size/(8*1024*1024)));assert.equal(uploadData['batch-0'].dir,'queue-folder');

 await extra.ctx.close();batchUpload=false;

 {
 // New navigation/drag/virtualization/diff checks against the same isolated fixture.
 filePrefs.owner={};const latest=await context();const np=latest.page;await np.goto(base+'/files');await np.getByText('Datei2.txt',{exact:true}).waitFor();
 assert.equal(await np.getByRole('menuitem',{name:'Speicherverbrauch analysieren',exact:true}).isVisible(),false);
 await np.getByRole('button',{name:'Werkzeuge',exact:true}).click();await np.getByRole('menuitem',{name:'Speicherverbrauch analysieren',exact:true}).waitFor();await np.getByRole('button',{name:'Werkzeuge',exact:true}).click();
 const beforeExpand=requests.filter(r=>r[2]==='/files/list').length;
 await np.getByRole('button',{name:'„Backup“ aufklappen',exact:true}).click();await np.locator('nav.places').getByRole('button',{name:'Ordner',exact:true}).waitFor();assert.equal(requests.filter(r=>r[2]==='/files/list').length,beforeExpand+1);
 assert.equal(await shownPath(np),'/');
 await np.getByRole('button',{name:'„Ordner“ aufklappen',exact:true}).click();await np.getByText('Keine Unterordner',{exact:true}).waitFor();assert.equal(await shownPath(np),'/');
 await np.locator('nav.places').getByRole('button',{name:'Ordner',exact:true}).click();await np.getByText('notiz.txt',{exact:true}).waitFor();await np.getByRole('button',{name:'Eine Ebene höher',exact:true}).click();await np.locator('[data-file-name="Datei2.txt"]').waitFor();
 await np.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();const targetPane=np.getByRole('region',{name:'Rechter Ordner',exact:true});await targetPane.getByRole('combobox').selectOption('volume/backup');await targetPane.getByText('Datei2.txt',{exact:true}).waitFor();
 await np.locator('.browser').getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();await np.locator('.browser').getByRole('checkbox',{name:'Datei10.txt auswählen',exact:true}).check();
 async function dragTo(source,target){const data=await np.evaluateHandle(()=>new DataTransfer());await source.dispatchEvent('dragstart',{dataTransfer:data});await target.dispatchEvent('dragover',{dataTransfer:data});await target.dispatchEvent('drop',{dataTransfer:data});await source.dispatchEvent('dragend',{dataTransfer:data});await data.dispose();}
 const copyCount=copies.length;
 await dragTo(np.locator('[data-file-name="Datei2.txt"]'),targetPane);
 await np.getByRole('dialog',{name:'Gezogene Dateien übernehmen',exact:true}).waitFor();assert.equal(copies.length,copyCount);
 await np.screenshot({path:out+'/drag-transfer.png',fullPage:true});await np.locator('dialog').getByRole('button',{name:'Kopieren nach …',exact:true}).click();assert.equal(copies.length,copyCount);
 await np.getByRole('button',{name:'Hierher kopieren',exact:true}).click();await np.getByRole('dialog').waitFor({state:'hidden'});assert.deepEqual(copies.at(-1).paths,['Datei2.txt','Datei10.txt']);assert.equal(copies.at(-1).toRoot,'volume/backup');assert.equal(copies.at(-1).move,false);
 await dragTo(targetPane.locator('.entry-row').filter({hasText:'Datei2.txt'}),np.locator('[data-file-name="Ordner"]'));await np.getByRole('dialog',{name:'Gezogene Dateien übernehmen',exact:true}).waitFor();await np.locator('dialog').getByRole('button',{name:'Abbrechen',exact:true}).click();assert.equal(copies.length,copyCount+1);
 await targetPane.getByRole('combobox').selectOption('usb/Backup-abc123');await targetPane.getByText('USB-Inhalt.txt',{exact:true}).waitFor();await dragTo(np.locator('[data-file-name="Datei2.txt"]'),targetPane);assert.equal(await np.getByRole('dialog').count(),0);assert.equal(copies.length,copyCount+1);
 await np.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();
 textContent=Array.from({length:100},(_,i)=>'Zeile '+i+' '+('lang '.repeat(30))).join('\n');previousContent=textContent.replace('Zeile 3 ','Alt 3 ');textVersion='diff-test';
 await np.locator('.browser').getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();await np.getByRole('button',{name:'Bearbeiten',exact:true}).click();await np.getByRole('button',{name:'Vorherige Fassung',exact:true}).click();await np.locator('dialog [data-kind="changed"]').first().waitFor();assert.equal(await np.locator('dialog .line[data-kind="changed"]').count(),2);
 await np.locator('dialog .scroll').first().evaluate(el=>{el.scrollTop=800;el.scrollLeft=120;el.dispatchEvent(new Event('scroll'));});await np.clock.runFor(50);assert.deepEqual(await np.locator('dialog .scroll').evaluateAll(els=>els.map(e=>[e.scrollTop,e.scrollLeft])),[[800,120],[800,120]]);await np.locator('dialog .scroll').first().evaluate(el=>{el.scrollTop=0;el.scrollLeft=0;el.dispatchEvent(new Event('scroll'));});await np.clock.runFor(50);await np.screenshot({path:out+'/text-line-diff.png',fullPage:true});await np.getByRole('button',{name:'Schließen',exact:true}).click();
 largeFolder=true;await np.reload();await np.locator('[data-file-name="Datei0.txt"]').waitFor();assert.ok(await np.locator('[data-file-name]').count()<100);
 await np.getByRole('checkbox',{name:'Alle auswählen',exact:true}).check();await np.getByText(/5000 ausgewählt/).waitFor();
 const virtual=np.locator('.file-entries .viewport');await virtual.evaluate(el=>{el.scrollTop=el.scrollHeight;el.dispatchEvent(new Event('scroll'));});await np.locator('[data-file-name="Datei4999.txt"]').waitFor();assert.equal(await np.getByRole('checkbox',{name:'Datei4999.txt auswählen',exact:true}).isChecked(),true);
 await np.getByRole('searchbox',{name:'In diesem Ordner suchen',exact:true}).fill('Datei4999');assert.equal(await np.locator('[data-file-name]').count(),1);
 await np.getByRole('searchbox',{name:'In diesem Ordner suchen',exact:true}).fill('');await np.getByRole('button',{name:'Große Symbole',exact:true}).click();await np.locator('.file-entries.large').waitFor();assert.ok(await np.locator('[data-file-name]').count()<100);await np.screenshot({path:out+'/large-folder-virtual.png',fullPage:true});
 await latest.ctx.close();largeFolder=false;
 }

 {
 mediaFeatures=true;filePrefs.owner={};const media=await context();const mp=media.page;await mp.goto(base+'/files');await mp.locator('[data-file-name="Urlaub.JPG"]').waitFor();
 await mp.locator('[data-file-name="Urlaub.JPG"]').focus();await mp.locator('.browser').getByRole('checkbox',{name:'Urlaub.JPG auswählen',exact:true}).check();await mp.locator('[data-file-name="Urlaub.JPG"]').focus();await mp.keyboard.press('Space');await mp.locator('dialog img').waitFor();await mp.keyboard.press('ArrowRight');await mp.getByRole('dialog',{name:'Vorschau: Zweites.JPG',exact:true}).waitFor();await mp.keyboard.press('ArrowLeft');await mp.locator('dialog img[alt="Urlaub.JPG"]').waitFor();await mp.getByRole('button',{name:'Schließen',exact:true}).click();
 for(const [name,selector] of [['Dokument.pdf','object[type="application/pdf"]'],['Ton.wav','audio']]){await mp.getByRole('button',{name:'Aktionen für '+name,exact:true}).click();await mp.getByRole('menuitem',{name:'Vorschau',exact:true}).click();await mp.locator('dialog '+selector).waitFor();if(name==='Ton.wav'){assert.equal(await mp.locator('audio').evaluate(el=>el.autoplay),false);assert.match(await mp.locator('audio').getAttribute('src'),/media=1/);}await mp.getByRole('button',{name:'Schließen',exact:true}).click();}
 await mp.getByRole('button',{name:'Aktionen für Film.mp4',exact:true}).click();await mp.getByRole('menuitem',{name:'Vorschau',exact:true}).click();await mp.getByText('Keine Vorschau verfügbar.',{exact:false}).waitFor();await mp.getByRole('button',{name:'Schließen',exact:true}).click();
 await mp.locator('[data-file-name="Ordner"]').focus();await mp.keyboard.press('Home');await mp.keyboard.press('Enter');await mp.getByText('notiz.txt',{exact:true}).waitFor();await mp.getByRole('button',{name:'Eine Ebene höher',exact:true}).click();await mp.locator('[data-file-name="Ordner"]').focus();await mp.keyboard.press('Home');await mp.keyboard.press('Shift+ArrowDown');assert.equal(await mp.locator('.browser .row input:checked').count(),2);
 await mp.keyboard.press('F1');await mp.getByRole('dialog',{name:'Tastenkürzel',exact:true}).waitFor();await mp.screenshot({path:out+'/keyboard-help.png',fullPage:true});await mp.getByRole('button',{name:'Schließen',exact:true}).click();

 await mp.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();const rPane=mp.getByRole('region',{name:'Rechter Ordner',exact:true});await rPane.getByRole('checkbox',{name:'Ordner auswählen',exact:true}).focus();await mp.keyboard.press('Home');await mp.keyboard.press('Enter');await rPane.getByText('notiz.txt',{exact:true}).waitFor();await rPane.getByRole('checkbox',{name:'notiz.txt auswählen',exact:true}).focus();await mp.keyboard.press('ArrowDown');await mp.keyboard.press('Space');await mp.locator('dialog pre').waitFor();await mp.getByRole('button',{name:'Schließen',exact:true}).click();await mp.getByRole('button',{name:'Zwei Ordner anzeigen',exact:true}).click();
 await mp.clock.runFor(1000);await mp.getByText('10 B / 30 B',{exact:true}).first().waitFor();progressCopied=20;await mp.clock.runFor(1000);await mp.getByText(/Noch ungefähr 1 Min/).waitFor();await mp.screenshot({path:out+'/transfer-estimate.png',fullPage:true});retryFailed=true;await mp.clock.runFor(1100);await mp.getByText('Die Verbindung wurde unterbrochen oder hat nicht rechtzeitig geantwortet.',{exact:true}).waitFor();retryFailed=false;
 await mp.getByRole('button',{name:'Papierkorb',exact:true}).click();await mp.getByRole('checkbox',{name:'Alle gelöschten Dateien',exact:true}).check();await mp.getByText(/Belegte Datenblöcke der Dateien/).waitFor();await mp.getByLabel('Ursprünglicher Ordner enthält',{exact:true}).fill('Archiv');await mp.getByLabel('Gelöscht bis',{exact:true}).fill('2025-12-31');assert.equal(await mp.getByRole('checkbox',{name:'a.txt auswählen',exact:true}).count(),0);await mp.getByRole('checkbox',{name:'Alle auswählen',exact:true}).check();await mp.getByRole('button',{name:'Endgültig löschen',exact:true}).click();await mp.locator('dialog').getByText('Archiv/b.txt',{exact:true}).waitFor();await mp.screenshot({path:out+'/trash-filter-delete.png',fullPage:true});await mp.locator('dialog').getByRole('button',{name:'Abbrechen',exact:true}).click();
 await media.ctx.close();mediaFeatures=false;
 }

 {
 filePrefs.owner={};const current=await context();const cp=current.page;await cp.goto(base+'/files');await cp.getByText('Datei2.txt',{exact:true}).waitFor();
 await cp.getByRole('button',{name:'Ordner merken',exact:true}).click();await cp.getByRole('button',{name:'Ordner',exact:true}).click();await cp.getByText('notiz.txt',{exact:true}).waitFor();await cp.getByRole('button',{name:'Ordner merken',exact:true}).click();await cp.getByRole('button',{name:'Ansicht',exact:true}).click();await cp.getByRole('checkbox',{name:'Ansicht für diesen Ordner merken',exact:true}).check();await cp.getByRole('button',{name:'Große Symbole',exact:true}).click();await cp.getByRole('button',{name:'Eine Ebene höher',exact:true}).click();assert.equal(await cp.getByRole('button',{name:'Details',exact:true}).getAttribute('aria-pressed'),'true');
 await cp.getByRole('button',{name:'Favoriten verwalten',exact:true}).click();const labels=cp.getByRole('dialog').getByRole('textbox');await labels.nth(0).fill('Hauptablage');await labels.nth(1).fill('Meine Notizen');await cp.getByRole('button',{name:'„Meine Notizen“ nach oben',exact:true}).click();await cp.getByRole('button',{name:'Speichern',exact:true}).click();await cp.getByText('Einstellungen werden im Benutzerkonto gespeichert …',{exact:true}).waitFor({state:'hidden'});
 await cp.reload();await cp.locator('.favorite-row').first().getByRole('button',{name:'Meine Notizen',exact:true}).click();await cp.getByText('notiz.txt',{exact:true}).waitFor();assert.equal(await cp.getByRole('button',{name:'Große Symbole',exact:true}).getAttribute('aria-pressed'),'true');
 await cp.getByRole('button',{name:'Neu',exact:true}).click();await cp.getByRole('menuitem',{name:'Neue Datei',exact:true}).click();await cp.getByRole('dialog').getByRole('textbox').fill('MeineNotiz');await cp.getByRole('dialog').getByRole('combobox').selectOption('json');await cp.getByRole('button',{name:'Erstellen und bearbeiten',exact:true}).click();await cp.getByRole('dialog',{name:'Ordner/MeineNotiz.json bearbeiten',exact:true}).waitFor();assert.deepEqual(createdFiles.at(-1),{root:'share/Backup',path:'Ordner/MeineNotiz.json'});await cp.getByRole('button',{name:'Schließen',exact:true}).click();await cp.getByRole('button',{name:'Eine Ebene höher',exact:true}).click();await cp.getByText('Datei2.txt',{exact:true}).waitFor();
 await cp.getByRole('button',{name:'Auswahl',exact:true}).click();await cp.getByRole('button',{name:'Passende Einträge auswählen',exact:true}).click();assert.equal(await cp.locator('[data-file-name="Urlaub.JPG"] input').isChecked(),true);await cp.getByRole('button',{name:'Auswahl umkehren',exact:true}).click();assert.equal(await cp.locator('[data-file-name="Urlaub.JPG"] input').isChecked(),false);assert.equal(await cp.locator('[data-file-name="Datei2.txt"] input').isChecked(),true);
 await cp.screenshot({path:out+'/files-070.png',fullPage:true});await cp.getByRole('button',{name:'Kopieren nach …',exact:true}).click();await cp.getByRole('heading',{name:'Übertragung prüfen',exact:true}).waitFor();await cp.getByText(/Freier Speicher im Ziel/).waitFor();await cp.screenshot({path:out+'/transfer-summary.png',fullPage:true});await cp.getByRole('dialog').getByRole('button',{name:'Abbrechen',exact:true}).click();await current.ctx.close();
 }

 {
 // 0.7.1: column sorting, double click, menus from the keyboard, sticky selection bar, path editing, places on phones.
 filePrefs.owner={};const fresh=await context();const p=fresh.page;await p.goto(base+'/files');await p.getByText('Datei2.txt',{exact:true}).waitFor();
 const names=()=>p.locator('.row:not(.header) .entry').allTextContents();
 await p.getByRole('button',{name:'Größe',exact:true}).click();await p.getByRole('button',{name:'Größe, aufsteigend sortiert',exact:true}).waitFor();
 let order=await names();assert.ok(order.indexOf('Datei2.txt')<order.indexOf('Datei10.txt'));
 await p.getByRole('button',{name:'Größe, aufsteigend sortiert',exact:true}).click();await p.getByRole('button',{name:'Größe, absteigend sortiert',exact:true}).waitFor();
 order=await names();assert.ok(order.indexOf('Datei10.txt')<order.indexOf('Datei2.txt'));
 await p.getByText('Einstellungen werden im Benutzerkonto gespeichert …',{exact:true}).waitFor({state:'hidden'});assert.equal(filePrefs.owner.sort,'size');assert.equal(filePrefs.owner.descending,true);
 await p.getByRole('button',{name:'Name',exact:true}).click();await p.getByRole('button',{name:'Name, aufsteigend sortiert',exact:true}).waitFor();
 await p.getByText('Urlaub.JPG',{exact:true}).dblclick();await p.locator('dialog img').waitFor();await p.keyboard.press('Escape');await p.clock.runFor(50);await p.getByRole('dialog').waitFor({state:'hidden'});
 assert.deepEqual(await p.locator('.row:not(.header) input:checked').count(),1);
 await p.getByRole('button',{name:'Ordner',exact:true}).last().dblclick();await p.getByText('notiz.txt',{exact:true}).waitFor();await p.clock.runFor(300);
 assert.equal(await p.getByRole('dialog').count(),0);assert.equal(await p.locator('.row:not(.header) input:checked').count(),0);
 await p.getByRole('button',{name:'Eine Ebene höher',exact:true}).click();await p.getByText('Datei2.txt',{exact:true}).waitFor();
 await p.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();await p.locator('[data-file-name="Datei2.txt"]').focus();
 await p.keyboard.press('Shift+F10');await p.getByRole('menu',{name:'Aktionen für Datei2.txt',exact:true}).waitFor();
 assert.equal(await p.evaluate(()=>document.activeElement?.getAttribute('role')),'menuitem');
 await p.keyboard.press('ArrowDown');await p.keyboard.press('Escape');await p.getByRole('menu').waitFor({state:'hidden'});
 assert.equal(await p.locator('.row:not(.header) input:checked').count(),1);
 await p.setViewportSize({width:1440,height:420});await p.evaluate(()=>window.scrollTo(0,0));
 const bar=await p.getByRole('region',{name:'Aktionen für die Auswahl',exact:true}).boundingBox();assert.ok(bar&&bar.y+bar.height<=420,JSON.stringify(bar));
 await p.setViewportSize({width:1440,height:1000});
 await p.locator('[data-file-name="Datei2.txt"]').focus();await p.keyboard.press('Escape');assert.equal(await p.locator('.row:not(.header) input:checked').count(),0);
 await p.getByRole('button',{name:'Pfad bearbeiten',exact:true}).click();await p.getByLabel('Ordnerpfad',{exact:true}).press('Escape');assert.equal(await p.getByLabel('Ordnerpfad',{exact:true}).count(),0);
 assert.equal(await p.getByRole('button',{name:'Einfügen …',exact:true}).count(),0);
 await p.screenshot({path:out+'/files-071.png',fullPage:true});
 await p.setViewportSize({width:390,height:900});assert.equal(await p.locator('#file-places').isVisible(),false);
 await p.getByRole('button',{name:'Speicherort: Backup',exact:true}).click();await p.locator('#file-places').waitFor();
 await p.locator('details.advanced summary').click();await p.getByRole('button',{name:'backup',exact:true}).click();
 await p.getByRole('button',{name:'Speicherort: backup',exact:true}).waitFor();assert.equal(await p.locator('#file-places').isVisible(),false);
 assert.ok(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await p.screenshot({path:out+'/files-071-mobile.png',fullPage:true});await fresh.ctx.close();
 }
 // README_SCREENSHOTS=<folder> also records the pictures of the README.
 if(process.env.README_SCREENSHOTS){
  const shots=process.env.README_SCREENSHOTS;await mkdir(shots,{recursive:true});filePrefs.owner={};
  const desk=await context('owner','graphit',1440);const d=desk.page;await d.goto(base+'/files');await d.getByText('Datei2.txt',{exact:true}).waitFor();
  await d.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();await d.getByRole('checkbox',{name:'Datei10.txt auswählen',exact:true}).check();
  await d.locator('[data-file-name="Datei10.txt"] .entry').click({button:'right'});await d.getByRole('menu').waitFor();await d.screenshot({path:shots+'/dateien.png'});
  await d.keyboard.press('Escape');await d.getByRole('button',{name:'Werkzeuge',exact:true}).click();await d.screenshot({path:shots+'/werkzeuge.png',clip:{x:260,y:150,width:1180,height:470}});await desk.ctx.close();
  const phone=await context('owner','hell',390);const m=phone.page;await m.goto(base+'/files');await m.getByText('Datei2.txt',{exact:true}).waitFor();
  await m.getByRole('checkbox',{name:'Datei2.txt auswählen',exact:true}).check();await m.screenshot({path:shots+'/dateien-mobil.png'});await phone.ctx.close();
 }
 assert.deepEqual(errors,[]);
 console.log('PASS: column sorting, double click, keyboard context menu, sticky selection bar, path editing, phone places, named/reordered account favorites, per-folder views across reload, new JSON editor, file-type/inverted selection, transfer preflight, PDF/audio preview, unsupported-video fallback, image gallery, trash filters/space/delete review, keyboard navigation in both panes and shortcut help, measured transfer ETA and readable errors, lazy folder tree, tools menu, reviewed drag transfers/USB lock, 5000-entry virtualization and full selection/search, synchronized line diff, previous-text compare/confirmed restore, duplicate scan and confirmed deletion, per-file conflict naming, selected comparison copy with versions, folder upload pause/resume/retry, transfer pause/resume/priority/rate, explicit storage analysis, ZIP download preview/name validation, effective permissions, automatic refresh with selection preservation and dialog protection, recursive search, bulk rename preview/conflicts, undo, folder checksum comparison, ZIP selection and destination conflict, remaining-file retry, properties/checksum comparison, two-pane transfers and protection, trash sorting/restore preview, upload review/replace confirmation/space check, completion/open destination, selected sizes/on-demand folder scan, persisted hidden files, conflict preview/replace confirmation, USB editing lock, USB backup last-success/free-space/live-progress, separate-folder restore and collision refusal, USB backup sidebar offline/mount/exact path/missing folder/admin-only, account preferences across fresh browser contexts, persistence retry, three views, multi-selection context menu, background context menu, copy/cut/paste targets, keyboard permissions, path validation, navigation history/scroll, range selection, per-file actions, safe image/text previews, persistent favorites, folder search, natural sorting, visible-only selection, visible recycle bin, default recycle, explicit permanent confirmation, exact request names, disabled/volume explanation, user/mobile access.');
} catch(error){console.error('Browser errors:',errors);for(const ctx of browser.contexts()){for(const page of ctx.pages()){console.error('Page:',page.url(),(await page.locator('body').innerText()).slice(-2500));await page.screenshot({path:out+'/failure.png',fullPage:true}).catch(()=>{});}}throw error;} finally {await browser.close();}
