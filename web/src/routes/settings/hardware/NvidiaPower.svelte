<script lang="ts">
 import { onMount } from 'svelte';
 import { api, ApiError } from '#lib/api.ts';
 import { t, errorText } from '#lib/i18n/index.svelte.ts';
 let { address }: { address: string } = $props();
 let percent = $state(0);
 let error = $state('');
 let ready = $state(false);
 let busy = $state(false);
 let saved = $state(false);
 onMount(() => { api.get<{settings: Record<string, {powerPercent: number}>; problems: Record<string,string>}>('/hardware/nvidia').then(value => { percent = value.settings[address]?.powerPercent ?? 0; error = value.problems[address] ?? ''; ready = true; }, err => error = errorText(err instanceof ApiError ? err.code : 'internal')); });
 async function save() {
  busy = true; error = ''; saved = false;
  try { await api.put(`/hardware/nvidia/${address}`, {powerPercent: percent}); saved = true; }
  catch (err) { error = errorText(err instanceof ApiError ? err.code : 'internal'); }
  finally { busy = false; }
 }
</script>
<form onsubmit={(event) => { event.preventDefault(); save(); }}>
 <label>{t('hardware.powerLimit')}<select class="field" bind:value={percent} disabled={!ready || busy} onchange={() => saved = false}>
  <option value={0}>{t('hardware.powerDefault')}</option>
  {#each [20,30,40,50,60,70,80,90,100] as value (value)}<option value={value}>{value} %</option>{/each}
 </select></label>
 <button class="btn" disabled={!ready || busy}>{t('common.save')}</button>
 {#if saved}<span role="status">{t('hardware.powerSaved')}</span>{/if}
 {#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</form>
<style>
 form { display: flex; flex-wrap: wrap; align-items: end; gap: 8px; margin-bottom: 10px; }
 label { display: flex; flex-direction: column; gap: 6px; }
 p { flex-basis: 100%; overflow-wrap: anywhere; }
</style>
