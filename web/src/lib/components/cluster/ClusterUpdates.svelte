<script lang="ts">
	// The nodes of the cluster on the updates page of the managing node:
	// version, newer release, the state of "Update all nodes" while it runs.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { activeNode } from '#lib/cluster-node.ts';
	import { poll } from '#lib/poll.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { nodeUpdate, type SwitchNode, type UpdateRun } from '#lib/cluster.svelte.ts';
	import UpdateAllDialog from './UpdateAllDialog.svelte';

	let cluster = $state('');
	let nodes = $state<SwitchNode[]>([]);
	let available = $state<Record<string, string>>({});
	let run = $state<UpdateRun | null>(null);
	let dialog = $state(false);

	async function load() {
		try {
			const res = await api.get<{ nodes: SwitchNode[]; manages: boolean }>('/cluster/nodes');
			nodes = res.manages ? res.nodes : [];
			if (nodes.length && !cluster)
				cluster = (await api.get<{ cluster: { name?: string } }>('/cluster')).cluster.name ?? '';
			run = (await api.get<{ run: UpdateRun | null }>('/cluster/update')).run;
		} catch {
			nodes = [];
		}
	}

	async function loadAvailable() {
		for (const node of nodes) {
			if (!node.online) continue;
			nodeUpdate(node, activeNode())
				.then((st) => (available = { ...available, [node.id]: st.available }))
				.catch(() => {});
		}
	}

	onMount(() => {
		const stop = poll(load, { interval: 4000, immediate: false, skip: () => !run?.running });
		load().then(loadAvailable);
		return stop;
	});

	function runNode(id: string) {
		return run?.nodes.find((n) => n.id === id);
	}
</script>

{#if nodes.length > 1}
	<section class="tile cluster-updates" aria-labelledby="cluster-updates-title">
		<div class="head">
			<h2 class="tile-title" id="cluster-updates-title">
				{t('cluster.updatesOf', { name: cluster })}
			</h2>
			<button
				type="button"
				class="btn btn-primary small"
				disabled={!!run?.running}
				onclick={() => (dialog = true)}
				><Icon name="download" size={15} />{t('cluster.updateAll')}</button
			>
		</div>
		<div class="table-wrap">
			<table>
				<thead
					><tr
						><th>{t('cluster.updateNode')}</th><th>{t('cluster.updateNow')}</th><th
							>{t('cluster.updateNew')}</th
						><th>{t('cluster.updateState')}</th></tr
					></thead
				>
				<tbody>
					{#each nodes as node (node.id)}
						{@const r = runNode(node.id)}
						<tr>
							<td
								>{node.name}{#if node.self}<span class="chip">{t('cluster.thisNode')}</span
									>{/if}</td
							>
							<td class="mono small">{node.version ?? '–'}</td>
							<td class="mono small">{r?.to || available[node.id] || '–'}</td>
							<td class:error-text={r?.state === 'failed'}>
								{#if r}{t(`cluster.run.${r.state}` as MessageKey)}{#if r.error}: {r.error}{/if}{:else if !node.online}{t(
										'cluster.offline',
									)}{:else if available[node.id]}{t('cluster.updateAvailable')}{:else}<span
										class="ok-text">{t('cluster.updateCurrent')}</span
									>{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		{#if run?.running}<p class="muted small" role="status">
				{t(run.parallel ? 'cluster.runningParallel' : 'cluster.runningSerial')}
			</p>{/if}
	</section>
{/if}

{#if dialog}
	<UpdateAllDialog {nodes} onclose={() => (dialog = false)} onstarted={(next) => (run = next)} />
{/if}

<style>
	.cluster-updates {
		padding: var(--space-3) var(--space-4) var(--space-4);
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
		margin-bottom: 8px;
	}
	.head h2 {
		margin: 0 auto 0 0;
	}
	.table-wrap {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--text-md);
	}
	th {
		text-align: left;
		font-size: var(--text-sm);
		font-weight: 600;
		color: var(--muted);
		padding: 6px 10px;
		border-bottom: 1px solid var(--border);
	}
	td {
		padding: 8px 10px;
		border-bottom: 1px solid var(--border);
	}
	td .chip {
		margin-left: 8px;
	}
	.ok-text {
		color: var(--ok-text);
	}
	.error-text {
		color: var(--error-text);
	}
	.small {
		font-size: var(--text-sm);
	}
</style>
