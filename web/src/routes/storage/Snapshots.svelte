<script lang="ts">
 import { api, ApiError } from '#lib/api.ts';
 import { t, errorText } from '#lib/i18n/index.svelte.ts';
 import Dialog from '#lib/components/Dialog.svelte';
 let { volume }: { volume: string } = $props();
 let open = $state(false);
 let snapshots = $state<{name: string}[]>([]);
 let name = $state('');
 let busy = $state(false);
 let error = $state('');
 let restored = $state('');
 let confirm = $state('');
 let removing = $state('');
 const url = $derived(`/storage/volumes/${volume}/snapshots`);
 async function load() { snapshots = await api.get(url); }
 async function show() { open = true; error = ''; try { await load(); } catch (err) { error = errorText(err instanceof ApiError ? err.code : 'internal'); } }
 async function act(action: string, value: string) {
  busy = true; error = ''; restored = '';
  try { await api.post(url, { action, name: value, confirm }); removing = ''; confirm = ''; if (action === 'restore') restored = `/mnt/${volume}/restored/${value}`; if (action === 'create') name = ''; await load(); }
  catch (err) { error = errorText(err instanceof ApiError ? err.code : 'internal'); }
  finally { busy = false; }
 }
</script>
<button type="button" class="btn" onclick={show}>{t('snapshot.title')}</button>
{#if open}
 <Dialog title={t('snapshot.title')} locked={busy} onclose={() => open = false}>
  <div class="snapshots">
   <p class="notice-warn">{t('snapshot.note')}</p>
 <a class="btn" href={'/storage/manage?'+new URLSearchParams({volume,mode:'snapshots'})}>{t('ops.browse')}</a>
 <a class="btn" href="/tools/maintenance">{t('ops.maintenance')}</a>
   <form onsubmit={(event) => { event.preventDefault(); act('create', name); }}>
    <label>{t('snapshot.name')} <input class="field" bind:value={name} pattern={'[a-zA-Z0-9][a-zA-Z0-9_\\-]{0,63}'} maxlength="64" required /></label>
    <button class="btn btn-primary" disabled={busy}>{t('snapshot.create')}</button>
   </form>
   {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
   <p role="status" class="mono">{restored}</p>
   {#each snapshots as snapshot (snapshot.name)}
    <div class="row">
     <span class="mono">{snapshot.name}</span>
     <button class="btn" disabled={busy} onclick={() => act('restore', snapshot.name)}>{t('snapshot.restore')}</button>
     <button class="btn" disabled={busy} onclick={() => { removing = snapshot.name; confirm = ''; }}>{t('files.delete')}</button>
    </div>
   {/each}
   <p class="muted">{t('snapshot.restoreNote', { path: `/mnt/${volume}/restored/` })}</p>
   {#if removing}
    <label>{t('snapshot.confirm', { name: removing })}<input class="field" bind:value={confirm} /></label>
    <button class="btn btn-warn" disabled={busy || confirm !== removing} onclick={() => act('delete', removing)}>{t('files.delete')}</button>
   {/if}
  </div>
  <div class="dialog-actions">
   <button type="button" class="btn" disabled={busy} onclick={() => open = false}>{t('common.done')}</button>
  </div>
 </Dialog>
{/if}
<style>
 .snapshots { display: flex; flex-direction: column; gap: 16px; }
 form, .row { display: flex; flex-wrap: wrap; gap: 8px; }
 form { align-items: flex-end; }
 .row { align-items: center; }
 .row .mono { flex-basis: 100%; overflow-wrap: anywhere; }
 label { display: flex; flex-direction: column; gap: 6px; }
</style>
