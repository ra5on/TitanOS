<script lang="ts">
 import {api,ApiError,type FileRoot} from '#lib/api.ts';
 import Dialog from '#lib/components/Dialog.svelte';
 import {t,errorText} from '#lib/i18n/index.svelte.ts';
 import {formatBytes} from '#lib/format.ts';
 let {root,dir,showHidden,onclose,onopen,onchanged}:{root:FileRoot;dir:string;showHidden:boolean;onclose:()=>void;onopen:(path:string)=>void;onchanged:()=>void}=$props();
 type Group={sha256:string;size:number;paths:string[];versions:Record<string,string>};
 let result=$state<{groups:Group[];scanned:number;hashed:number}|null>(null),busy=$state(false),error=$state(''),selected=$state<string[]>([]),review=$state(false),confirmed=$state(false),permanent=$state(false);
 let controller:AbortController|undefined;$effect(()=>()=>controller?.abort());
 async function scan(){controller?.abort();const c=new AbortController();controller=c;busy=true;error='';result=null;selected=[];review=false;try{const r=await api.post<typeof result>('/files/duplicates',{root:root.id,path:dir,showHidden},c.signal);if(!c.signal.aborted)result=r;}catch(e){if(!c.signal.aborted)error=errorText(e instanceof ApiError?e.code:'internal');}finally{if(controller===c)busy=false;}}
 async function remove(){if(!confirmed||!root.writable||!selected.length)return;busy=true;error='';try{const versions=Object.assign({},...(result?.groups.map(g=>g.versions)??[]));await api.post('/files/delete',{root:root.id,paths:selected,permanent:!root.recycle||permanent,versions});result=null;selected=[];review=false;onchanged();}catch(e){error=errorText(e instanceof ApiError?e.code:'internal');}finally{busy=false;}}
</script>
<Dialog title={t('files.duplicatesTitle')} {onclose} locked={busy} wide>
 <p class="mono">{root.name}/{dir}</p><p class="muted">{t('files.duplicatesHint')}</p>
 <button class="btn" disabled={busy||review} onclick={scan}>{t('files.duplicatesStart')}</button>
 {#if busy}<p role="status">{t('files.searchRunning')}</p>{#if !review}<button class="btn" onclick={()=>{controller?.abort();busy=false;}}>{t('common.cancel')}</button>{/if}{/if}
 {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
 {#if result}<p>{t('files.duplicatesCount',{count:result.groups.length,scanned:result.scanned,hashed:result.hashed})}</p>
 <div class="results">{#each result.groups as group (group.sha256)}<section><h3>{formatBytes(group.size)} · {group.paths.length} ×</h3><p class="mono muted">SHA-256: {group.sha256}</p>
 {#each group.paths as path}<div class="row"><label><input type="checkbox" disabled={busy||review||!root.writable} checked={selected.includes(path)} onchange={e=>{selected=e.currentTarget.checked?[...selected,path]:selected.filter(p=>p!==path);confirmed=false;}}/>{path}</label><button class="btn" disabled={busy} onclick={()=>onopen(path.split('/').slice(0,-1).join('/'))}>{t('files.openFolder')}</button></div>{/each}</section>{/each}</div>
 {#if review}<ul>{#each selected as path}<li class="mono">{path}</li>{/each}</ul>
 {#if root.recycle}<label><input type="checkbox" bind:checked={permanent} onchange={()=>confirmed=false}/>{t('files.duplicatesPermanent')}</label>{/if}
 <p class="notice-warn">{t(root.recycle&&!permanent?'files.duplicatesTrashHint':'files.duplicatesDeleteHint')}</p>
 <label><input type="checkbox" bind:checked={confirmed}/>{t('files.duplicatesConfirm')}</label>
 <div class="dialog-actions"><button class="btn" disabled={busy} onclick={()=>review=false}>{t('common.cancel')}</button><button class="btn btn-danger" disabled={busy||!confirmed||!root.writable} onclick={remove}>{t(root.recycle&&!permanent?'files.duplicatesTrash':'files.duplicatesPermanent')}</button></div>
 {:else}<button class="btn" disabled={busy||!selected.length||!root.writable} onclick={()=>{review=true;confirmed=false;permanent=false;}}>{t('files.duplicatesReview')}</button>{/if}{/if}
 <div class="dialog-actions"><button class="btn" disabled={busy} onclick={onclose}>{t('files.editClose')}</button></div>
</Dialog>
<style>.results{max-height:45vh;overflow:auto;}.row{display:flex;gap:12px;justify-content:space-between;align-items:center;padding:8px 0;}.row label{overflow-wrap:anywhere;}.mono{overflow-wrap:anywhere;}section{border-top:1px solid var(--border);margin-top:12px;}</style>
