<script lang="ts">
 import { t } from '#lib/i18n/index.svelte.ts';
 import { formatBytes } from '#lib/format.ts';
 export type DriverStatus = {
  state: 'idle' | 'checking' | 'downloading' | 'ready' | 'current' | 'unavailable' | 'error' | 'environment' | 'sourceReady' | 'building'; automatic: boolean; error?: string; checked?: string;
  pending?: { version: string }; canRestore: boolean;
  source?: { driver: string; toolkit: string; downloaded: boolean };
  versions: { version: string; sha256: string; size: number; downloaded: boolean; recommended?: boolean; revision: number; local?: boolean }[] | null;
 };
 let { status, busy, action }: { status: DriverStatus; busy: boolean; action: (data: Record<string, unknown>) => void } = $props();
 let selected = $state('bundled');
 const versions = $derived(status.versions ?? []);
 const choice = $derived(versions.find(v => v.sha256 === selected));
 const loading = $derived(['checking','downloading','environment','building'].includes(status.state));
</script>
<section class="driver-updates">
 <h3>{t('plugins.driverTitle')}</h3>
 <p class="muted">{t('plugins.driverHint')}</p>
 <label class="check"><input type="checkbox" checked={status.automatic} disabled={busy} onchange={(e) => action({action:'automatic',automatic:e.currentTarget.checked})} /><span>{t('plugins.driverAutomatic')}</span></label>
 <div class="row"><button class="btn" type="button" disabled={busy || loading} onclick={() => action({action:'check'})}>{t(loading ? 'plugins.driverWorking' : 'plugins.driverCheck')}</button><span role="status" class="muted">{t(`plugins.driverState.${status.state}`)}</span></div>
 {#if status.source}
  <p>{t('plugins.driverSource', { driver: status.source.driver, toolkit: status.source.toolkit })}</p>
  <button class="btn" type="button" disabled={busy || loading || !!status.pending || !status.source.downloaded} onclick={() => action({action:'prepare'})}>{t('plugins.driverPrepare')}</button>
 {/if}
 <p class="muted">{t('plugins.driverBuildHint')}</p>
 <label><span>{t('plugins.driverVersion')}</span><select class="field" bind:value={selected} disabled={busy || !!status.pending}>
  <option value="bundled">{t('plugins.driverBundled')}</option>
  {#each versions as version (version.sha256)}<option value={version.sha256}>{version.version}{version.local ? ` · ${t('plugins.driverLocal')}` : ''}{version.recommended ? ` · ${t('plugins.driverRecommended')}` : ''} · {formatBytes(version.size)}{version.downloaded ? ` · ${t('plugins.driverDownloaded')}` : ''}</option>{/each}
 </select></label>
 {#if status.pending}
  <p role="status" class="notice-warn">{t('plugins.driverPending',{version:status.pending.version})}</p>
  <button type="button" class="btn" disabled={busy} onclick={() => action({action:'select',sha256:'cancel'})}>{t('plugins.driverCancel')}</button>
 {:else if choice && !choice.downloaded}
  <button type="button" class="btn" disabled={busy || loading} onclick={() => action({action:'check',sha256:selected})}>{t('plugins.driverDownload')}</button>
 {:else}
  <button type="button" class="btn btn-primary" disabled={busy || (selected === 'bundled' ? !status.canRestore : !choice?.downloaded)} onclick={() => action({action:'select',sha256:selected})}>{t('plugins.driverSelect')}</button>
 {/if}
 {#if status.error}<details class="notice-warn"><summary>{t('plugins.errorDetails')}</summary><pre>{status.error}</pre></details>{/if}
</section>
<style>
 .driver-updates { display:flex; flex-direction:column; gap:12px; border-top:1px solid var(--border); padding-top:16px; }
 p { margin:0; font-size:0.8125rem; }
 label { display:flex; flex-direction:column; gap:6px; font-size:0.8125rem; min-width:0; }
 .check { flex-direction:row; align-items:flex-start; gap:8px; }
 .row { display:flex; flex-wrap:wrap; align-items:center; gap:8px; }
 select { width:100%; min-width:0; }
 pre { white-space:pre-wrap; overflow-wrap:anywhere; }
</style>
