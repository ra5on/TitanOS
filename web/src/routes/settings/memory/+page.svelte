<script lang="ts">
 import { api, ApiError, type SystemInfo } from '#lib/api.ts';
 import { formatBytes } from '#lib/format.ts';
 import { t, errorText } from '#lib/i18n/index.svelte.ts';
 type Status = { settings: { cachePercent: number }; readOnly: boolean; budgetBytes: number; reserveBytes: number; inFlightBytes: number; memory: SystemInfo['memory']; paused: boolean; error?: string };
 let status = $state<Status | null>(null);
 let percent = $state(20), busy = $state(false), error = $state(''), saved = $state(false);
 function fail(err: unknown) { error = errorText(err instanceof ApiError ? err.code : 'internal') + (err instanceof ApiError ? `\n${err.message}` : ''); }
 $effect(() => {
  let stopped = false;
  api.get<Status>('/system/memory').then(result => { if (!stopped) { status = result; percent = result.settings.cachePercent; } }, fail);
  const timer = setInterval(() => { api.get<Status>('/system/memory').then(result => { if (!stopped) status = result; }, () => {}); }, 5000);
  return () => { stopped = true; clearInterval(timer); };
 });
 async function save(event: SubmitEvent) {
  event.preventDefault(); busy = true; error = ''; saved = false;
  try { status = await api.put<Status>('/system/memory', { cachePercent: percent }); saved = true; }
  catch (err) { fail(err); } finally { busy = false; }
 }
</script>
<h1>{t('memoryPolicy.title')}</h1>
<p class="muted">{t('memoryPolicy.intro')}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if status}
 {#if status.error}<p class="notice-warn" role="status">{status.error}</p>{/if}
 {#if status.readOnly}<p class="notice-warn">{t('memoryPolicy.readOnly')}</p>{/if}
 <form class="tile" onsubmit={save}>
  <label for="cache-budget">{t('memoryPolicy.percent')}</label>
  <select id="cache-budget" class="field" bind:value={percent} disabled={busy || status.readOnly}>
   {#each [10,20,30,40,50,60,70,80,90] as value (value)}<option value={value}>{value} %</option>{/each}
  </select>
  <p class="muted">{t('memoryPolicy.hint')}</p>
  <dl>
   <div><dt>{t('memoryPolicy.budget')}</dt><dd>{formatBytes(status.budgetBytes)}</dd></div>
   <div><dt>{t('memoryPolicy.reserve')}</dt><dd>{formatBytes(status.reserveBytes)}</dd></div>
   <div><dt>{t('memoryPolicy.available')}</dt><dd>{formatBytes(status.memory.availableBytes)}</dd></div>
   <div><dt>{t('memoryPolicy.cache')}</dt><dd>{formatBytes(status.memory.cacheBytes ?? 0)}</dd></div>
   <div><dt>{t('memoryPolicy.dirty')}</dt><dd>{formatBytes((status.memory.dirtyBytes ?? 0) + (status.memory.writebackBytes ?? 0))}</dd></div>
  </dl>
  {#if status.paused}<p class="notice-warn" role="status">{t('error.upload_memory_pressure')}</p>{/if}
  <button class="btn btn-primary" disabled={busy || status.readOnly}>{t('common.save')}</button>
  {#if saved}<p role="status">{t('memoryPolicy.saved')}</p>{/if}
 </form>
{:else if !error}<p>{t('common.loading')}</p>{/if}
<style>
 h1 { font-size: var(--text-page); font-weight: 600; }
 .tile { max-width: 680px; padding: var(--tile-pad); display: flex; flex-direction: column; gap: 14px; }
 label { font-weight: 600; }
 p, dl { margin: 0; }
 dl { display: flex; flex-direction: column; gap: 10px; }
 dl div { display: flex; justify-content: space-between; gap: 16px; flex-wrap: wrap; }
 dd { margin: 0; font-family: var(--font-mono); }
 dt { color: var(--muted); }
 button { align-self: flex-start; }
 .notice-error { white-space: pre-wrap; }
</style>
