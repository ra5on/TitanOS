'use strict';
const assert=require('node:assert/strict');
const photos=require('../titan/web/photos.js');
class Node {
 constructor(attrs={}){this.attrs=attrs;this.dataset=attrs.dataset||{};this.events=new Map();this.open=false;this.disabled=false;this.isConnected=true;this._html='';this.clicked=0;this.value=attrs.value||'';}
 addEventListener(type,callback,options={}){const rows=this.events.get(type)||new Set();rows.add(callback);this.events.set(type,rows);options.signal?.addEventListener('abort',()=>rows.delete(callback),{once:true});}
 dispatch(type,details={}){const event={type,target:this,preventDefault(){this.defaultPrevented=true;},...details};for(const callback of [...(this.events.get(type)||[])])callback(event);return event;}
 closest(selector){return selector==='[data-photo-command]'&&this.dataset.photoCommand?this:null;}
 matches(selector){return this.attrs.selector===selector||selector==='input,select,textarea'&&this.attrs.tag==='select';}
 showModal(){this.open=true;}close(){this.open=false;}focus(){this.ownerDocument.activeElement=this;}click(){this.clicked++;}
 set innerHTML(value){this._html=value;if(value.includes('<form>')){this.form=new Node();this.form.ownerDocument=this.ownerDocument;this.submit=new Node();this.form.querySelector=()=>this.submit;}}
 get innerHTML(){return this._html;}
 querySelector(selector){if(selector==='form')return this.form;return this.children?.[selector]||null;}
 querySelectorAll(){return [];}
}
const fixture=()=>{
 const doc={visibilityState:'visible',activeElement:new Node()},node=new Node();node.ownerDocument=doc;
 const lightbox=new Node(),settings=new Node(),upload=new Node({selector:'[data-photo-upload]'});for(const child of [lightbox,settings,upload])child.ownerDocument=doc;
 node.children={'[data-photo-lightbox]':lightbox,'[data-photo-settings]':settings,'[data-photo-upload]':upload};node.querySelectorAll=selector=>selector==='dialog'?[lightbox,settings]:[];
 return {node,lightbox,settings,upload,click:(cmd,id)=>{const target=new Node({dataset:{photoCommand:cmd,id}});return node.dispatch('click',{target});},turn:()=>new Promise(resolve=>setImmediate(resolve))};
};
const data={libraries:[{id:'library1',name:'Familie',share:'pictures',path:'Photos',state:'ready',available:true}],albums:[{id:'album1',name:'Urlaub'}],sources:[{id:'share:pictures',name:'Fotospeicher',share:'pictures',path:''}],items:[{id:'photo1',name:'<Family>.jpg',library:'library1',library_name:'Familie',share:'pictures',path:'Photos/<Family>.jpg',size:1024,taken:1700000000,modified:1700000000,favorite:false,revision:'revision1'},{id:'photo2',name:'Second.jpg',library:'library1',library_name:'Familie',share:'pictures',path:'Photos/Second.jpg',size:3000,taken:1600000000,modified:1600000000,favorite:false,revision:'revision2'}],total:2,offset:0,limit:80,has_more:false,indexing:false};
global.window={TitanFolderPicker:{render:()=>'<div>Folder picker</div>',mount:()=>({}),disposeWithin(){}}};
global.FormData=class{constructor(form){this.values=form.values||{};}get(key){return this.values[key];}};
(async()=>{
 const html=photos.render(data,{owner:'render'});assert(html.includes('data-photos-workbench'));assert(html.includes('Bibliotheken'));assert(html.includes('Papierkorb'));assert(html.includes('&lt;Family&gt;.jpg'));assert(!html.includes('<Family>'));assert(html.includes('loading="lazy"'));assert(html.includes('/api/photos/preview?photo=photo1'));assert(!html.includes('https://'));
 assert(photos.render({...data,items:[],libraries:[],sources:[],total:0},{owner:'empty'}).includes('Deine Fotos bekommen ein Zuhause.'));
 const query=new URL('http://nas'+photos.query({library:'my library',favorite:true,search:'a&b',offset:80}));assert.equal(query.searchParams.get('search'),'a&b');assert.equal(query.searchParams.get('offset'),'80');assert.equal(query.searchParams.get('favorite'),'1');
 photos.state('one').search='private';assert.equal(photos.state('two').search,'');
 const f=fixture(),requests=[],uploads=[],notices=[];let answer=true;
 photos.mount(f.node,{data:structuredClone(data),owner:'actions',api:async(url,body)=>{requests.push({url,body});if(url==='/api/photos/upload'){uploads.push(body);return {offset:body.offset+Buffer.from(body.data,'base64').length};}return body?{ok:true}:structuredClone(data);},confirm:async()=>answer,toast:message=>notices.push(message),upload:async(file,target)=>uploads.push({file,target}),bytes:value=>value+' B'});
 f.click('open','photo1');await f.turn();assert(f.lightbox.open);assert(f.lightbox.innerHTML.includes('Original herunterladen'));assert(f.lightbox.innerHTML.includes('/api/photos/original?photo=photo1'));assert(f.lightbox.innerHTML.includes('preview=1'));assert(f.lightbox.innerHTML.includes('Papierkorb'));assert(!f.lightbox.innerHTML.includes('<Family>'));
 f.click('favorite','photo1');await f.turn();assert.deepEqual(requests.at(-1).body,{action:'favorite',photo:'photo1',value:true});assert(f.lightbox.innerHTML.includes('Favorit entfernen'));
 f.node.dispatch('keydown',{key:'ArrowRight',target:new Node()});await f.turn();assert(f.lightbox.innerHTML.includes('Second.jpg'));
 answer=false;const before=requests.length;f.click('trash','photo2');await f.turn();assert.equal(requests.length,before);assert(f.lightbox.open);
 answer=true;f.click('trash','photo2');await f.turn();assert(requests.some(row=>row.body?.action==='trash'&&row.body.photo==='photo2'));assert(!f.lightbox.open);
 f.click('favorites');await f.turn();assert.equal(new URL('http://nas'+requests.at(-1).url).searchParams.get('favorite'),'1');
 f.click('library-manage','library1');await f.turn();assert(f.settings.open);assert(f.settings.innerHTML.includes('Originaldateien bleiben erhalten'));f.click('scan','library1');await f.turn();assert(requests.some(row=>row.body?.action==='scan'&&row.body.library==='library1'));
 f.click('upload');await f.turn();assert(f.settings.open);const uploadForm=f.settings.form;uploadForm.values={library:'library1'};const requestCount=requests.length;uploadForm.dispatch('submit');await f.turn();assert.equal(f.upload.clicked,1);assert(!f.settings.open);assert.equal(requests.length,requestCount,'Opening file chooser must not replace its input by refreshing the page.');
 f.upload.files=[{name:'new.jpg',size:3,slice:()=>({arrayBuffer:async()=>new Uint8Array([1,2,3]).buffer})}];f.node.dispatch('change',{target:f.upload});await f.turn();await f.turn();assert.equal(uploads.length,1);assert.deepEqual(uploads[0],{library:'library1',name:'new.jpg',offset:0,data:'AQID'});assert(requests.some(row=>row.body?.action==='scan'));
 f.click('library-remove','library1');await f.turn();assert(requests.some(row=>row.body?.action==='library_remove'&&row.body.library==='library1'));assert.equal(photos.state('actions').library,'');
 f.click('open','photo1');assert(f.lightbox.open);photos.dispose();assert(!f.lightbox.open);const afterDispose=requests.length;f.click('favorite','photo1');await f.turn();assert.equal(requests.length,afterDispose);
 console.log('Photo rendering, escaped names, authenticated URLs, per-user views, gallery filters, viewer actions, keyboard navigation, confirmation, upload chooser lifetime and disposal passed.');
})().catch(error=>{console.error(error);process.exitCode=1;photos.dispose();});
