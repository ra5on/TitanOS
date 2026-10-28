<script lang="ts">
 import { api, ApiError } from '#lib/api.ts';
 import PageHead from '#lib/components/PageHead.svelte';
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
<PageHead title={t('powerButton.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('powerButton.intro')}</p>
</PageHead>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if status}
 {#if status.error}<p class="notice-warn" role="status">{status.error}</p>{/if}
 {#if status.readOnly}<p class="notice-warn">{t('energy.readOnly')}</p>{/if}
 <form class="tile panel" onsubmit={save} aria-labelledby="power-action-title">
  <h2 class="tile-title" id="power-action-title">{t('powerButton.press')}</h2>
  <select id="power-action" class="field" aria-labelledby="power-action-title" bind:value={action} disabled={busy || status.readOnly}>
   {#each (['ignore', 'reboot', 'poweroff'] as const) as value (value)}<option value={value}>{t(`powerButton.${value}`)}</option>{/each}
  </select>
  <p class="muted hint">{t('powerButton.shutdownHint')}</p>
  <p class="muted hint">{t('powerButton.longPress')}</p>
  <div class="row-actions">
   <button class="btn btn-primary" disabled={busy || status.readOnly}>{t('common.save')}</button>
   {#if saved}<span class="muted" role="status">{t('powerButton.saved')}</span>{/if}
  </div>
 </form>
{:else if !error}<p>{t('common.loading')}</p>{/if}
<style>
 select { max-width: 32rem; }
 .hint { font-size: 0.875rem; }
 .notice-error { white-space: pre-wrap; }
</style>
