<script lang="ts">
	import { tileOptions, type TileOptions } from '../options.ts';
	let { options = tileOptions(null, 'disks') }: { options?: TileOptions } = $props();
	import { formatDiskBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';

	// The boot stick is not a data disk; it has its own line in the system tile.
	let showAll = $state(false);
	const disks = $derived(
		(storage.status?.disks ?? []).filter(
			(disk) =>
				disk.role !== 'stick' &&
				(options.items === null ||
					options.items.includes(disk.identity || disk.serial || disk.name)),
		),
	);

	function dot(disk: (typeof disks)[number]): string {
		if (disk.memberState === 'faulty' || (disk.smart && !disk.smart.healthy)) return 'error';
		if (disk.memberState === 'rebuilding') return 'warn';
		return disk.role === 'member' && disk.memberState === 'active' ? 'ok' : '';
	}

	function label(disk: (typeof disks)[number]): string {
		const size = formatDiskBytes(disk.sizeBytes);
		const kind = t(`disk.${disk.kind}`);
		if (disk.role === 'member')
			return `${size} ${kind} · ${disk.volume}${disk.memberState && disk.memberState !== 'active' ? ` · ${t(`member.${disk.memberState}`)}` : ''}`;
		return `${size} ${kind} · ${t(disk.role === 'free' ? 'disk.unassigned' : 'disk.usedRole')}`;
	}
</script>

<div class="collection" class:grid={options.list === 'grid'}>
	{#each disks.slice(0, showAll ? disks.length : options.rows) as disk (disk.name)}
		<div class="disk">
			<span class="dot {dot(disk)}" aria-hidden="true"></span>
			<a
				class="mono dev"
				href="/storage#disk-{encodeURIComponent(disk.name)}"
				data-edit-part="value">{disk.name}</a
			>
			{#if options.details}<span class="muted what">{label(disk)}</span>{/if}
			<span class="mono temp" data-edit-part="value"
				>{disk.smart && disk.smart.temperatureC > 0 ? `${disk.smart.temperatureC} °C` : ''}</span
			>
		</div>
	{:else}
		{#if storage.status}<p class="muted">{t('storage.noDisks')}</p>{/if}
	{/each}
</div>

<div class="collection-footer">
	<span
		>{t('dashboard.visibleItems', {
			count: Math.min(disks.length, showAll ? disks.length : options.rows),
			total: disks.length,
		})}</span
	>{#if !showAll && disks.length > options.rows}<button
			class="link"
			onclick={() => (showAll = true)}>{t('dashboard.showAll')}</button
		>{/if}
</div>

<style>
	.collection-footer {
		display: flex;
		gap: 10px;
		flex-wrap: wrap;
		color: var(--muted);
		font-size: 11px;
		margin-top: 10px;
	}
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
	.disk {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: var(--row-pad) 0;
		border-top: 1px solid var(--panel-border);
		font-size: 0.875rem;
	}
	.disk:first-child {
		border-top: 0;
		padding-top: 0;
	}
	.dev {
		width: auto;
		min-width: 4rem;
		max-width: 35%;
		overflow-wrap: anywhere;
		font-size: var(--tile-value-size, 16px);
		font-family: var(--tile-value-font);
		font-weight: var(--tile-value-weight);
		flex: none;
		font-weight: 500;
	}
	.what {
		flex: 1 1 auto;
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.temp {
		font-size: var(--tile-value-size, 16px);
	}
	p {
		font-size: 0.875rem;
	}
</style>
