<script lang="ts">
	// Peers of the NetBird network with the fields chosen for this tile. The
	// data comes from the dashboard, which asks the server only while it is shown.
	import { formatDateTime } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { TileOptions } from '../options.ts';
	import {
		NETBIRD_MARKER_FIELDS,
		visiblePeers,
		type NetbirdField,
		type NetbirdPeer,
		type NetbirdPeers,
	} from '../netbird.ts';
	let {
		options,
		value,
		error = '',
	}: { options: TileOptions; value: NetbirdPeers | null; error?: string } = $props();
	let showAll = $state(false);
	const peers = $derived(
		value ? visiblePeers(value.peers, options.items, options.runningOnly) : [],
	);
	const rows = $derived(
		options.netbirdFields.filter((field) => !NETBIRD_MARKER_FIELDS.includes(field)),
	);
	const stateKey = (state: string) => `netbird.states.${state}` as MessageKey;
	function stateLabel(text: string) {
		const label = t(stateKey(text));
		return label === stateKey(text) ? text : label;
	}
	function show(peer: NetbirdPeer, field: NetbirdField): string {
		switch (field) {
			case 'ipv4':
				return peer.ipv4 || '–';
			case 'ipv6':
				return peer.ipv6 || '–';
			case 'fqdn':
				return peer.fqdn || '–';
			case 'lastSeen':
				return peer.lastSeen ? formatDateTime(new Date(peer.lastSeen)) : '–';
			case 'connection':
				return peer.connection ? t(`netbird.peer.${peer.connection}`) : '–';
			case 'networks':
				return peer.networks.join(', ') || t('netbird.peer.noNetworks');
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
		{t(value.state === 'inactive' ? 'netbird.peer.inactive' : 'netbird.peer.unknown')}
	</p>
	<a class="link-note" href="/plugins">{t('nav.plugins')}</a>
{:else}
	{#if value.state !== 'Connected'}<p class="muted note">
			{t('netbird.peer.notConnected', { state: stateLabel(value.state) })}
		</p>{/if}
	{#if error}<p class="muted note">{error}</p>{/if}
	<ul class:grid={options.list === 'grid'}>
		{#each peers.slice(0, showAll ? peers.length : options.rows) as peer (peer.id)}<li
				class:self={peer.self && options.netbirdFields.includes('self')}
			>
				<div class="head">
					<span class="name" data-edit-part="value">{peer.name || peer.fqdn || '–'}</span>
					{#each options.netbirdFields.filter((f) => NETBIRD_MARKER_FIELDS.includes(f)) as marker}
						{#if marker === 'self' && peer.self}<span class="chip info"
								>{t('netbird.peer.self')}</span
							>{:else if marker === 'status'}<span class="chip" class:ok={peer.online === true}
								>{peer.online === null
									? t('netbird.peer.statusUnknown')
									: peer.online
										? t('netbird.peer.connected')
										: t('netbird.peer.notConnectedShort')}</span
							>{/if}
					{/each}
				</div>
				{#if rows.length}<dl class:unlabelled={!options.showLabels}>
						{#each rows as field}<div>
								{#if options.showLabels}<dt data-edit-part="labels">
										{t(`netbird.field.${field}`)}
									</dt>{/if}
								<dd class="mono">{show(peer, field)}</dd>
							</div>{/each}
					</dl>{/if}
			</li>{:else}<li class="muted">
				{value.peers.length ? t('netbird.peer.noneVisible') : t('netbird.peer.none')}
			</li>{/each}
	</ul>
	<div class="collection-footer">
		<span
			>{t('dashboard.visibleItems', {
				count: Math.min(peers.length, showAll ? peers.length : options.rows),
				total: peers.length,
			})}</span
		>{#if !showAll && peers.length > options.rows}<button
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
		font-size: var(--text-xs);
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
		font: var(--tile-value-weight, 400) var(--tile-value-size, var(--text-md))/1.35
			var(--tile-value-font);
		color: var(--tile-value-color, var(--text));
		overflow-wrap: anywhere;
		margin-right: auto;
		font-weight: 600;
	}
	.chip {
		font-size: var(--text-xs);
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
		font: var(--tile-label-weight, 400) var(--tile-label-size, var(--text-sm))
			var(--tile-label-font);
		color: var(--tile-label-color, var(--muted));
		text-align: var(--tile-label-align);
		overflow-wrap: anywhere;
	}
	dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
		font-size: var(--text-base);
		color: var(--tile-value-color, var(--text));
		text-align: var(--tile-align);
	}
	p {
		margin: 0;
	}
	.note,
	.link-note {
		font-size: var(--text-sm);
	}
	.note {
		margin-bottom: 8px;
	}
</style>
