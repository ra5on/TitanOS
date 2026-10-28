// Rendered product UI, isolated fixture data only. No requests reach a NAS.
import assert from 'node:assert/strict';
import {mkdir, writeFile} from 'node:fs/promises';
import {fixtureFactory} from './test-ui-fixtures.mjs';
const {chromium} = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({headless:true, executablePath:process.env.BROWSER_EXECUTABLE});
const base=process.env.UI_URL || 'http://127.0.0.1:5186';
const out=process.env.UI_OUTPUT || 'out/validation/ui-023';
await mkdir(out,{recursive:true});
const errors=[], evidence=[];
const context=fixtureFactory(browser,errors);
const net=(id,name,parent,address,management=false,vlan=0)=>({id,name,parent,address,management,vlan,purpose:management?'management':'apps',bridge:true,ipv4:'static',gateway:management?'192.168.1.1':'',ipv6:'auto',address6:'',gateway6:'',dns:[],routes:[]});
async function fixture(theme='graphit',width=1440,locale='de',appearance={}) {
 const f=await context('owner',theme,width,locale);
 f.data.prefs.appearance={theme,...appearance}; f.data.system.version='0.31.0';
 await f.page.setViewportSize({width,height:width<761?844:1000});
 const config={hostname:'titan-test',ports:['enp1s0'],bondMode:'',vlan:0,bridge:true,dhcp:true,address:'',gateway:'',dns:[],bonds:[],networks:[net('admin','Verwaltung','enp1s0','192.168.1.20/24',true),net('apps','Anwendungen','enp2s0','192.168.30.20/24',false,30)]};
 const network={config,pending:null,pendingSeconds:0,interfaces:[{name:'enp1s0',mac:'52:54:00:00:00:01',carrier:true,speedMbps:2500},{name:'enp2s0',mac:'52:54:00:00:00:02',carrier:true,speedMbps:2500}],device:'br0',addresses:['192.168.1.20/24'],gateway:'192.168.1.1',dns:['192.168.1.1'],configured:true,bridgeReady:true};
 const tokens=[], jobs=[];
 await f.ctx.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname.slice(7), method=route.request().method();
  const json=value=>route.fulfill({json:value});
  if(path==='/network')return json({network,bondModes:['active-backup','802.3ad']});
  if(path==='/account/tokens'){
   if(method==='POST'){const req=route.request().postDataJSON(); tokens.push({id:'fixture',...req,created:'2026-10-26T12:00:00Z'});return json({secret:'titanos_fixture_example_only',token:tokens[0]});}
   return json({tokens,scopes:['system:read','files:read','files:write','docker:read','docker:write','vms:read','vms:write']});
  }
  if(path==='/account/tokens/fixture'){tokens.splice(0);return route.fulfill({status:204});}
  if(path==='/docker/networks')return json({networks:[{name:'apps',parent:'apps',driver:'ipvlan',subnet:'192.168.30.0/24',gateway:'192.168.30.1',range:'192.168.30.128/26',hostAccess:false}],parents:config.networks});
  if(path==='/vms/imports')return json({jobs,chunkBytes:8<<20});
  if(path==='/plugins/releases')return json({releases:[{version:'0.31.0',bytes:600e6,protected:true,reason:'current',fingerprint:'current'},{version:'0.30.0',bytes:600e6,protected:true,reason:'fallback',fingerprint:'fallback'},{version:'0.28.0',bytes:600e6,protected:false,fingerprint:'old'}]});
  if(path==='/files/list')return json([{name:'Dokumente',dir:true,size:0,modified:'2026-10-26T12:00:00Z'},{name:'Jahresbericht – langer Dateiname.pdf',dir:false,size:123456,modified:'2026-10-26T12:00:00Z'},{name:'Notizen.txt',dir:false,size:128,modified:'2026-10-26T12:00:00Z'}]);
  return route.fallback();
 });
 return {...f,network,tokens,jobs};
}
async function noOverflow(page){assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'horizontal page overflow');}
try {
 for(const [theme,width,locale,appearance] of [['graphit',390,'de',{}],['hell',390,'en',{}],['graphit',390,'de',{style:'flat',font:120,rTile:0,rCtl:0}],['hell',1440,'en',{style:'outline',rTile:24,rCtl:14}],['graphit',1440,'de',{}]]){
  const {page,ctx}=await fixture(theme,width,locale,appearance);
  await page.goto(base+'/files');
  const first=page.locator('[data-file-name]').first();await first.waitFor();
  await noOverflow(page);
  if(appearance.font)assert.equal(await page.evaluate(()=>getComputedStyle(document.documentElement).getPropertyValue('--font-scale').trim()),(appearance.font/100).toFixed(2));
  if(appearance.style)assert.equal(await page.locator('html').getAttribute('data-surface'),appearance.style);
  const box=await first.boundingBox();
  if(width===390&&!appearance.font)assert.ok(box.y<=260,`first mobile entry at ${box.y}px`);
  if(width===390){
   const toolbar=page.locator('.mobile-file-actions');await toolbar.waitFor({state:'visible'});
   for(const b of await toolbar.locator(':scope > button,:scope > .dropdown > button').all())assert.ok((await b.boundingBox()).height>=44);
   await toolbar.getByRole('button',{name:locale==='de'?'Suche':'Search',exact:true}).click();
   const search=page.locator('#mobile-file-search input');await search.fill('Notizen');
   assert.equal(await page.locator('[data-file-name]').count(),1);await search.fill('');
   await page.locator('#mobile-file-search button').click();
   await page.locator('[data-file-name="Notizen.txt"] input[type=checkbox]').check();
   await page.locator('.mobile-selection').waitFor({state:'visible'});
   await page.locator('.mobile-selection button').last().click();
  }
  const filename=`files-${width}-${theme}-${appearance.font||100}.png`;
  await page.screenshot({path:out+'/'+filename,fullPage:true});
  evidence.push({page:'files',theme,width,locale,appearance,firstEntryY:box.y,screenshot:filename});
  await ctx.close();
 }
 const f=await fixture(); const {page,ctx,tokens}=f;
 await page.goto(base+'/settings/api');
 await page.getByRole('button',{name:'Token erstellen',exact:true}).click();
 let dialog=page.getByRole('dialog');
 assert.equal(await dialog.locator('input[type=checkbox]:checked').count(),1);
 await dialog.getByRole('textbox',{name:'Bezeichnung',exact:true}).fill('CLI-Test');
 await dialog.getByRole('button',{name:'Token erstellen',exact:true}).click();
 await page.getByRole('dialog',{name:'Token erstellt',exact:true}).waitFor();
 assert.deepEqual(tokens[0].scopes,['system:read']);
 await page.getByRole('button',{name:'Schließen',exact:true}).click();
 await page.getByRole('button',{name:'Widerrufen',exact:true}).click();
 await page.getByRole('dialog').getByRole('button',{name:'Widerrufen',exact:true}).click();
 assert.equal(tokens.length,0);
 await page.goto(base+'/settings/network');
 assert.equal(await page.getByRole('textbox',{name:'Name des NAS'}).inputValue(),'titan-test');
 await page.getByRole('button',{name:'Aktionen für Verwaltung',exact:true}).click();
 assert.ok(await page.getByRole('menuitem',{name:'Entfernen',exact:true}).isDisabled(),'management network removable');
 await page.getByRole('menuitem',{name:'Bearbeiten',exact:true}).click();
 await page.getByRole('dialog',{name:'Netz bearbeiten'}).waitFor();
 await page.getByRole('dialog').getByRole('button',{name:'Abbrechen',exact:true}).click();
 await page.getByRole('tab',{name:'Schnittstellen',exact:true}).click();
 await page.getByRole('cell',{name:'Verwaltung',exact:true}).waitFor();
 assert.match(page.url(),/tab=interfaces/);
 await page.getByRole('tab',{name:'Fernzugriff',exact:true}).click();
 await page.getByRole('heading',{name:/Cloudflare/}).waitFor();
 await page.goto(base+'/docker/networks');
 await page.getByRole('button',{name:'LAN-Netz anlegen',exact:true}).click();
 await page.getByRole('dialog').waitFor();
 await page.getByRole('dialog').getByRole('button',{name:'Abbrechen',exact:true}).click();
 await page.goto(base+'/vms/media');
 await page.getByRole('button',{name:'Medium importieren',exact:true}).click();
 await page.getByRole('dialog').waitFor();
 await page.getByRole('dialog').getByRole('button',{name:'Abbrechen',exact:true}).click();
 await page.goto(base+'/plugins/cleanup');
 await page.locator('input[type=checkbox]').waitFor();
 assert.equal(await page.locator('input[type=checkbox]').count(),1);
 await page.locator('input[type=checkbox]').check();
 await page.getByRole('button',{name:'Auswahl prüfen',exact:true}).click();
 assert.ok(await page.getByRole('dialog').getByRole('button',{name:'Entfernen',exact:true}).isDisabled());
 await ctx.close();
 for(const width of [390,1440])for(const theme of ['graphit','hell'])for(const path of ['/settings/network','/docker/networks','/settings/api','/vms/media','/plugins/cleanup']){
  const {page,ctx}=await fixture(theme,width);
  await page.goto(base+path); await page.locator('main').waitFor();
  await page.getByRole('heading',{level:1}).waitFor();
  await noOverflow(page);
  const file=path.slice(1).replaceAll('/','-')+`-${width}-${theme}.png`;
  await page.screenshot({path:out+'/'+file,fullPage:true});
  await ctx.close();
 }
 assert.deepEqual(errors,[]);
 await writeFile(out+'/results.json',JSON.stringify({fixtureData:true,evidence,errors},null,2)+'\n');
 console.log('PASS mobile first row, themes, font120, no overflow, scoped token defaults/revoke, network edit, LAN/media dialogs and protected plugin cleanup');
} finally {await browser.close();}
