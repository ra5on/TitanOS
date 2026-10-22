<script lang="ts">
 import { session } from '#lib/session.svelte.ts';
 import { onMount } from 'svelte';
 import { api, ApiError } from '#lib/api.ts';
 import { errorText, t, tOr, type MessageKey } from '#lib/i18n/index.svelte.ts';
 import PageHead from '#lib/components/PageHead.svelte';
 import EmptyState from '#lib/components/EmptyState.svelte';
 import Dialog from '#lib/components/Dialog.svelte';
 type Trigger = { kind: string; at?: string; minutes?: number; time?: string; timezone?: string; weekday?: number; event?: string; target?: string; root?: string; path?: string; percent?: number };
 type Automation = { id?: string; revision?: number; name:string; prompt:string; enabled:boolean; trigger:Trigger; conditions:{volume?:string;noRebuild?:boolean}; execution:string; notify:string; catchUp:string; overlap:string; next?:string; detail?:string; runs?:{chat:string;scheduled:string;state:string;detail?:string}[] };
 let items=$state<Automation[]>([]), editing=$state<Automation|null>(null), removing=$state<Automation|null>(null);
 let roots=$state<{id:string;name:string;kind:string}[]>([]), volumes=$state<{name:string}[]>([]), drives=$state<{id:string;label:string;name:string;model:string}[]>([]);
 const admin=$derived(['owner','admin'].includes(session.data?.user?.role??''));
 async function catalog(){
  const calls=await Promise.allSettled([api.get<{id:string;name:string;kind:string}[]>('/files/roots'),...(admin?[api.get<{volumes:{name:string}[]}>('/storage'),api.get<{id:string;label:string;name:string;model:string}[]>('/usb')]:[])]);
  const r=calls[0];if(r.status==='fulfilled'&&Array.isArray(r.value))roots=r.value.filter(v=>v.kind!=='stick');
  const v=calls[1];if(v?.status==='fulfilled'&&!Array.isArray(v.value)&&'volumes' in v.value)volumes=v.value.volumes;
  const d=calls[2];if(d?.status==='fulfilled'&&Array.isArray(d.value))drives=d.value as typeof drives;
 }
 let busy=$state(false), error=$state(''), loaded=$state(false), once=$state('');
 const label=(name:string)=>t(`automation.${name}` as MessageKey);
 const date=(value?:string)=>value && !value.startsWith('0001-') ? new Date(value).toLocaleString() : '—';
 const failure=(e:unknown)=>e instanceof ApiError ? errorText(e.code)+(e.message ? `: ${e.message}`:'') : errorText('internal');
 async function load(){try{items=(await api.get<{automations:Automation[]}>('/assistant/automations')).automations;loaded=true;error='';}catch(e){error=failure(e);}}
 onMount(()=>{load();const timer=setInterval(()=>{if(!editing&&!removing&&!busy&&document.visibilityState==='visible')load();},15000);return()=>clearInterval(timer);});
 function edit(a?:Automation,duplicate=false){
  catalog();error='';editing=a?structuredClone($state.snapshot(a)):{name:'',prompt:'',enabled:true,trigger:{kind:'daily',time:'18:00',timezone:Intl.DateTimeFormat().resolvedOptions().timeZone,weekday:0},conditions:{},execution:'inherit',notify:'errors',catchUp:'latest',overlap:'skip'};
  if(duplicate){delete editing.id;delete editing.revision;editing.enabled=false;delete editing.runs;delete editing.next;}
  const at=editing.trigger.at?new Date(editing.trigger.at):new Date(Date.now()+3600000);once=new Date(at.getTime()-at.getTimezoneOffset()*60000).toISOString().slice(0,16);
 }
 function changeKind(){if(!editing)return;editing.trigger={kind:editing.trigger.kind,time:'18:00',timezone:Intl.DateTimeFormat().resolvedOptions().timeZone,weekday:0,minutes:60,event:admin?'notification':'files',target:admin?'backup_failed':'',root:'',path:'',percent:85};}
 async function save(){if(!editing)return;busy=true;try{const a=structuredClone($state.snapshot(editing));if(a.trigger.kind==='once')a.trigger.at=new Date(once).toISOString();await api.post('/assistant/automations',a);editing=null;await load();}catch(e){error=failure(e);}finally{busy=false;}}
 async function toggle(a:Automation){busy=true;try{await api.post('/assistant/automations',{...a,enabled:!a.enabled});await load();}catch(e){error=failure(e);}finally{busy=false;}}
 async function run(a:Automation){busy=true;try{await api.post(`/assistant/automations/${a.id}/run`);await load();}catch(e){error=failure(e);}finally{busy=false;}}
 async function remove(){if(!removing)return;busy=true;try{await api.delete(`/assistant/automations/${removing.id}`);removing=null;await load();}catch(e){error=failure(e);}finally{busy=false;}}
</script>

<PageHead title={label('title')}>
 {#snippet actions()}<a class="btn" href="/assistant">{t('assistant.title')}</a><button class="btn" onclick={load} disabled={busy}>{label('refresh')}</button><button class="btn btn-primary" onclick={()=>edit()} disabled={busy}>{label('create')}</button>{/snippet}
</PageHead>
<p class="muted intro">{label('intro')}</p>
{#if error && !editing && !removing}<p class="notice-error" role="alert">{error}</p>{/if}
{#if loaded && !items.length}<EmptyState title={label('empty')} text={label('emptyText')} />{/if}
<div class="automations">
 {#each items as a (a.id)}
  <section class="tile automation">
   <div class="head"><h2>{a.name}</h2><span class="muted">{label(a.enabled?'enabled':'paused')}</span></div>
   <p class="instruction">{a.prompt}</p>
   <div class="facts"><span>{label(a.trigger.kind)}{#if a.trigger.kind==='event'} · {label(a.trigger.event??'notification')} · {a.trigger.target||a.trigger.root}{/if}{#if ['daily','weekly'].includes(a.trigger.kind)} · {a.trigger.time} · {a.trigger.timezone}{/if}{#if a.trigger.kind==='weekly'} · {label(`day${a.trigger.weekday??0}`)}{/if}{#if a.trigger.kind==='interval'} · {a.trigger.minutes} {label('minutes')}{/if}</span><span>{label('next')}: {date(a.next)}</span><span>{label('execution')}: {label(a.execution)}</span></div>
   {#if a.detail}<p class="notice-warn">{tOr(`automation.detail.${a.detail}`,a.detail)}</p>{/if}
   <div class="actions"><button class="btn" onclick={()=>edit(a)} disabled={busy}>{label('edit')}</button><button class="btn" onclick={()=>toggle(a)} disabled={busy}>{label(a.enabled?'pause':'enable')}</button><button class="btn" onclick={()=>run(a)} disabled={busy}>{label('run')}</button><button class="btn" onclick={()=>edit(a,true)} disabled={busy}>{label('duplicate')}</button><button class="btn btn-danger" onclick={()=>removing=a} disabled={busy}>{label('delete')}</button></div>
   <details><summary>{label('history')} ({a.runs?.length??0})</summary><div class="history">{#each [...(a.runs??[])].reverse() as r (r.chat)}<div><time>{date(r.scheduled)}</time><a href={`/assistant?chat=${r.chat}`}>{tOr(`agent.state.${r.state}`,label('starting'))}</a>{#if r.detail}<span class="muted">{r.detail}</span>{/if}</div>{/each}</div></details>
  </section>
 {/each}
</div>
{#if editing}
 <Dialog title={label(editing.id?'edit':'create')} wide locked={busy} onclose={()=>{editing=null;error='';}}>
  <form onsubmit={(e)=>{e.preventDefault();save();}}>
   <label>{label('name')}<input class="field" bind:value={editing.name} maxlength="120" required disabled={busy}/></label>
   <label>{label('prompt')}<textarea class="field" bind:value={editing.prompt} rows="5" maxlength="8000" required disabled={busy}></textarea></label>
   <label>{label('trigger')}<select class="field" bind:value={editing.trigger.kind} onchange={changeKind} disabled={busy}>{#each ['once','interval','daily','weekly','event'] as kind}<option value={kind}>{label(kind)}</option>{/each}</select></label>
   {#if editing.trigger.kind==='once'}<label>{label('at')}<input class="field" type="datetime-local" bind:value={once} required disabled={busy}/></label>{/if}
   {#if editing.trigger.kind==='interval'}<label>{label('minutes')}<input class="field" type="number" min="1" max="525600" bind:value={editing.trigger.minutes} required disabled={busy}/></label>{/if}
   {#if ['daily','weekly'].includes(editing.trigger.kind)}<div class="fields"><label>{label('time')}<input class="field" type="time" bind:value={editing.trigger.time} required disabled={busy}/></label><label>{label('timezone')}<input class="field" bind:value={editing.trigger.timezone} required disabled={busy}/></label></div>{/if}
   {#if editing.trigger.kind==='weekly'}<label>{label('weekday')}<select class="field" bind:value={editing.trigger.weekday} disabled={busy}>{#each [0,1,2,3,4,5,6] as day}<option value={day}>{label(`day${day}`)}</option>{/each}</select></label>{/if}
   {#if editing.trigger.kind==='event'}
    <label>{label('event')}<select class="field" bind:value={editing.trigger.event} disabled={busy}>{#each (admin?['notification','usb','volume','files']:['files']) as event}<option value={event}>{label(event)}</option>{/each}</select></label>
    {#if editing.trigger.event==='files'}<label>{label('root')}<select class="field" bind:value={editing.trigger.root} required disabled={busy}><option value="">{label('choose')}</option>{#if editing.trigger.root&&!roots.some(r=>r.id===editing?.trigger.root)}<option value={editing.trigger.root}>{editing.trigger.root}</option>{/if}{#each roots as root (root.id)}<option value={root.id}>{root.name} ({root.kind})</option>{/each}</select></label><label>{label('path')}<input class="field" bind:value={editing.trigger.path} disabled={busy}/></label><p class="muted small">{label('filesHelp')}</p>
    {:else if editing.trigger.event==='usb'}<label>{label('usbDevice')}<select class="field" bind:value={editing.trigger.target} required disabled={busy}><option value="">{label('choose')}</option>{#if editing.trigger.target&&!drives.some(d=>d.id===editing?.trigger.target)}<option value={editing.trigger.target}>{editing.trigger.target}</option>{/if}{#each drives as drive (drive.id)}<option value={drive.id}>{drive.label||drive.model||drive.name} ({drive.id})</option>{/each}</select></label>
    {:else if editing.trigger.event==='volume'}<label>{label('volumeName')}<select class="field" bind:value={editing.trigger.target} required disabled={busy}><option value="">{label('choose')}</option>{#if editing.trigger.target&&!volumes.some(v=>v.name===editing?.trigger.target)}<option value={editing.trigger.target}>{editing.trigger.target}</option>{/if}{#each volumes as volume}<option value={volume.name}>{volume.name}</option>{/each}</select></label>
    {:else}<label>{label('eventKind')}<select class="field" bind:value={editing.trigger.target} required disabled={busy}>{#if editing.trigger.target&&!['backup_failed','config_backup_failed','volume_degraded','disk_unhealthy','disk_hot','update_available'].includes(editing.trigger.target)}<option value={editing.trigger.target}>{editing.trigger.target}</option>{/if}{#each ['backup_failed','config_backup_failed','volume_degraded','disk_unhealthy','disk_hot','update_available'] as kind}<option value={kind}>{label(`event_${kind}`)}</option>{/each}</select></label>{/if}
    {#if editing.trigger.event==='volume'}<label>{label('percent')}<input class="field" type="number" min="1" max="100" bind:value={editing.trigger.percent} required disabled={busy}/></label>{/if}
    <p class="muted small">{label('eventHelp')}</p>
   {/if}
   <div class="fields"><label>{label('execution')}<select class="field" bind:value={editing.execution} disabled={busy}>{#each ['inherit','confirm','direct'] as mode}<option value={mode}>{label(mode)}</option>{/each}</select></label><label>{label('notify')}<select class="field" bind:value={editing.notify} disabled={busy}>{#each ['errors','always','never'] as mode}<option value={mode}>{label(mode)}</option>{/each}</select></label></div>
   <details><summary>{label('conditions')}</summary><div class="advanced">
    {#if admin}<label>{label('requiredVolume')}<input class="field" bind:value={editing.conditions.volume} disabled={busy}/></label><label class="check"><input type="checkbox" bind:checked={editing.conditions.noRebuild} disabled={busy}/>{label('noRebuild')}</label>{/if}
    <label>{label('catchUp')}<select class="field" bind:value={editing.catchUp} disabled={busy}><option value="latest">{label('latest')}</option><option value="skip">{label('skip')}</option></select></label>
    <label>{label('overlap')}<select class="field" bind:value={editing.overlap} disabled={busy}><option value="skip">{label('skip')}</option><option value="queue">{label('queue')}</option></select></label>
   </div></details>
   <label class="check"><input type="checkbox" bind:checked={editing.enabled} disabled={busy}/>{label('enabled')}</label>
   {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
   <div class="dialog-actions"><button type="button" class="btn" onclick={()=>{editing=null;error='';}} disabled={busy}>{label('cancel')}</button><button class="btn btn-primary" type="submit" disabled={busy}>{label('save')}</button></div>
  </form>
 </Dialog>
{/if}
{#if removing}<Dialog title={label('delete')} locked={busy} onclose={()=>{removing=null;error='';}}><p>{removing.name}</p><p>{label('deleteHelp')}</p>{#if error}<p class="notice-error">{error}</p>{/if}<div class="dialog-actions"><button class="btn" disabled={busy} onclick={()=>removing=null}>{label('cancel')}</button><button class="btn btn-danger" disabled={busy} onclick={remove}>{label('delete')}</button></div></Dialog>{/if}
<style>
 .intro{margin-bottom:20px}.automations{display:grid;gap:16px}.automation{display:grid;gap:12px;padding:var(--tile-pad)}.head,.actions,.facts{display:flex;gap:12px;align-items:center;flex-wrap:wrap}.head{justify-content:space-between}h2{font-size:1.1rem}.instruction{white-space:pre-wrap;overflow-wrap:anywhere}.facts,.small{font-size:.8125rem}.facts{color:var(--muted)}form,.advanced{display:grid;gap:14px}label{display:grid;gap:6px}label.check{display:flex;gap:8px;align-items:center}.fields{display:grid;grid-template-columns:1fr 1fr;gap:12px}.history{display:grid;gap:8px;margin-top:12px}.history>div{display:flex;flex-wrap:wrap;gap:12px}.advanced{margin-top:12px}.field{width:100%;min-width:0}summary{cursor:pointer}@media(max-width:760px){.fields{grid-template-columns:1fr}.actions>.btn{flex:1 1 auto}}
</style>
