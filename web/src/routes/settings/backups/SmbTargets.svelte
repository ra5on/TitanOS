<script lang="ts">
 import { api, ApiError } from '#lib/api.ts';
 import { t, errorText } from '#lib/i18n/index.svelte.ts';
 import Dialog from '#lib/components/Dialog.svelte';
 export type SmbTarget = {id:string;host:string;username:string;password?:string;share:string;folder:string};
 let {targets,onchange}:{targets:SmbTarget[];onchange:()=>void}=$props();
 let draft=$state<SmbTarget|null>(null);
 let shares=$state<string[]>([]);
 let busy=$state(false);
 let error=$state('');
 let deleting=$state<string|null>(null);
 let editing=$state(false);
 function edit(target?:SmbTarget) {
  draft=target ? {...target,password:''} : {id:'',host:'',username:'',password:'',share:'',folder:''};
  editing=!!target; shares=target ? [target.share] : []; error='';
 }
 function problem(err:unknown) {error=err instanceof ApiError ? `${errorText(err.code)} ${err.message}` : errorText('internal');}
 async function discover(){
  if(!draft)return; busy=true;error='';shares=[];draft.share='';
  try {shares=(await api.post<{shares:string[]}>('/backups/smb/discover',draft)).shares;if(!shares.length)error=t('backup.smb.noShares');}
  catch(err){problem(err);}finally{busy=false;}
 }
 async function save(event:SubmitEvent){
  event.preventDefault();if(!draft)return;busy=true;error='';
  try {await api.put('/backups/smb',draft);draft=null;onchange();}catch(err){problem(err);}finally{busy=false;}
 }
 async function remove(){
  if(!deleting)return;busy=true;error='';
  try{await api.delete(`/backups/smb/${encodeURIComponent(deleting)}`);deleting=null;onchange();}catch(err){problem(err);}finally{busy=false;}
 }
</script>
<section class="tile">
 <div class="row"><h2>{t('backup.smb.title')}</h2><span class="chip warn">Beta</span></div>
 <p class="muted">{t('backup.smb.intro')}</p>
 {#each targets as target(target.id)}
 <div class="row"><span class="mono">{target.id} · //{target.host}/{target.share}/{target.folder}</span><button class="btn" onclick={()=>edit(target)}>{t('backup.edit')}</button><button class="btn" onclick={()=>{deleting=target.id;error='';}}>{t('backup.remove')}</button></div>
 {/each}
 <button class="btn" onclick={()=>edit()}>{t('backup.smb.add')}</button>
</section>
{#if draft}
 <Dialog title={t('backup.smb.title')} locked={busy} onclose={()=>{if(!busy)draft=null;}}>
 <form onsubmit={save}>
  <p class="muted">{t('backup.smb.setup')}</p>
  <label>{t('backup.smb.name')}<input class="field" bind:value={draft.id} pattern="[A-Za-z][A-Za-z0-9\-]*" maxlength="31" required disabled={editing} /></label>
  <label>{t('backup.smb.host')}<input class="field" bind:value={draft.host} oninput={()=>{shares=[];if(draft)draft.share='';}} required /></label>
  <label>{t('backup.smb.user')}<input class="field" bind:value={draft.username} oninput={()=>{shares=[];if(draft)draft.share='';}} required autocomplete="off" /></label>
  <label>{t('backup.smb.password')}<input class="field" type="password" bind:value={draft.password} autocomplete="new-password" placeholder={targets.some(t=>t.id===draft?.id)?t('backup.smb.keepPassword'):''} /></label>
  <button class="btn" type="button" disabled={busy||!draft.id||!draft.host||!draft.username} onclick={discover}>{t('backup.smb.discover')}</button>
  <label>{t('backup.smb.share')}<select class="field" aria-label={t('backup.smb.share')} bind:value={draft.share} required><option value="" disabled>–</option>{#each shares as share(share)}<option value={share}>{share}</option>{/each}</select></label>
  <label>{t('backup.smb.folder')}<input class="field" bind:value={draft.folder} placeholder="TitanOS" /></label>
  <p class="muted">{t('backup.smb.credentialsHint')}</p>
  {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
  <div class="dialog-actions"><button type="button" class="btn" disabled={busy} onclick={()=>draft=null}>{t('common.cancel')}</button><button class="btn btn-primary" disabled={busy||!draft.share}>{busy?t('backup.smb.testing'):t('backup.smb.save')}</button></div>
 </form>
 </Dialog>
{/if}
{#if deleting}
 <Dialog title={t('backup.remove')} locked={busy} onclose={()=>{if(!busy)deleting=null;}}>
 <p>{t('backup.smb.removeHint')}</p>
 {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
 <div class="dialog-actions"><button class="btn" disabled={busy} onclick={()=>deleting=null}>{t('common.cancel')}</button><button class="btn" disabled={busy} onclick={remove}>{t('backup.remove')}</button></div>
 </Dialog>
{/if}
<style>
 section,form{display:flex;flex-direction:column;gap:14px} section{padding:20px}.row{display:flex;flex-wrap:wrap;align-items:center;gap:12px}.row span{overflow-wrap:anywhere}label{display:flex;flex-direction:column;gap:6px}h2,p{margin:0}button{align-self:flex-start}
</style>
