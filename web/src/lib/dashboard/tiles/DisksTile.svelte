<script lang="ts">
	import { formatDiskBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';

	// The boot stick is not a data disk; it has its own line in the system tile.
	const disks = $derived((storage.status?.disks ?? []).filter((disk) => disk.role !== 'stick'));

	function dot(disk: (typeof disks)[number]): string {
		if (disk.memberState === 'faulty' || (disk.smart && !disk.smart.healthy)) return 'error';
		if (disk.memberState === 'rebuilding') return 'warn';
		return disk.role === 'member' && disk.memberState === 'active' ? 'ok' : '';
	}

	function label(disk: (typeof disks)[number]): string {
		const size = formatDiskBytes(disk.sizeBytes);
		const kind = t(`disk.${disk.kind}`);
		if (disk.role === 'member') return `${size} ${kind} · ${disk.volume}${disk.memberState && disk.memberState !== 'active' ? ` · ${t(`member.${disk.memberState}`)}` : ''}`;
		return `${size} ${kind} · ${t(disk.role === 'free' ? 'disk.unassigned' : 'disk.usedRole')}`;
	}
</script>

{#each disks as disk (disk.name)}
	<div class="disk">
		<span class="dot {dot(disk)}" aria-hidden="true"></span>
		<span class="mono dev">{disk.name}</span>
		<span class="muted what">{label(disk)}</span>
		<span class="mono temp">{disk.smart && disk.smart.temperatureC > 0 ? `${disk.smart.temperatureC} °C` : ''}</span>
	</div>
{:else}
	{#if storage.status}<p class="muted">{t('storage.noDisks')}</p>{/if}
{/each}

<style>
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
		width: 4.75rem;
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
		font-size: 0.8125rem;
	}
	p {
		font-size: 0.875rem;
	}
</style>
