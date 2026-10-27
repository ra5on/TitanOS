<script lang="ts">
 import {onMount} from 'svelte';
 import {api,ApiError} from '#lib/api.ts';
 import {t,tOr,errorText,type MessageKey} from '#lib/i18n/index.svelte.ts';
 import PageHead from '#lib/components/PageHead.svelte';
 import EmptyState from '#lib/components/EmptyState.svelte';
 import Dialog from '#lib/components/Dialog.svelte';
 type Task={id:string;title:string;updated:string;priority?:number;dependsOn?:string[];run?:{state:string;stage?:string;tool?:string;detail?:string}};
 let tasks=$state<Task[]>([]),editing=$state<Task|null>(null),filter=$state('open'),busy=$state(false),error=$state(''),loaded=$state(false);
 const label=(key:string)=>t(`tasksAI.${key}` as MessageKey);
 const terminal=(task:Task)=>!task.run||['done','stopped'].includes(task.run.state);
 const shown=$derived(tasks.filter(task=>filter==='all'||filter==='open'&&!terminal(task)||filter==='finished'&&terminal(task)).sort((a,b)=>(b.priority??0)-(a.priority??0)));
 const failure=(e:unknown)=>e instanceof ApiError?errorText(e.code)+(e.message?`: ${e.message}`:''):errorText('internal');
 async function load(){try{tasks=(await api.get<{chats:Task[]}>('/assistant/chats')).chats;loaded=true;error='';}catch(e){error=failure(e);}}
 onMount(()=>{load();const timer=setInterval(()=>{if(!busy&&!editing&&document.visibilityState==='visible')load()},5000);return()=>clearInterval(timer)});
 async function control(task:Task,action:string){busy=true;error='';try{await api.post(`/assistant/chats/${encodeURIComponent(task.id)}/control`,{action});await load();}catch(e){error=failure(e);}finally{busy=false;}}
 // Resuming answers with the event stream of the agent; the job runs on in
 // the NAS, so the stream is closed as soon as the NAS accepted the request.
 async function resume(task:Task){
  busy=true;error='';const stream=new AbortController();
  try{
   let res:Response;
   try{res=await fetch(`/api/v1/assistant/chats/${encodeURIComponent(task.id)}/control`,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'resume'}),signal:stream.signal});}
   catch{throw new ApiError(0,'unreachable','');}
   if(!res.ok){let code='internal',message='';try{const data=await res.json();code=data.error?.code??code;message=data.error?.message??'';}catch{/* not JSON */}throw new ApiError(res.status,code,message);}
  }catch(e){error=failure(e);}
  finally{stream.abort();busy=false;await load();}
 }
 async function save(){if(!editing)return;busy=true;try{await api.put(`/assistant/chats/${editing.id}/schedule`,{priority:editing.priority??0,dependsOn:editing.dependsOn??[]});editing=null;await load();}catch(e){error=failure(e);}finally{busy=false;}}
</script>
<PageHead title={label('title')} parent={{href:'/assistant',label:t('assistant.title')}}>{#snippet actions()}<button class="btn" onclick={load} disabled={busy}>{label('refresh')}</button><a class="btn" href="/assistant/automations">{t('automation.title')}</a>{/snippet}</PageHead>
<p class="muted">{label('intro')}</p>
<label class="filter">{label('filter')}<select class="field" bind:value={filter}>{#each ['open','finished','all'] as option}<option value={option}>{label(option)}</option>{/each}</select></label>
{#if error&&!editing}<div class="notice-error load-error" role="alert"><span>{error}</span><button class="btn" disabled={busy} onclick={load}>{t('common.retry')}</button></div>{/if}
{#if !loaded&&!error}<p class="muted" role="status">{t('common.loading')}</p>{/if}
{#if loaded&&!shown.length}<EmptyState title={label('empty')}/>{/if}
<div class="tasks">{#each shown as task (task.id)}<section class="tile"><div class="head"><h2><a href={`/assistant?chat=${task.id}`}>{task.title}</a></h2><span>{task.run?tOr(`agent.state.${task.run.state}`,task.run.state):'—'}</span></div>
{#if task.run?.stage}<p>{tOr(`agent.stage.${task.run.stage}`,task.run.stage)} {task.run.tool??''}</p>{/if}
{#if task.run?.detail}<p class="notice-warn">{tOr(`tasksAI.${task.run.detail}`,task.run.detail)}</p>{/if}
<p class="muted">{label('priority')}: {label(`priority${task.priority??0}`)}</p>
{#if task.dependsOn?.length}<p>{label('dependencies')}: {task.dependsOn.map(id=>tasks.find(v=>v.id===id)?.title??id).join(', ')}</p>{/if}
<div class="actions"><a class="btn" href={`/assistant?chat=${task.id}`}>{label('openChat')}</a>{#if task.run&&['queued','waiting','paused','blocked'].includes(task.run.state)}<button class="btn" disabled={busy} onclick={()=>editing=structuredClone($state.snapshot(task))}>{label('schedule')}</button>{/if}{#if task.run&&['queued','running','waiting'].includes(task.run.state)}<button class="btn" disabled={busy} onclick={()=>control(task,'pause')}>{t('agent.pause')}</button>{/if}{#if task.run?.state==='paused'}<button class="btn" disabled={busy} onclick={()=>resume(task)}>{t('agent.resume')}</button>{/if}{#if task.run&&['queued','running','waiting','paused','blocked'].includes(task.run.state)}<button class="btn" disabled={busy} onclick={()=>control(task,'stop')}>{label('stop')}</button>{/if}</div>
</section>{/each}</div>
{#if editing}<Dialog title={label('schedule')} locked={busy} onclose={()=>editing=null}><form onsubmit={(e)=>{e.preventDefault();save();}}><label>{label('priority')}<select class="field" bind:value={editing.priority}>{#each [-1,0,1] as p}<option value={p}>{label(`priority${p}`)}</option>{/each}</select></label><p class="muted">{label('dependencyHint')}</p>{#each tasks.filter(task=>task.id!==editing?.id) as task}<label class="check"><input type="checkbox" checked={editing.dependsOn?.includes(task.id)??false} onchange={(e)=>{if(editing)editing.dependsOn=e.currentTarget.checked?[...(editing.dependsOn??[]),task.id]:(editing.dependsOn??[]).filter(id=>id!==task.id)}}/>{task.title}</label>{/each}{#if error}<p class="notice-error">{error}</p>{/if}<div class="dialog-actions"><button type="button" class="btn" disabled={busy} onclick={()=>editing=null}>{t('common.cancel')}</button><button class="btn btn-primary" disabled={busy}>{label('save')}</button></div></form></Dialog>{/if}
<style>.tasks{display:grid;gap:var(--gap);margin-top:20px}.tile{padding:var(--tile-pad);display:grid;gap:12px}.head,.actions{display:flex;gap:12px;justify-content:space-between;flex-wrap:wrap}h2{font-size:1rem}form,label{display:grid;gap:10px}.check{display:flex;align-items:center}.filter{max-width:320px;margin-top:16px}.load-error{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:8px;margin-top:16px}</style>
