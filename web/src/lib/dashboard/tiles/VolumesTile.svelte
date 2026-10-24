<script lang="ts">
	import { tileOptions, type TileOptions } from '../options.ts';
	import { formatDiskBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import type { Volume } from '#lib/api.ts';
	let { options = tileOptions(null, 'volumes') }: { options?: TileOptions } = $props();
	let menu = $state<{ volume: Volume; x: number; y: number } | null>(null);
	const volumes = $derived(
		(storage.status?.volumes ?? [])
			.filter((v) => options.items === null || options.items.includes(v.name))
			.slice(0, options.rows),
	);
</script>

<table>
	<thead
		><tr
			><th>{t('dashboard.name')}</th><th>{t('dashboard.volumeState')}</th><th
				>{t('dashboard.vmUsage')}</th
			>{#if options.details}<th>{t('dashboard.used')}</th>{/if}</tr
		></thead
	><tbody
		>{#each volumes as v (v.name)}{@const percent =
				v.sizeBytes > 0 ? (100 * v.usedBytes) / v.sizeBytes : 0}<tr
				><td
					><button
						class="name"
						data-edit-part="value"
						aria-label={t('dashboard.menu', { name: v.name })}
						aria-haspopup="menu"
						onclick={(e) => {
							const r = e.currentTarget.getBoundingClientRect();
							menu = { volume: v, x: r.left, y: r.bottom };
						}}>{v.name}<Icon name="chevronDown" size={11} /></button
					></td
				><td
					><span class:ok={v.state === 'ok'} class:warn={v.state !== 'ok'}
						>{t(`level.${v.level}`)} · {t(`state.${v.state}`)}{v.sync
							? ' ' + formatPercent(v.sync.percent)
							: ''}</span
					></td
				><td
					><div
						class="track"
						role="progressbar"
						aria-label={v.name}
						aria-valuemin="0"
						aria-valuemax="100"
						aria-valuenow={v.mounted ? Math.min(100, percent) : undefined}
					>
						<span style:width="{v.mounted ? Math.min(100, percent) : 0}%"></span>
					</div></td
				>{#if options.details}<td class="mono"
						>{v.mounted
							? `${formatDiskBytes(v.usedBytes)} / ${formatDiskBytes(v.sizeBytes)}`
							: t('storage.notMounted')}</td
					>{/if}</tr
			>{:else}<tr
				><td colspan="4">{storage.status ? t('volumes.empty') : t('common.loading')}</td></tr
			>{/each}</tbody
	>
</table>
{#if menu}{@const v = menu.volume}<ContextMenu
		label={t('dashboard.menu', { name: v.name })}
		x={menu.x}
		y={menu.y}
		onclose={() => (menu = null)}
		><div class="menu-title">{v.name}</div>
		<a role="menuitem" href="/storage#volume-{encodeURIComponent(v.name)}"
			><Icon name="disk" size={16} /><span>{t('dashboard.showDetails')}</span></a
		><a role="menuitem" href="/storage?tab=disks#volume-{encodeURIComponent(v.name)}"
			><Icon name="list" size={16} /><span>{t('dashboard.volumeDrives')}</span></a
		><a role="menuitem" href="/tools/maintenance"
			><Icon name="tool" size={16} /><span>{t('nav.tools')}</span></a
		></ContextMenu
	>{/if}

<style>
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--tile-label-size, 12px);
		table-layout: auto;
	}
	th {
		font-weight: 400;
		color: var(--muted);
		text-align: left;
		padding: 0 9px 7px 0;
		font-size: var(--tile-label-size, 12px);
	}
	td {
		border-top: 1px solid var(--border);
		padding: 7px 9px 7px 0;
		overflow-wrap: anywhere;
	}
	td:last-child,
	th:last-child {
		text-align: right;
		padding-right: 0;
	}
	.name {
		display: flex;
		align-items: center;
		gap: 4px;
		border: 0;
		background: none;
		cursor: pointer;
		color: var(--tile-value-color, var(--text));
		padding: 0;
		font: var(--tile-value-weight) var(--tile-value-size, 14px) var(--tile-value-font);
		overflow-wrap: anywhere;
		text-align: left;
		white-space: normal;
	}
	.ok {
		color: var(--ok-text);
	}
	.warn {
		color: var(--warn-text);
	}
	.track {
		min-width: 36px;
		height: 6px;
		background: var(--track);
		overflow: hidden;
	}
	.track span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	.mono {
		font-size: var(--tile-label-size, 12px);
		font-family: var(--tile-value-font);
	}
</style>
