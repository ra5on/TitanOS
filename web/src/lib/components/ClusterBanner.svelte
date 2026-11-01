<script lang="ts">
	// While this tab manages another node of the cluster, a strip says so and
	// leads back. A node of another version is managed as well; the strip
	// names both versions and leads to its updates. A node that left the
	// cluster sends the tab back home.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { activeNode, switchNode } from '#lib/cluster-node.ts';
	import { clusterNodes, compareVersions, homeNode, otherVersion } from '#lib/cluster.svelte.ts';
	import { onNodeRestart } from '#lib/node-restart.ts';

	const id = activeNode();
	// A request waits for the node while it starts again (app update).
	let restarting = $state(false);
	$effect(() => onNodeRestart((value) => (restarting = value)));
	const node = $derived(id ? clusterNodes.nodes.find((n) => n.id === id) : undefined);
	const differs = $derived(!!node && otherVersion(node, clusterNodes.version));
	const older = $derived(differs && compareVersions(node?.version ?? '', clusterNodes.version) < 0);

	$effect(() => {
		if (id && clusterNodes.loaded && !node) switchNode(null);
	});
</script>

{#if id && node}
	<div class="cluster-banner" class:differs role="status">
		<span>{t('cluster.managing', { name: node.name })}</span>
		{#if differs}<span class="versions"
				>{t(node.compatible === false ? 'cluster.versionsLimited' : 'cluster.versions', {
					name: node.name,
					version: node.version ?? '',
					own: clusterNodes.version,
				})}</span
			>{/if}
		{#if restarting}<span class="restarting">{t('cluster.restarting')}</span>{/if}
		{#if older}<a class="btn small btn-primary" href="/settings/updates"
				>{t('cluster.updateNamed', { name: node.name })}</a
			>{/if}
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
	.restarting,
	.versions {
		font-weight: 400;
	}
	.differs {
		border-color: var(--warn-border);
		background: var(--warn-bg);
	}
</style>
