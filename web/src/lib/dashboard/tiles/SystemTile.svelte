<script lang="ts">
	import { formatBytes, formatBytesOf, formatDuration } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	const info = $derived(system.info);
</script>

{#if info}
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
	dl {
		margin: 0;
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
