<script lang="ts">
 import VirtualEntries from './VirtualEntries.svelte';
 import {previewKind,rangeSelection} from '#lib/file-actions.ts';
 import {beginFileDrag,endFileDrag,hasFileDrag} from '#lib/file-drag.ts';
 import {onMount,tick} from 'svelte';
 import { api, ApiError, type FileRoot, type FileEntry } from '#lib/api.ts';
 import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
 import { formatBytes } from '#lib/format.ts';
 import { fileList } from '#lib/file-list.ts';
 let {roots,refresh,showHidden,onchange,ondropfiles,onpreview}:{onpreview:(root:string,path:string,entry:FileEntry)=>void;ondropfiles:(event:DragEvent,root:string,path:string)=>void;roots:FileRoot[];refresh:number;showHidden:boolean;onchange:(root:string,path:string,selected:string[])=>void}=$props();
 let rootId=$state(''),path=$state(''),selected=$state<string[]>([]),entries=$state<FileEntry[]>([]),error=$state(''),busy=$state(false),search=$state('');
 const root=$derived(roots.find(r=>r.id===rootId));
 const visible=$derived(fileList(entries.filter(e=>showHidden||!e.name.startsWith('.')),search,'name',false,i18n.locale));
 $effect(()=>{if(!roots.some(r=>r.id===rootId)){rootId=roots.find(r=>r.kind==='share')?.id??roots[0]?.id??'';path='';}});
 $effect(()=>{onchange(rootId,path,selected);});
 $effect(()=>{refresh;showHidden;const id=rootId,dir=path;const controller=new AbortController();entries=[];selected=[];error='';busy=true;
  if(id)api.get<FileEntry[]>('/files/list?'+new URLSearchParams({root:id,path:dir}),controller.signal).then(result=>{if(!controller.signal.aborted)entries=result;}).catch(err=>{if(!controller.signal.aborted)error=errorText(err instanceof ApiError?err.code:'internal');}).finally(()=>{if(!controller.signal.aborted)busy=false;});
  return()=>controller.abort();
 });
 $effect(()=>{const names=new Set(visible.map(entry=>entry.name));const kept=selected.filter(name=>names.has(name));if(kept.length!==selected.length)selected=kept;});
 onMount(()=>{let active=true,inFlight=false;let controller:AbortController|undefined;
  const blocked=()=>document.hidden||busy||!rootId||root?.kind==='stick'||document.querySelector('dialog[open]')!==null||['INPUT','TEXTAREA','SELECT'].includes(document.activeElement?.tagName??'');
  async function poll(){if(inFlight||blocked())return;const id=rootId,dir=path,before=entries;controller=new AbortController();inFlight=true;
   try{const result=await api.get<FileEntry[]>('/files/list?'+new URLSearchParams({root:id,path:dir}),controller.signal);
    if(!active||id!==rootId||dir!==path||before!==entries||blocked())return;
    const scroll=pane?.scrollTop??0;entries=result;const names=new Set(result.map(e=>e.name));selected=selected.filter(n=>names.has(n));error='';await tick();if(active&&id===rootId&&dir===path&&pane)pane.scrollTop=scroll;
   }catch{if(active&&id===rootId&&dir===path)error=t('files.autoRefreshFailed');}finally{inFlight=false;}
  }
  const timer=setInterval(poll,10000);return()=>{active=false;controller?.abort();clearInterval(timer);};
 });
 let pane=$state<HTMLElement>();
 let list=$state<{reveal:(index:number)=>void}>(),anchor='';
 async function keyboard(event:KeyboardEvent){
  if(document.querySelector('dialog[open]')||event.ctrlKey||event.metaKey||event.altKey||event.defaultPrevented)return;
  if(event.target instanceof Element&&event.target.closest('input:not([type=checkbox]),select,button,textarea'))return;
  const key=event.key.toLowerCase();
  if(['arrowup','arrowdown','arrowleft','arrowright','home','end'].includes(key)&&visible.length){event.preventDefault();event.stopPropagation();const focused=event.target instanceof Element?event.target.closest<HTMLElement>('[data-pane-name]')?.dataset.paneName:undefined;const old=visible.findIndex(e=>e.name===(focused??selected.at(-1)));const index=key==='home'?0:key==='end'?visible.length-1:Math.max(0,Math.min(visible.length-1,old<0?0:old+(['arrowup','arrowleft'].includes(key)?-1:1)));const name=visible[index].name;if(event.shiftKey){if(!anchor)anchor=visible[Math.max(0,old)].name;selected=rangeSelection(visible.map(e=>e.name),anchor,name);}else{selected=[name];anchor=name;}list?.reveal(index);await tick();pane?.querySelector<HTMLElement>(`[data-pane-name="${CSS.escape(name)}"]`)?.focus({preventScroll:true});}
  else if((key==='enter'||key===' ')&&selected.length===1){if(event.target instanceof Element&&event.target.closest('input'))return;const entry=visible.find(e=>e.name===selected[0]);if(!entry)return;event.preventDefault();event.stopPropagation();if(entry.dir&&key==='enter')open([path,entry.name].filter(Boolean).join('/'));else if(!entry.dir&&previewKind(entry.name,entry.size))onpreview(rootId,[path,entry.name].filter(Boolean).join('/'),entry);}
 }

 function open(next:string){path=next;selected=[];search='';anchor='';}
</script>
<!-- Keyboard events are delegated from the focusable file rows. -->
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<section onkeydown={keyboard} ondragover={event=>{if(root?.writable&&hasFileDrag(event))event.preventDefault();}} ondrop={event=>ondropfiles(event,rootId,path)} bind:this={pane} class="tile pane" aria-label={t('files.secondPane')}>
 <div class="controls">
 <label>{t('files.secondPane')}<select class="field" bind:value={rootId} onchange={()=>open('')}>
 {#each roots.filter(r=>r.kind!=='stick') as place (place.id)}<option value={place.id}>{place.name} · {t(place.kind==='share'?'files.shares':place.kind==='volume'?'files.volumes':'files.usb')}</option>{/each}
 </select></label>
 <p class="mono">{root?.name}/{path}</p>
 {#if root && !root.writable}<span class="chip">{t('files.readOnly')}</span>{/if}
 <button class="btn" disabled={!path} onclick={()=>open(path.split('/').slice(0,-1).join('/'))}>{t('files.parentFolder')}</button>
 <input class="field" aria-label={t('files.search')} placeholder={t('files.search')} bind:value={search}/>
 </div>
 {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
 {#if busy}<p role="status">{t('files.previewLoading')}</p>{/if}
 <VirtualEntries bind:this={list} items={visible} key={e=>e.name} label={t('files.title')} reset={`${rootId}/${path}/${search}`} rowHeight={72}>
 {#snippet children(entry)}
 <!-- svelte-ignore a11y_no_static_element_interactions -->
 <div class="entry entry-row" tabindex="-1" data-pane-name={entry.name} draggable={root?.kind!=='stick'} ondragstart={event=>{const names=selected.includes(entry.name)?selected:[entry.name];selected=names;beginFileDrag(event,{root:rootId,paths:names.map(n=>[path,n].filter(Boolean).join('/'))});}} ondragend={endFileDrag} ondragover={event=>{if(entry.dir&&root?.writable&&hasFileDrag(event)){event.preventDefault();event.stopPropagation();}}} ondrop={event=>{if(entry.dir)ondropfiles(event,rootId,[path,entry.name].filter(Boolean).join('/'));}}><input type="checkbox" aria-label={t('files.select',{name:entry.name})} checked={selected.includes(entry.name)} onchange={e=>selected=e.currentTarget.checked?[...selected,entry.name]:selected.filter(n=>n!==entry.name)}/>
 {#if entry.dir}<button class="folder" onclick={()=>open([path,entry.name].filter(Boolean).join('/'))}>{entry.name}/</button>{:else}<span>{entry.name}</span>{/if}
 <span class="muted">{entry.dir?'–':formatBytes(entry.size)}</span></div>
 {/snippet}</VirtualEntries>
 <p class="muted">{t('files.selected',{count:selected.length})}</p>
</section>
<style>.entry-row:focus-visible{outline:2px solid var(--accent);outline-offset:-2px;}.pane{min-width:0;overflow:auto;}.controls{display:flex;gap:10px;flex-wrap:wrap;padding:12px;border-bottom:1px solid var(--border);}.controls label{display:flex;flex-direction:column;gap:6px;width:100%;}.controls p{width:100%;margin:0;overflow-wrap:anywhere;}.entry{display:grid;grid-template-columns:20px minmax(0,1fr) auto;gap:10px;align-items:center;padding:10px;border-bottom:1px solid var(--border);}.entry span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}.folder{border:0;background:transparent;color:var(--text);text-align:left;cursor:pointer;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:inherit;}p{padding:10px;}</style>
