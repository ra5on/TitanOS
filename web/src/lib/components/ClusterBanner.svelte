<script lang="ts">
	// While this tab manages another node of the cluster, a strip says so and
	// leads back. A node that left the cluster sends the tab back home.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { activeNode, switchNode } from '#lib/cluster-node.ts';
	import { clusterNodes, homeNode } from '#lib/cluster.svelte.ts';

	const id = activeNode();
	const node = $derived(id ? clusterNodes.nodes.find((n) => n.id === id) : undefined);

	$effect(() => {
		if (id && clusterNodes.loaded && !node) switchNode(null);
	});
</script>

{#if id && node}
	<div class="cluster-banner" role="status">
		<span>{t('cluster.managing', { name: node.name })}</span>
		<button type="button" class="btn small" onclick={() => switchNode(null)}
			>{t('cluster.back', { name: homeNode()?.name ?? '' })}</button
		>
	</div>
{/if}

<style>
	.cluster-banner {
		/* Only as wide as its text and button, not across the page. */
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 12px;
		width: fit-content;
		max-width: 100%;
		box-sizing: border-box;
		margin-bottom: 16px;
		padding: 4px 4px 4px 14px;
		font-size: var(--text-md);
		border: 1px solid var(--accent);
		border-radius: var(--radius-tile);
		background: color-mix(in srgb, var(--accent) 14%, transparent);
		color: var(--text);
		font-weight: 500;
	}
</style>
