<script lang="ts">
	import { tileOptions, type TileOptions } from '../options.ts';
	let { options = tileOptions(null, 'volumes') }: { options?: TileOptions } = $props();
	import { formatDiskBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';

	const volumes = $derived(
		(storage.status?.volumes ?? [])
			.filter((v) => options.items === null || options.items.includes(v.name))
			.slice(0, options.rows),
	);

	function tone(state: string): string {
		return state === 'ok'
			? 'ok'
			: state === 'degraded' || state === 'failed' || state === 'missing'
				? 'error'
				: 'warn';
	}
</script>

<div class="collection" class:grid={options.list === 'grid'}>
	{#each volumes as volume (volume.name)}
		{@const percent = volume.sizeBytes > 0 ? (100 * volume.usedBytes) / volume.sizeBytes : 0}
		<div class="volume">
			<div class="line">
				<span class="mono name">{volume.name}</span>
				<span class="chip">{t(`level.${volume.level}`)}</span>
				<span class="chip {tone(volume.state)}"
					>{t(`state.${volume.state}`)}{volume.sync
						? ` ${formatPercent(volume.sync.percent)}`
						: ''}</span
				>
				{#if options.details}<span class="muted detail"
						>{volume.members.length} × · {t(`fs.${volume.filesystem}`)}</span
					>{/if}
			</div>
			<div class="bar"><span style:width="{percent}%"></span></div>
			<div class="line small muted">
				<span
					>{volume.mounted
						? t('storage.usedOf', {
								used: formatDiskBytes(volume.usedBytes),
								total: formatDiskBytes(volume.sizeBytes),
							})
						: t('storage.notMounted')}</span
				>
				<span class="mono">{volume.mounted ? formatPercent(percent) : ''}</span>
			</div>
		</div>
	{:else}
		{#if storage.status}
			<div class="empty">
				<p>{t('volumes.empty')}</p>
				<p class="muted"><a href="/storage">{t('nav.storage')}</a></p>
			</div>
		{/if}
	{/each}
</div>

<style>
	.collection {
		display: flex;
		flex-direction: column;
		gap: var(--tile-row-gap, 10px);
	}
	.collection.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
		gap: var(--tile-row-gap, 10px);
	}
	.volume {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.volume + .volume {
		padding-top: 14px;
		border-top: 1px solid var(--panel-border);
	}
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 10px;
	}
	.name {
		font-size: 1rem;
		font-weight: 500;
	}
	.detail {
		margin-left: auto;
		font-size: 0.8125rem;
	}
	.small {
		justify-content: space-between;
		font-size: 0.8125rem;
	}
	.bar {
		height: 8px;
		border-radius: var(--radius-bar);
	}
	.empty {
		flex: 1 1 auto;
		display: flex;
		flex-direction: column;
		justify-content: center;
		gap: 4px;
		font-size: 0.875rem;
	}
</style>
