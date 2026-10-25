// Isolated API fixtures only: never talks to a NAS or copies host files.
import assert from 'node:assert/strict';
import {fixtureFactory} from './test-ui-fixtures.mjs';
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE||'playwright');
const browser=await chromium.launch({headless:true,executablePath:process.env.BROWSER_EXECUTABLE});
const base=process.env.UI_URL||'http://127.0.0.1:5198';
const errors=[],create=fixtureFactory(browser,errors);
try{
 for(const width of [1440,390]){
  const {ctx,page}=await create('owner','graphit',width);
  const copies=[];
  await ctx.route('**/api/v1/files/list*',route=>route.fulfill({json:[{name:'Fotos',dir:true,size:0,modified:'2026-10-25T00:00:00Z'}]}));
  await ctx.route('**/api/v1/files/inspect',route=>route.fulfill({status:408,json:{error:{code:'file_scan_timeout',message:'fixture preview timeout'}}}));
  await ctx.route('**/api/v1/files/copy',route=>{
   copies.push(route.request().postDataJSON());
   return copies.length===1?route.fulfill({status:507,json:{error:{code:'no_space',message:'fixture no space'}}}):route.fulfill({json:{id:'large-copy'}});
  });
  await page.goto(base+'/files?root=share%2FDocuments');
  await page.getByRole('checkbox',{name:'Fotos auswählen',exact:true}).check();
  if(width===390){await page.getByRole('button',{name:'Weitere Aktionen',exact:true}).click();await page.getByRole('menuitem',{name:'Kopieren nach …',exact:true}).click();}
  else await page.getByRole('button',{name:'Kopieren nach …',exact:true}).click();
  const dialog=page.getByRole('dialog'),start=dialog.getByRole('button',{name:'Hierher kopieren',exact:true});
  await dialog.getByRole('combobox').first().selectOption('share/Projects');
  await dialog.getByText(/Der Umfang braucht länger/).waitFor();
  assert.equal(await start.isEnabled(),true,'preview deadline blocked the large copy');
  assert.equal(copies.length,0,'preview wrote user data');
  await start.click();
  await dialog.getByText('Auf dem Volume ist nicht genug Platz frei.',{exact:true}).waitFor();
  assert.equal(await dialog.isVisible(),true,'space error closed the dialog');
  await start.click();
  await dialog.waitFor({state:'hidden'});
  assert.equal(copies.length,2);
  assert.deepEqual(copies[0].paths,['Fotos']);
  assert.equal(copies[0].toRoot,'share/Projects');
  await ctx.close();
 }
 assert.deepEqual(errors,[]);
 console.log('Large-copy preview timeout, server-side space rejection, preserved dialog and retry passed at 1440/390 px');
}finally{await browser.close()}
