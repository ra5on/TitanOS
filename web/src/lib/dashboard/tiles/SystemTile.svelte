<script lang="ts">
	import { formatBytes, formatBytesOf, formatDuration, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	const info = $derived(system.info);
	const memoryPercent = $derived(info && info.memory.totalBytes > 0 ? (100 * info.memory.usedBytes) / info.memory.totalBytes : 0);
</script>

{#if info}
	<div class="meter">
		<div class="row"><span>{t('system.cpu')}</span><span class="mono">{formatPercent(info.cpu.usagePercent)}</span></div>
		<div class="bar"><span style:width="{info.cpu.usagePercent}%"></span></div>
	</div>
	<div class="meter">
		<div class="row"><span>{t('system.memory')}</span><span class="mono">{formatBytesOf(info.memory.usedBytes, info.memory.totalBytes)}</span></div>
		<div class="bar"><span style:width="{memoryPercent}%"></span></div>
		<div class="memory-note muted"><span>{t('system.memoryIncludesCache')}</span><span>{t('system.memoryAvailable', { value: formatBytes(info.memory.availableBytes) })}</span></div>
	</div>
	<dl>
		<div><dt>{t('system.inRam')}</dt><dd class="mono">{info.image ? formatBytes(info.image.sizeBytes) : '–'}</dd></div>
		<div><dt>{t('system.overlay')}</dt><dd class="mono">{info.image ? formatBytesOf(info.image.overlayUsedBytes, info.image.overlayTotalBytes) : '–'}</dd></div>
		<div><dt>{t('system.uptime')}</dt><dd class="mono">{formatDuration(info.uptimeSeconds)}</dd></div>
		<div>
			<dt>{t('system.stick')}</dt>
			<dd class="mono">{info.stick.present ? t('system.stickOk', { label: info.stick.label }) : t('system.stickMissing')}</dd>
		</div>
	</dl>
{/if}

<style>
	.memory-note { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 4px 12px; font-size: 0.75rem; }
	.meter {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.row {
		display: flex;
		justify-content: space-between;
		gap: 12px;
		font-size: 0.875rem;
	}
	dl {
		margin: 0;
		padding-top: 12px;
		border-top: 1px solid var(--panel-border);
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px;
	}
	dl div {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	dt {
		font-size: 0.75rem;
		color: var(--muted);
	}
	dd {
		margin: 0;
		font-size: 0.9375rem;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
</style>
