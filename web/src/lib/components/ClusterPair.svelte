<script lang="ts">
	// Gets the pairing code for backups to another node of the cluster
	// (POST /cluster/nodes/{id}/backup-pair): that node creates the source,
	// the data goes through the cluster. Hidden outside a cluster and while
	// the tab manages another node.
	import { api } from '#lib/api.ts';
	import { activeNode } from '#lib/cluster-node.ts';
	import { clusterNodes } from '#lib/cluster.svelte.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	let { onpair }: { onpair: (code: string) => void } = $props();

	const others = $derived(activeNode() ? [] : clusterNodes.nodes.filter((n) => !n.self));
	let node = $state('');
	let busy = $state(false);
	let error = $state('');

	async function pair() {
		if (!node) return;
		busy = true;
		error = '';
		try {
			const res = await api.post<{ code: string }>(
				`/cluster/nodes/${encodeURIComponent(node)}/backup-pair`,
			);
			onpair(res.code);
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

{#if others.length}
	<div class="pair">
		<label
			><span>{t('cluster.pairNode')}</span><select class="field" bind:value={node} disabled={busy}>
				<option value="">{t('cluster.pairNone')}</option>
				{#each others as n (n.id)}<option value={n.id} disabled={!n.online}>{n.name}</option>{/each}
			</select></label
		>
		<button type="button" class="btn" disabled={busy || !node} onclick={pair}
			>{t('cluster.pairButton')}</button
		>
	</div>
	<p class="muted small">{t('cluster.pairHint')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{/if}

<style>
	.pair {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 8px 12px;
	}
	.pair label {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
</style>
