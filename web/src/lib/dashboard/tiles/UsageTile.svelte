<script lang="ts">
 import { formatBytes, formatBytesOf, formatPercent } from '#lib/format.ts';
 import { t } from '#lib/i18n/index.svelte.ts';
 import { system, systemHistory } from '#lib/system.svelte.ts';
 import type { TileOptions } from '../options.ts';
 import History from './History.svelte';
 let { kind, options }: { kind: 'cpu' | 'memory'; options: TileOptions } = $props();
 const info = $derived(system.info);
 const percent = $derived(info ? kind === 'cpu' ? info.cpu.usagePercent : info.memory.totalBytes > 0 ? info.memory.usedBytes / info.memory.totalBytes * 100 : 0 : 0);
 const temperature = $derived(info?.temperatures.find((sensor) => sensor.chip === 'coretemp' && sensor.label === 'Package id 0'));
</script>
{#if info}
 <div class="reading mono" title={kind === 'memory' ? t('system.memoryIncludesCache') : ''}>{kind === 'cpu' ? formatPercent(percent) : formatBytesOf(info.memory.usedBytes, info.memory.totalBytes)}</div>
 <div class="bar"><span style:width="{percent}%"></span></div>
 {#if kind === 'cpu' && options.temperature}<div class="detail"><span class="muted">Package id 0</span><span class="mono">{temperature?.celsius == null ? '–' : `${temperature.celsius.toFixed(1)} °C`}</span></div>{/if}
 {#if options.details}
  {#if kind === 'cpu'}<span class="muted model">{info.cpu.model} · {info.cpu.threads}</span>
  {:else}<div class="detail"><span class="muted">{t('system.memoryCache', { value: formatBytes(info.memory.cacheBytes ?? 0) })}</span><span class="mono">{t('system.memoryAvailable', { value: formatBytes(info.memory.availableBytes) })}</span></div>{/if}
 {/if}
 {#if options.history}<History values={systemHistory[kind]} />{/if}
{/if}
<style>
 .reading { font-size: 1.25rem; font-weight: 500; overflow-wrap: anywhere; }
 .detail { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 4px 12px; font-size: .8125rem; }
 .model { font-size: .8125rem; overflow-wrap: anywhere; }
</style>
