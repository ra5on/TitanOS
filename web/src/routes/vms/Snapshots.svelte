<script lang="ts">
 import { api, ApiError, type VM } from '#lib/api.ts';
 import { errorText, t } from '#lib/i18n/index.svelte.ts';
 import { vmLabel } from '#lib/vm-name.ts';
 import { formatBytes } from '#lib/format.ts';
 import Dialog from '#lib/components/Dialog.svelte';
 let { vm }: {vm: VM} = $props();
 type Schedule={enabled:boolean;time:string;days:number[];keep:number;memory:boolean;allowPause:boolean};
 type Snapshot={id:string;name:string;created:string;memory:boolean;automatic:boolean;bytes:number;incomplete?:boolean};
 type Status={snapshots:Snapshot[];schedule:Schedule;pending:boolean;reason:''|'passthrough'|'folders'|'block'|'disks'|'preparing'|'volume'|'iso';job:{state:'running'|'failed';text:string}|null};
 let status=$state<Status|null>(null);
 let panel=$state<''|'create'|'plan'|'list'|'restore'|'delete'|'recover'>('');
 let target=$state<Snapshot|null>(null);
 let name=$state('');let memory=$state(true);let allowPause=$state(false);let confirm=$state('');let discardMemory=$state(false);
 let schedule=$state<Schedule>({enabled:true,time:'03:00',days:[0,1,2,3,4,5,6],keep:7,memory:true,allowPause:false});
 let error=$state('');let busy=$state(false);let menu=$state<HTMLDetailsElement>();
 const base=$derived(`/vms/${vm.name}`);
 const working=$derived(busy||status?.job?.state==='running');
 async function load(url=base){try{const result=await api.get<Status>(url+'/snapshots');if(url===base)status=result}catch(e){if(url===base)error=e instanceof ApiError?errorText(e.code):String(e)}}
 $effect(()=>{const url=base;status=null;panel='';error='';load(url);const timer=setInterval(()=>{if(!document.hidden)load(url)},3000);return()=>clearInterval(timer)});
 function open(next:typeof panel,snapshot:Snapshot|null=null){
  if(menu)menu.open=false;error='';target=snapshot;confirm='';memory=snapshot?.memory??true;allowPause=false;discardMemory=false;
  name=new Date().toLocaleString();
  if(next==='plan'&&status?.schedule?.time)schedule={...status.schedule,days:[...(status.schedule.days??[])]};
  panel=next;
 }
 async function submit(e:SubmitEvent){
  e.preventDefault();busy=true;error='';const url=base;
  try{
   if(panel==='create')await api.post(url+'/snapshots',{name,memory,allowPause});
   if(panel==='plan')await api.put(url+'/snapshot-schedule',schedule);
   if(panel==='restore'&&target)await api.post(url+`/snapshots/${target.id}/restore`,{confirm,memory});
   if(panel==='delete'&&target)await api.delete(url+`/snapshots/${target.id}`,{confirm});
   if(panel==='recover')await api.post(url+'/snapshot-recovery',{confirm,discardMemory});
   panel='list';await load(url);
  }catch(e){error=e instanceof ApiError?errorText(e.code)+(e.code==='vm_failed'?`\n${e.message}`:''):String(e)}finally{busy=false}
 }
 const days=['sun','mon','tue','wed','thu','fri','sat'] as const;
 const confirmed=$derived(confirm===(panel==='delete'?target?.name:vmLabel(vm)));
</script>
<details class="snapshot-menu" bind:this={menu}>
 <summary class="btn">{t('snapshots.title')} ▾{#if status?.pending || status?.job?.state==='failed'} !{/if}</summary>
 <div class="menu tile">
  <button class="btn" onclick={()=>open('create')} disabled={working||!vm.available||!!status?.reason||status?.pending}>{t('snapshots.create')}</button>
  <button class="btn" onclick={()=>open('plan')} disabled={working}>{t('snapshots.plan')}</button>
  <button class="btn" onclick={()=>open('list')}>{t('snapshots.manage')}{#if status}{' '}({status.snapshots.length}){/if}</button>
 </div>
</details>
{#if panel}
 <Dialog title={t(`snapshots.${panel==='list'?'manage':panel}`)} locked={busy} wide onclose={()=>{panel=''}}>
  {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
  {#if status?.reason}<p class="notice-warn">{t(`snapshots.reason.${status.reason}`)}</p>{/if}
  {#if status?.job}<div class:notice-error={status.job.state==='failed'} class:notice-info={status.job.state==='running'} role="status">{t(`snapshots.job.${status.job.state}`)}{#if status.job.text}<pre>{status.job.text}</pre>{/if}</div>{/if}
  {#if panel==='list'}
   <p class="muted">{t('snapshots.scope')}</p>
   {#if status?.pending&&!working}<div class="notice-warn">{t('snapshots.interrupted')} <button class="btn" onclick={()=>open('recover')}>{t('snapshots.recover')}</button></div>{/if}
   {#if status?.schedule.enabled}<p>{t('snapshots.scheduled',{time:status.schedule.time,keep:status.schedule.keep})}</p>{/if}
   {#each status?.snapshots??[] as snapshot(snapshot.id)}
    <div class="snapshot-row">
     <div class="snapshot-info"><strong>{snapshot.incomplete?t('snapshots.incomplete'):snapshot.name}</strong>{#if snapshot.incomplete}<span class="mono">{snapshot.id}</span>{/if}{#if !snapshot.incomplete}<span class="muted">{new Date(snapshot.created).toLocaleString()} · {formatBytes(snapshot.bytes)} · {t(snapshot.memory?'snapshots.withMemory':'snapshots.disksOnly')}{snapshot.automatic?` · ${t('snapshots.automatic')}`:''}</span>{/if}</div>
     <button class="btn" disabled={working||status?.pending||vm.state==='running'||snapshot.incomplete} onclick={()=>open('restore',snapshot)}>{t('snapshots.restore')}</button>
     <button class="btn" disabled={working||status?.pending} onclick={()=>open('delete',snapshot)}>{t('snapshots.delete')}</button>
    </div>
   {:else}<p class="muted">{t('snapshots.empty')}</p>{/each}
   {#if vm.state==='running'}<p class="muted">{t('snapshots.stopFirst')}</p>{/if}
   <div class="dialog-actions"><button class="btn" onclick={()=>{panel=''}}>{t('snapshots.close')}</button><button class="btn" onclick={()=>open('plan')} disabled={working}>{t('snapshots.plan')}</button><button class="btn btn-primary" onclick={()=>open('create')} disabled={working||!vm.available||!!status?.reason||status?.pending}>{t('snapshots.create')}</button></div>
  {:else}
   <form onsubmit={submit}>
    {#if panel==='create'}
     <label>{t('snapshots.name')}<input class="field" bind:value={name} maxlength="80" required/></label>
     <p class="muted">{t('snapshots.scope')}</p>
     {#if vm.state==='running'}
      <label class="check"><input type="checkbox" bind:checked={memory}/>{t('snapshots.includeMemory')}</label>
      <p class="notice-warn">{t('snapshots.pauseInfo')}</p>
      <label class="check"><input type="checkbox" bind:checked={allowPause} required/>{t('snapshots.allowPause')}</label>
     {:else}<p>{t('snapshots.stoppedInfo')}</p>{/if}
    {:else if panel==='plan'}
     <label class="check"><input type="checkbox" bind:checked={schedule.enabled}/>{t('snapshots.enable')}</label>
     <div class="schedule-fields">
      <label>{t('snapshots.time')}<input class="field" type="time" bind:value={schedule.time} required disabled={!schedule.enabled}/></label>
      <label>{t('snapshots.keep')}<input class="field" type="number" min="1" max="100" bind:value={schedule.keep} required disabled={!schedule.enabled}/></label>
     </div>
     <fieldset disabled={!schedule.enabled}><legend>{t('snapshots.days')}</legend><div class="week">{#each days as day,index}<label class="check"><input type="checkbox" checked={schedule.days.includes(index)} onchange={(e)=>{schedule.days=e.currentTarget.checked?[...schedule.days,index]:schedule.days.filter(d=>d!==index)}}/>{t(`snapshots.day.${day}`)}</label>{/each}</div></fieldset>
     <label class="check"><input type="checkbox" bind:checked={schedule.memory} disabled={!schedule.enabled}/>{t('snapshots.includeMemory')}</label>
     <p class="notice-warn">{t('snapshots.pauseInfo')}</p>
     <label class="check"><input type="checkbox" bind:checked={schedule.allowPause} disabled={!schedule.enabled} required={schedule.enabled&&schedule.memory}/>{t('snapshots.allowPause')}</label>
     <p class="muted">{t('snapshots.retention')}</p>
    {:else if panel==='restore'}
     <p class="notice-warn">{t('snapshots.restoreWarning',{name:target?.name??'',vm:vmLabel(vm)})}</p>
     {#if target?.memory}<label class="check"><input type="checkbox" bind:checked={memory}/>{t('snapshots.restoreMemory')}</label><p class="muted">{t('snapshots.runtime')}</p>{/if}
    {:else if panel==='delete'}<p class="notice-warn">{t('snapshots.deleteWarning',{name:target?.name??''})}</p>
    {:else if panel==='recover'}
     <p class="notice-warn">{t('snapshots.recoverInfo')}</p>
     <label class="check"><input type="checkbox" bind:checked={discardMemory}/>{t('snapshots.discardMemory')}</label>
    {/if}
    {#if ['restore','delete','recover'].includes(panel)}<label>{t('snapshots.confirm',{name:panel==='delete'?target?.name??'':vmLabel(vm)})}<input class="field" bind:value={confirm} autocomplete="off" required/></label>{/if}
    <div class="dialog-actions"><button class="btn" type="button" onclick={()=>{panel='list'}} disabled={busy}>{t('snapshots.back')}</button><button class="btn btn-primary" type="submit" disabled={busy||(panel==='create'&&(working||!vm.available||!!status?.reason||(vm.state==='running'&&!memory)))||(panel==='plan'&&schedule.enabled&&!schedule.days.length)||(['restore','delete','recover'].includes(panel)&&!confirmed)}>{t(panel==='plan'?'snapshots.save':`snapshots.${panel}`)}</button></div>
   </form>
  {/if}
 </Dialog>
{/if}
<style>
 .snapshot-menu{position:relative}.snapshot-menu summary{list-style:none;cursor:pointer}.menu{position:absolute;right:0;top:calc(100% + 6px);z-index:20;padding:8px;display:flex;flex-direction:column;gap:6px;min-width:230px;box-shadow:var(--shadow-menu)}
 form{display:flex;flex-direction:column;gap:16px}label{display:flex;flex-direction:column;gap:6px}.check{flex-direction:row;align-items:center;gap:8px}.check input{width:18px;height:18px;flex:none}
 .snapshot-row{display:flex;align-items:center;gap:8px;flex-wrap:wrap;border-bottom:1px solid var(--border);padding:12px 0}.snapshot-info{display:flex;flex-direction:column;gap:4px;flex:1 1 260px;min-width:0;overflow-wrap:anywhere}.schedule-fields,.week{display:flex;gap:16px;flex-wrap:wrap}.schedule-fields>label{flex:1}fieldset{border:1px solid var(--border);padding:12px;border-radius:var(--radius-ctl)}pre{white-space:pre-wrap;overflow-wrap:anywhere;font-size:.8rem;margin-top:8px}.notice-error{white-space:pre-wrap}
</style>
