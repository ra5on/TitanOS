<script lang="ts">
	// Devices of the tailnet with the fields chosen for this tile. The data
	// comes from the dashboard, which asks the server only while it is shown.
	import { formatDateTime } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { TileOptions } from '../options.ts';
	import {
		MARKER_FIELDS,
		visibleDevices,
		type TailnetDevice,
		type TailnetDevices,
		type TailscaleField,
	} from '../tailscale.ts';
	let {
		options,
		value,
		error = '',
	}: { options: TileOptions; value: TailnetDevices | null; error?: string } = $props();
	let showAll = $state(false);
	const devices = $derived(
		value ? visibleDevices(value.devices, options.items, options.runningOnly) : [],
	);
	const rows = $derived(options.tailscaleFields.filter((field) => !MARKER_FIELDS.includes(field)));
	const stateKey = (state: string) => `tailscale.states.${state}` as MessageKey;
	function stateLabel(text: string) {
		const label = t(stateKey(text));
		return label === stateKey(text) ? text : label;
	}
	function show(device: TailnetDevice, field: TailscaleField): string {
		switch (field) {
			case 'ipv4':
				return device.ipv4.join(', ') || '–';
			case 'ipv6':
				return device.ipv6.join(', ') || '–';
			case 'hostName':
				return device.hostName || '–';
			case 'dnsName':
				return device.dnsName || '–';
			case 'lastSeen':
				return device.lastSeen ? formatDateTime(new Date(device.lastSeen)) : '–';
			case 'os':
				return device.os || '–';
			case 'exitNode':
				return t(
					device.exitNode
						? 'tailscale.device.exitInUse'
						: device.exitNodeOption
							? 'tailscale.device.exitOffered'
							: 'tailscale.device.exitNo',
				);
			case 'routes':
				return device.routes.join(', ') || t('tailscale.device.noRoutes');
			default:
				return '';
		}
	}
</script>

{#if error && !value}
	<p class="muted">{error}</p>
{:else if !value}
	<p class="muted">{t('common.loading')}</p>
{:else if !value.active}
	<p class="muted">
		{t(value.state === 'inactive' ? 'tailscale.device.inactive' : 'tailscale.device.unknown')}
	</p>
	<a class="link-note" href="/plugins">{t('nav.plugins')}</a>
{:else}
	{#if value.state !== 'Running'}<p class="muted note">
			{t('tailscale.device.notRunning', { state: stateLabel(value.state) })}
		</p>{/if}
	{#if error}<p class="muted note">{error}</p>{/if}
	<ul class:grid={options.list === 'grid'}>
		{#each devices.slice(0, showAll ? devices.length : options.rows) as device (device.id)}<li
				class:self={device.self && options.tailscaleFields.includes('self')}
			>
				<div class="head">
					<span class="name" data-edit-part="value">{device.name || device.hostName || '–'}</span>
					{#each options.tailscaleFields.filter((f) => MARKER_FIELDS.includes(f)) as marker}
						{#if marker === 'self' && device.self}<span class="chip info"
								>{t('tailscale.device.self')}</span
							>{:else if marker === 'status'}<span class="chip" class:ok={device.online === true}
								>{device.online === null
									? t('tailscale.device.statusUnknown')
									: device.online
										? t('dashboard.online')
										: t('tailscale.offline')}</span
							>{/if}
					{/each}
				</div>
				{#if rows.length}<dl class:unlabelled={!options.showLabels}>
						{#each rows as field}<div>
								{#if options.showLabels}<dt data-edit-part="labels">
										{t(`tailscale.field.${field}`)}
									</dt>{/if}
								<dd class="mono">{show(device, field)}</dd>
							</div>{/each}
					</dl>{/if}
			</li>{:else}<li class="muted">
				{value.devices.length ? t('tailscale.device.noneVisible') : t('tailscale.device.none')}
			</li>{/each}
	</ul>
	<div class="collection-footer">
		<span
			>{t('dashboard.visibleItems', {
				count: Math.min(devices.length, showAll ? devices.length : options.rows),
				total: devices.length,
			})}</span
		>{#if !showAll && devices.length > options.rows}<button
				class="link"
				onclick={() => (showAll = true)}>{t('dashboard.showAll')}</button
			>{/if}
	</div>
{/if}

<style>
	.collection-footer {
		display: flex;
		gap: 10px;
		flex-wrap: wrap;
		color: var(--muted);
		font-size: 11px;
		margin-top: 10px;
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: var(--tile-row-gap, 10px);
		min-width: 0;
	}
	ul.grid {
		display: grid;
		grid-template-columns: repeat(
			auto-fill,
			minmax(min(var(--tile-entry-width, 170px), 100%), 1fr)
		);
		gap: var(--tile-gap, 10px);
	}
	li {
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	li + li {
		border-top: 1px solid var(--border);
		padding-top: var(--tile-row-gap, 10px);
	}
	ul.grid li + li {
		border-top: 0;
		padding-top: 0;
	}
	li.self .name {
		color: var(--accent);
	}
	.head {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 6px;
		min-width: 0;
	}
	.name {
		font: var(--tile-value-weight, 400) var(--tile-value-size, 14px)/1.35 var(--tile-value-font);
		color: var(--tile-value-color, var(--text));
		overflow-wrap: anywhere;
		margin-right: auto;
		font-weight: 600;
	}
	.chip {
		font-size: 0.6875rem;
	}
	dl {
		margin: 0;
		display: grid;
		gap: 2px;
		min-width: 0;
	}
	dl div {
		display: grid;
		grid-template-columns: minmax(80px, 0.6fr) minmax(0, 1.5fr);
		gap: 10px;
		align-items: baseline;
		min-width: 0;
	}
	dl.unlabelled div {
		grid-template-columns: minmax(0, 1fr);
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
		font-size: 0.8125rem;
		color: var(--tile-value-color, var(--text));
		text-align: var(--tile-align);
	}
	p {
		margin: 0;
	}
	.note,
	.link-note {
		font-size: 0.75rem;
	}
	.note {
		margin-bottom: 8px;
	}
</style>
