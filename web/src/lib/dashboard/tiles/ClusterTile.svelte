<script lang="ts">
	// The nodes of the cluster on the dashboard: reachable, version, the way
	// they are reached on, load and space (GET /cluster, from the overview
	// the node keeps for a few seconds).
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { otherVersion, wayLabel, type ClusterPage, type NodeView } from '#lib/cluster.svelte.ts';
	import type { TileOptions } from '../options.ts';

	let {
		options,
		value,
		error = '',
	}: { options: TileOptions; value: ClusterPage | null; error?: string } = $props();

	const nodes = $derived(
		(value?.nodes ?? []).filter((n) => options.items === null || options.items.includes(n.id)),
	);

	/** Version, way, load and space in one line. */
	function meta(n: NodeView): string {
		if (!n.online) return t('cluster.offline');
		const parts = [n.status?.version ?? ''];
		if (!n.self && n.way) parts.push(wayLabel(n.way));
		if (n.status?.cpuPercent != null) parts.push(`CPU ${Math.round(n.status.cpuPercent)} %`);
		parts.push(space(n));
		return parts.filter(Boolean).join(' · ');
	}

	function space(n: NodeView): string {
		const vols = n.status?.volumes ?? [];
		const size = vols.reduce((sum, v) => sum + v.sizeBytes, 0);
		if (!size) return '';
		const used = vols.reduce((sum, v) => sum + v.usedBytes, 0);
		return t('tile.clusterFree', { free: formatBytes(size - used) });
	}
</script>

{#if error && !value}
	<p class="muted">{error}</p>
{:else if !value}
	<p class="muted">{t('common.loading')}</p>
{:else if !value.cluster.name}
	<p class="muted">{t('tile.clusterNone')}</p>
	<a class="link-note" href="/settings/cluster">{t('cluster.title')}</a>
{:else}
	<ul>
		{#each nodes as node (node.id)}
			<li>
				<span
					class="dot"
					class:warn={node.online &&
						(node.fallback || node.clockSkew || otherVersion(node, value.cluster.version))}
					class:off={!node.online}
					aria-hidden="true"
				></span>
				<span class="name" data-edit-part="value">{node.name}</span>
				<span class="meta mono">{meta(node)}</span>
			</li>
		{/each}
	</ul>
	<div class="collection-footer">
		<a class="link" href="/settings/cluster">{t('cluster.title')}</a>
	</div>
{/if}

<style>
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: var(--tile-row-gap, 8px);
		min-width: 0;
	}
	li {
		display: grid;
		grid-template-columns: 10px auto minmax(0, 1fr);
		align-items: baseline;
		gap: 8px;
		min-width: 0;
	}
	li + li {
		border-top: 1px solid var(--border);
		padding-top: var(--tile-row-gap, 8px);
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--status-ok);
		align-self: center;
	}
	.dot.warn {
		background: var(--status-warn);
	}
	.dot.off {
		background: var(--status-inactive);
	}
	.name {
		font: var(--tile-value-weight, 400) var(--tile-value-size, var(--text-md))/1.35
			var(--tile-value-font);
		color: var(--tile-value-color, var(--text));
		font-weight: 600;
	}
	.meta {
		color: var(--tile-label-color, var(--muted));
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
		text-align: var(--tile-align);
	}
	.collection-footer {
		display: flex;
		gap: 10px;
		flex-wrap: wrap;
		color: var(--muted);
		font-size: var(--text-xs);
		margin-top: 10px;
	}
	.collection-footer .link {
		margin-left: auto;
	}
	p {
		margin: 0;
	}
</style>
