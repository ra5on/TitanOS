<script lang="ts">
 import { api, ApiError } from '#lib/api.ts';
 import { t, errorText } from '#lib/i18n/index.svelte.ts';
 type Action = 'ignore' | 'reboot' | 'poweroff';
 type Status = { settings: { action: Action }; readOnly: boolean; error?: string };
 let status = $state<Status | null>(null);
 let action = $state<Action>('poweroff');
 let busy = $state(false), error = $state(''), saved = $state(false);
 function take(result: Status) { status = result; action = result.settings.action; }
 function fail(err: unknown) { error = errorText(err instanceof ApiError ? err.code : 'internal') + (err instanceof ApiError ? `\n${err.message}` : ''); }
 $effect(() => { api.get<Status>('/system/power-button').then(take, fail); });
 async function save(event: SubmitEvent) {
  event.preventDefault(); busy = true; saved = false; error = '';
  try { take(await api.put<Status>('/system/power-button', { action })); saved = true; }
  catch (err) { fail(err); } finally { busy = false; }
 }
</script>
<h1>{t('powerButton.title')}</h1>
<p class="muted">{t('powerButton.intro')}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if status}
 {#if status.error}<p class="notice-warn" role="status">{status.error}</p>{/if}
 {#if status.readOnly}<p class="notice-warn">{t('energy.readOnly')}</p>{/if}
 <form class="tile" onsubmit={save}>
  <label for="power-action">{t('powerButton.press')}</label>
  <select id="power-action" class="field" bind:value={action} disabled={busy || status.readOnly}>
   {#each (['ignore', 'reboot', 'poweroff'] as const) as value (value)}<option value={value}>{t(`powerButton.${value}`)}</option>{/each}
  </select>
  <p class="muted">{t('powerButton.shutdownHint')}</p>
  <p class="muted">{t('powerButton.longPress')}</p>
  <button class="btn btn-primary" disabled={busy || status.readOnly}>{t('common.save')}</button>
  {#if saved}<p role="status">{t('powerButton.saved')}</p>{/if}
 </form>
{:else if !error}<p>{t('common.loading')}</p>{/if}
<style>
 h1 { font-size: var(--text-page); font-weight: 600; }
 .tile { max-width: 640px; padding: var(--tile-pad); display: flex; flex-direction: column; gap: 14px; }
 label { font-weight: 600; }
 p { margin: 0; }
 button { align-self: flex-start; }
 .notice-error { white-space: pre-wrap; }
</style>
