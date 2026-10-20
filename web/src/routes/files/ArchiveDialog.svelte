<script lang="ts">
 import {untrack} from 'svelte';
 import {api,ApiError,type FileRoot,type FileEntry} from '#lib/api.ts';
 import Dialog from '#lib/components/Dialog.svelte';
 import {folderPath} from '#lib/file-actions.ts';
 import {t,errorText} from '#lib/i18n/index.svelte.ts';
 import {formatBytes} from '#lib/format.ts';
 let {root,path,dir,roots,onclose,ondone}:{root:FileRoot;path:string;dir:string;roots:FileRoot[];onclose:()=>void;ondone:()=>void}=$props();
 type Item={path:string;size:number;dir:boolean};
 let listing=$state<{entries:Item[];bytes:number;version:string}|null>(null),selected=$state<string[]>([]),busy=$state(false),error=$state('');
 let target=$state(''),targetDir=$state(''),name=$state(''),checkedKey=$state(''),conflict=$state(false),free=$state<number|null>(null);
 const targets=$derived(roots.filter(r=>r.writable&&r.kind!=='stick'));
 const key=$derived(JSON.stringify([target,targetDir,name,selected]));
 const selectable=$derived(listing?.entries.filter(e=>!e.dir||!listing!.entries.some(other=>other.path.startsWith(e.path+'/')))??[]);
 const bytes=$derived(selectable.filter(e=>selected.includes(e.path)).reduce((sum,e)=>sum+e.size,0));
 const targetPath=$derived([targetDir,name].filter(Boolean).join('/'));
 $effect(()=>{const controller=new AbortController();root.id;path;
  untrack(()=>{target=targets.find(r=>r.id===root.id)?.id??targets[0]?.id??'';targetDir=target===root.id?dir:'';name=path.split('/').at(-1)!.replace(/\.zip$/i,'')||'Archive';});
  api.post<{entries:Item[];bytes:number;version:string}>('/files/archive',{root:root.id,path},controller.signal).then(result=>{if(!controller.signal.aborted){listing=result;selected=result.entries.filter(e=>!e.dir||!result.entries.some(other=>other.path.startsWith(e.path+'/'))).map(e=>e.path);}}).catch(err=>{if(!controller.signal.aborted)error=errorText(err instanceof ApiError?err.code:'internal');});
  return()=>controller.abort();
 });
 async function check(){busy=true;error='';checkedKey='';
 try{const normalized=folderPath(targetDir);if(normalized===null||!name||name==='.'||name==='..'||/[\/\\]/.test(name))throw new ApiError(400,'invalid_path','');targetDir=normalized;
 const current=await api.get<FileRoot[]>('/files/roots');const destination=current.find(r=>r.id===target);if(!destination?.writable)throw new ApiError(403,'read_only_share','');
 const entries=await api.get<FileEntry[]>('/files/list?'+new URLSearchParams({root:target,path:targetDir}));free=destination.freeBytes;conflict=entries.some(e=>e.name===name);checkedKey=key;
 }catch(err){error=errorText(err instanceof ApiError?err.code:'internal');}finally{busy=false;}}
 async function extract(){if(!listing||checkedKey!==key||conflict||!targets.some(r=>r.id===target))return;busy=true;error='';try{await api.post('/files/extract',{root:root.id,path,version:listing.version,paths:selected,toRoot:target,toDir:targetDir,name});ondone();}catch(err){error=errorText(err instanceof ApiError?err.code:'internal');}finally{busy=false;}}
</script>
<Dialog title={t('files.archiveTitle')} {onclose} locked={busy} wide>
 <p class="mono">{root.name}/{path}</p>
 {#if !listing&&!error}<p role="status">{t('files.previewLoading')}</p>{/if}
 {#if listing}<fieldset disabled={busy}><label><input type="checkbox" checked={selected.length===selectable.length} onchange={e=>selected=e.currentTarget.checked?selectable.map(e=>e.path):[]}/>{t('files.selectAll')}</label>
 <div class="entries">{#each listing.entries as entry (entry.path)}<div class="entry">
 {#if selectable.some(e=>e.path===entry.path)}<input type="checkbox" aria-label={entry.path} checked={selected.includes(entry.path)} onchange={e=>selected=e.currentTarget.checked?[...selected,entry.path]:selected.filter(p=>p!==entry.path)}/>{:else}<span></span>{/if}
 <span class="mono">{entry.path}{entry.dir?'/':''}</span><span>{entry.dir?'–':formatBytes(entry.size)}</span></div>{/each}</div>
 <p>{t('files.archiveSelected',{count:selected.length,size:formatBytes(bytes)})}</p>
 <label>{t('files.archiveDestination')}<select class="field" bind:value={target} onchange={()=>targetDir=''}>{#each targets as place (place.id)}<option value={place.id}>{place.name}</option>{/each}</select></label>
 <label>{t('files.archiveParent')}<input class="field mono" bind:value={targetDir}/></label>
 <label>{t('files.archiveFolder')}<input class="field" bind:value={name} maxlength="255"/></label>
 <p class="muted">{t('files.archiveHint')}</p>
 <button class="btn" disabled={!target||!selected.length} onclick={check}>{t('files.archiveCheck')}</button>
 </fieldset>
 {#if checkedKey===key}<p class="mono">{targets.find(r=>r.id===target)?.name}/{targetPath}</p><p>{t('files.backupFree',{size:formatBytes(free??0)})}</p>{#if conflict}<p class="notice-error" role="alert">{t('files.archiveConflict')}</p>{/if}{#if free!==null&&bytes>free}<p class="notice-error">{t('files.uploadNoSpace')}</p>{/if}{/if}
 {/if}
 {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
 <div class="dialog-actions"><button class="btn" disabled={busy} onclick={onclose}>{t('common.cancel')}</button><button class="btn btn-primary" disabled={busy||!listing||!selected.length||checkedKey!==key||conflict||free===null||bytes>free||!targets.some(r=>r.id===target)} onclick={extract}>{t('files.archiveExtract')}</button></div>
</Dialog>
<style>fieldset{border:0;padding:0;margin:0;}label{display:flex;gap:8px;flex-wrap:wrap;align-items:center;margin:10px 0;}label .field{flex:1;min-width:0;}.entries{max-height:32vh;overflow:auto;}.entry{display:grid;grid-template-columns:20px minmax(0,1fr) auto;gap:10px;border-bottom:1px solid var(--border);padding:8px 0;}.mono{overflow-wrap:anywhere;}</style>
