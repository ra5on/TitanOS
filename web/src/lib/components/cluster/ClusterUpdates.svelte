<script lang="ts">
	// The nodes of the cluster on the updates page of the managing node:
	// version, newer release, the state of "Update all nodes" while it runs.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
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

<!-- PLAN 78-1: a plain heading with one row per node (no tile), so that it
     fits the rows of the updates page. -->
{#if nodes.length > 1}
	<SettingGroup title={t('cluster.updatesOf', { name: cluster })} id="cluster-updates">
		{#snippet tools()}
			<button
				type="button"
				class="btn btn-primary small"
				disabled={!!run?.running}
				onclick={() => (dialog = true)}
				><Icon name="download" size={15} />{t('cluster.updateAll')}</button
			>
		{/snippet}
		{#each nodes as node (node.id)}
			{@const r = runNode(node.id)}
			{@const next = r?.to || available[node.id]}
			<SettingRow icon="tailnet" title={node.name}>
				{#snippet badge()}
					{#if node.self}<span class="chip">{t('cluster.thisNode')}</span>{/if}
				{/snippet}
				{#snippet description()}
					{t('cluster.updateNow')}: <span class="mono">{node.version ?? '–'}</span>{#if next}{' '}
						· {t('cluster.updateNew')}: <span class="mono">{next}</span>{/if}
				{/snippet}
				{#snippet control()}
					{#if r}<span class:error-text={r.state === 'failed'}
							>{t(`cluster.run.${r.state}` as MessageKey)}{#if r.error}: {r.error}{/if}</span
						>{:else if !node.online}<span class="chip error">{t('cluster.offline')}</span
						>{:else if available[node.id]}<span class="chip info"
							>{t('cluster.updateAvailable')}</span
						>{:else}<span class="chip ok">{t('cluster.updateCurrent')}</span>{/if}
				{/snippet}
			</SettingRow>
		{/each}
		{#if run?.running}<p class="muted small" role="status">
				{t(run.parallel ? 'cluster.runningParallel' : 'cluster.runningSerial')}
			</p>{/if}
	</SettingGroup>
{/if}

{#if dialog}
	<UpdateAllDialog {nodes} onclose={() => (dialog = false)} onstarted={(next) => (run = next)} />
{/if}

<style>
	.error-text {
		color: var(--error-text);
	}
	.small {
		font-size: var(--text-sm);
	}
</style>
