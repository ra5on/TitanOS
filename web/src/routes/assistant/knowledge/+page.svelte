<script lang="ts">
 import {onMount} from 'svelte';
 import {api,ApiError} from '#lib/api.ts';
 import {t,errorText,type MessageKey} from '#lib/i18n/index.svelte.ts';
 import PageHead from '#lib/components/PageHead.svelte';
 import Dialog from '#lib/components/Dialog.svelte';
 import EmptyState from '#lib/components/EmptyState.svelte';
 type Entry={id?:string;revision?:number;kind:string;title:string;content?:string;origin?:string;root?:string;path?:string;updated?:string};
 type Result={hits:{root:string;path:string;page?:number;text:string}[];errors:{path:string;error:string}[];cut:boolean;nextOffset?:number;checked:number};
 let entries=$state<Entry[]>([]),editing=$state<Entry|null>(null),removing=$state<Entry|null>(null),roots=$state<{id:string;name:string}[]>([]);
 let busy=$state(false),error=$state(''),loaded=$state(false),source=$state(''),query=$state(''),result=$state<Result|null>(null);
 const label=(key:string)=>t(`knowledge.${key}` as MessageKey);
 const failure=(e:unknown)=>e instanceof ApiError?errorText(e.code)+(e.message?`: ${e.message}`:''):errorText('internal');
 async function load(){try{entries=(await api.get<{entries:Entry[]}>('/assistant/knowledge')).entries;loaded=true;error='';}catch(e){error=failure(e);}}
 onMount(()=>{load();api.get<{id:string;name:string}[]>('/files/roots').then(v=>roots=v.filter(r=>!r.id.startsWith('stick')),()=>{});});
 function edit(e?:Entry){editing=e?structuredClone($state.snapshot(e)):{kind:'fact',title:'',content:'',origin:'',root:'',path:''};error='';}
 async function save(){if(!editing)return;busy=true;try{await api.post('/assistant/knowledge',editing);editing=null;await load();}catch(e){error=failure(e);}finally{busy=false;}}
 async function remove(){if(!removing)return;busy=true;try{await api.post('/assistant/knowledge/delete',{id:removing.id,revision:removing.revision});removing=null;result=null;await load();}catch(e){error=failure(e);}finally{busy=false;}}
 async function search(offset=0){busy=true;error='';try{result=await api.post<Result>('/assistant/knowledge/search',{source,query,offset});}catch(e){result=null;error=failure(e);}finally{busy=false;}}
</script>
<PageHead title={label('title')} parent={{href:'/assistant',label:t('assistant.title')}}>
 {#snippet actions()}<button class="btn" onclick={load} disabled={busy}>{label('refresh')}</button><button class="btn btn-primary" onclick={()=>edit()} disabled={busy}>{label('create')}</button>{/snippet}
</PageHead>
<p class="muted">{label('intro')}</p>
{#if error&&!editing&&!removing}<p class="notice-error" role="alert">{error}</p>{/if}
{#if loaded&&!entries.length}<EmptyState title={label('empty')} text={label('intro')}/>{/if}
<div class="entries">
 {#each entries as entry (entry.id)}<section class="tile"><div class="head"><h2>{entry.title}</h2><span class="chip">{label(entry.kind)}</span></div>
 {#if entry.content}<p class="content">{entry.content}</p>{/if}{#if entry.kind==='source'}<p class="mono">{entry.root}/{entry.path}</p>{/if}
 <p class="muted small">{entry.origin??''}{entry.updated?` · ${new Date(entry.updated).toLocaleString()}`:''}</p>
 <div class="actions"><button class="btn" onclick={()=>edit(entry)}>{label('edit')}</button><button class="btn btn-danger" onclick={()=>removing=entry}>{label('forget')}</button></div></section>{/each}
</div>
{#if entries.some(e=>e.kind==='source')}
<section class="tile search"><h2>{label('search')}</h2><p class="muted">{label('searchHint')}</p>
<form onsubmit={(e)=>{e.preventDefault();search();}}><label>{label('source')}<select class="field" bind:value={source} required onchange={()=>result=null}><option value="">—</option>{#each entries.filter(e=>e.kind==='source') as e}<option value={e.id}>{e.title}</option>{/each}</select></label><label>{label('query')}<input class="field" bind:value={query} required maxlength="200" oninput={()=>result=null}/></label><button class="btn" disabled={busy}>{label('search')}</button></form>
{#if result}<p role="status">{label('checked')}: {result.checked}</p>{#if result.cut}<p class="notice-warn">{label('cut')}</p>{/if}{#each result.hits as hit}<article><p class="mono">{hit.root}/{hit.path}{hit.page?` · ${label('page')} ${hit.page}`:''}</p><blockquote>{hit.text}</blockquote></article>{/each}{#each result.errors as failure}<p class="notice-warn">{failure.path}: {failure.error}</p>{/each}{#if result.nextOffset!==undefined}<button class="btn" disabled={busy} onclick={()=>search(result!.nextOffset)}>{label('next')}</button>{/if}{/if}
</section>{/if}
{#if editing}<Dialog title={label(editing.id?'edit':'create')} wide locked={busy} onclose={()=>editing=null}><form onsubmit={(e)=>{e.preventDefault();save();}}>
<label>{label('kind')}<select class="field" bind:value={editing.kind}>{#each ['fact','preference','rule','source','template','project'] as kind}<option value={kind}>{label(kind)}</option>{/each}</select></label>
<label>{label('name')}<input class="field" bind:value={editing.title} maxlength="200" required/></label>
{#if editing.kind==='source'}<label>{label('root')}<select class="field" bind:value={editing.root} required><option value="">—</option>{#if editing.root&&!roots.some(r=>r.id===editing?.root)}<option value={editing.root}>{editing.root}</option>{/if}{#each roots as root}<option value={root.id}>{root.name} ({root.id})</option>{/each}</select></label><label>{label('path')}<input class="field mono" bind:value={editing.path}/></label>{:else}<label>{label('content')}<textarea class="field" rows="8" bind:value={editing.content} maxlength="8000"></textarea></label>{/if}
<label>{label('origin')}<input class="field" bind:value={editing.origin} maxlength="500"/></label>{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
<div class="dialog-actions"><button type="button" class="btn" disabled={busy} onclick={()=>editing=null}>{label('cancel')}</button><button class="btn btn-primary" disabled={busy}>{label('save')}</button></div></form></Dialog>{/if}
{#if removing}<Dialog title={label('forget')} locked={busy} onclose={()=>removing=null}><p>{removing.title}</p><p>{label('forgetHint')}</p>{#if error}<p class="notice-error">{error}</p>{/if}<div class="dialog-actions"><button class="btn" disabled={busy} onclick={()=>removing=null}>{label('cancel')}</button><button class="btn btn-danger" disabled={busy} onclick={remove}>{label('forget')}</button></div></Dialog>{/if}
<style>
 .entries{display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,350px),1fr));gap:var(--gap);margin:20px 0}.tile{padding:var(--tile-pad);display:grid;gap:12px;min-width:0}.head,.actions{display:flex;gap:12px;justify-content:space-between;flex-wrap:wrap}h2{font-size:1rem}.content{white-space:pre-wrap;overflow-wrap:anywhere}.small{font-size:.8125rem}form,label{display:grid;gap:10px}.field{width:100%}.mono{overflow-wrap:anywhere}blockquote{white-space:pre-wrap;overflow-wrap:anywhere;margin:12px;border-left:2px solid var(--accent);padding-left:12px}
</style>
