<script lang="ts">
	import { formatBytes, formatBytesOf, formatDuration } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	let { details = true }: { details?: boolean } = $props();
	const info = $derived(system.info);
</script>

{#if info}
	<strong class="hostname" data-edit-part="value">{info.hostname}</strong>
	<dl data-edit-part="labels">
		<div>
			<dt>{t('about.version')}</dt>
			<dd>{info.version}</dd>
		</div>
		<div>
			<dt>{t('dashboard.systemSlot')}</dt>
			<dd>{info.image?.slot?.toUpperCase() || '–'}</dd>
		</div>
		{#if details}<div>
				<dt>{t('system.inRam')}</dt>
				<dd class="mono">{info.image ? formatBytes(info.image.sizeBytes) : '–'}</dd>
			</div>
			<div>
				<dt>{t('system.overlay')}</dt>
				<dd class="mono">
					{info.image
						? formatBytesOf(info.image.overlayUsedBytes, info.image.overlayTotalBytes)
						: '–'}
				</dd>
			</div>{/if}
		{#if details}<div>
				<dt>{t('system.uptime')}</dt>
				<dd class="mono">{formatDuration(info.uptimeSeconds)}</dd>
			</div>
			<div>
				<dt>{t('system.stick')}</dt>
				<dd class="mono">
					{info.stick.present
						? t('system.stickOk', { label: info.stick.label })
						: t('system.stickMissing')}
				</dd>
			</div>
		{/if}
	</dl>
{/if}

<style>
	.hostname {
		font-size: var(--tile-value-size, 28px);
		font-weight: var(--tile-value-weight, 500);
		font-family: var(--tile-value-font);
		color: var(--tile-value-color, var(--text));
		text-align: var(--tile-align);
		overflow-wrap: anywhere;
	}
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
