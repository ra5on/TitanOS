<script lang="ts">
	import { api } from '#lib/api.ts';
	import { formatBytes, formatBytesOf, formatDuration } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import HelpTip from '#lib/components/HelpTip.svelte';
	import { system } from '#lib/system.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import type { TileOptions } from '../options.ts';
	import type { SystemField } from '../system-fields.ts';
	let { options }: { options: TileOptions } = $props();
	const info = $derived(system.info);
	let update = $state<{
		lastCheck: string | null;
		checkError: string;
		available: { version: string } | null;
	} | null>(null);
	$effect(() => {
		if (
			!options.systemFields.includes('update') ||
			!['owner', 'admin'].includes(session.data?.user?.role ?? '')
		)
			return;
		let disposed = false;
		api
			.get<typeof update>('/update')
			.then((v) => {
				if (!disposed) update = v;
			})
			.catch(() => {});
		return () => {
			disposed = true;
		};
	});
	function value(field: SystemField): string {
		if (!info) return '–';
		switch (field) {
			case 'hostname':
				return info.hostname;
			case 'version':
				return `TitanOS ${info.version}`;
			case 'uptime':
				return formatDuration(info.uptimeSeconds);
			case 'mainboard':
				return info.inventory?.mainboard || '–';
			case 'bios':
				return info.inventory?.bios || '–';
			case 'cpu':
				return `${info.cpu.model} · ${info.cpu.threads} Threads`;
			case 'power':
				return info.cpu.powerWatts == null ? '–' : `${info.cpu.powerWatts.toFixed(1)} W`;
			case 'memory':
				return formatBytes(info.memory.totalBytes);
			case 'kernel':
				return info.kernel;
			case 'slot':
				return info.image?.slot?.toUpperCase() || '–';
			case 'ipv4':
				return info.network?.ipv4 || '–';
			case 'ipv6':
				return info.network?.ipv6Addresses?.join(', ') || '–';
			case 'link':
				return info.network
					? `${info.network.interface} · ${info.network.speedMbps ? info.network.speedMbps + ' Mbit/s' : '–'}`
					: '–';
			case 'drivers':
				return (
					(info.accelerators ?? []).map((d) => `${d.name}: ${d.driver || '–'}`).join(', ') || '–'
				);
			case 'update':
				return !update?.lastCheck || update.checkError
					? '–'
					: update.available
						? t('dashboard.updateAvailable', { version: update.available.version })
						: t('dashboard.upToDate');
			case 'image':
				return info.image ? formatBytes(info.image.sizeBytes) : '–';
			case 'overlay':
				return info.image
					? formatBytesOf(info.image.overlayUsedBytes, info.image.overlayTotalBytes)
					: '–';
			case 'stick':
				return info.stick.present ? info.stick.label : t('system.stickMissing');
		}
	}
</script>

{#if info}<dl
		class:stacked={options.systemLayout === 'stacked'}
		class:unlabelled={!options.showLabels}
	>
		{#each options.systemFields as field}<div>
				{#if options.showLabels}<dt data-edit-part="labels">
						{t(`dashboard.field.${field}`)}{#if field === 'slot'}<HelpTip term="slot" />{/if}
					</dt>{/if}
				<dd class:hostname={field === 'hostname'} data-edit-part="value">{value(field)}</dd>
			</div>{/each}
	</dl>{/if}

<style>
	dl {
		margin: 0;
		display: grid;
		gap: 6px;
		min-width: 0;
	}
	dl div {
		display: grid;
		grid-template-columns: minmax(95px, 0.65fr) minmax(0, 1.5fr);
		gap: 12px;
		align-items: baseline;
		min-width: 0;
	}
	dt {
		font: var(--tile-label-weight, 400) var(--tile-label-size, 12px) var(--tile-label-font);
		color: var(--tile-label-color, var(--muted));
		text-align: var(--tile-label-align);
		overflow-wrap: anywhere;
	}
	dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
		font: var(--tile-value-weight, 400) var(--tile-value-size, 14px)/1.45 var(--tile-value-font);
		color: var(--tile-value-color, var(--text));
		text-align: var(--tile-align);
	}
	.stacked div,
	.unlabelled div {
		grid-template-columns: minmax(0, 1fr);
		gap: 2px;
	}
</style>
