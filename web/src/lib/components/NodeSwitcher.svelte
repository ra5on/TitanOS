<script lang="ts">
	// Chooses the node of the cluster this tab manages (lib/cluster-node.ts).
	// Shown only in a cluster; nodes that are away or run another version
	// cannot be chosen.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { activeNode, switchNode } from '#lib/cluster-node.ts';
	import { clusterNodes, manageable } from '#lib/cluster.svelte.ts';

	const current = $derived(activeNode() ?? clusterNodes.nodes.find((n) => n.self)?.id ?? '');

	function choose(event: Event) {
		const id = (event.currentTarget as HTMLSelectElement).value;
		const node = clusterNodes.nodes.find((n) => n.id === id);
		if (!node || id === current) return;
		switchNode(node.self ? null : id);
	}
</script>

{#if clusterNodes.nodes.length > 1}
	<label class="switcher"
		><span>{t('cluster.node')}</span><select
			class="mono"
			value={current}
			onchange={choose}
			aria-label={t('cluster.switch')}
		>
			{#each clusterNodes.nodes as node (node.id)}
				<option value={node.id} disabled={!manageable(node, clusterNodes.version)}
					>{node.name}{node.self ? ` (${t('cluster.thisNode')})` : ''}{!node.online
						? ` – ${t('cluster.offline')}`
						: node.version !== clusterNodes.version && !node.self
							? ` – ${t('cluster.otherVersionShort')}`
							: ''}</option
				>
			{/each}
		</select></label
	>
{/if}

<style>
	.switcher {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	select {
		font: inherit;
		color: var(--text);
		background: transparent;
		border: 1px solid var(--border);
		border-radius: var(--radius-small, 4px);
		padding: 1px 4px;
	}
</style>
