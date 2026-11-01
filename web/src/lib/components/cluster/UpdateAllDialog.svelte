<script lang="ts">
	// "Update all nodes": the nodes with their version and the release found
	// on each; one after the other (this node last) or all at once
	// (POST /cluster/update). The run itself is shown by ClusterUpdates.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { activeNode } from '#lib/cluster-node.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { nodeUpdate, type UpdateRun } from '#lib/cluster.svelte.ts';

	export type UpdateNode = {
		id: string;
		name: string;
		self: boolean;
		online: boolean;
		version?: string;
	};
	type Props = {
		nodes: UpdateNode[];
		onclose: () => void;
		onstarted: (run: UpdateRun) => void;
	};
	let { nodes, onclose, onstarted }: Props = $props();

	type Row = UpdateNode & { available: string; kind: string; loading: boolean; failed: boolean };
	let rows = $state<Row[]>([]);
	let chosen = $state<string[]>([]);
	let parallel = $state(false);
	let busy = $state(false);
	let error = $state('');

	onMount(() => {
		rows = nodes.map((n) => ({ ...n, available: '', kind: '', loading: true, failed: false }));
		rows.forEach(async (row, i) => {
			try {
				const st = await nodeUpdate(row, activeNode());
				rows[i] = { ...rows[i], ...st, loading: false };
				if (st.available && !chosen.includes(row.id)) chosen = [...chosen, row.id];
			} catch {
				rows[i] = { ...rows[i], loading: false, failed: true };
			}
		});
	});

	function toggle(id: string, on: boolean) {
		chosen = on ? [...chosen, id] : chosen.filter((c) => c !== id);
	}

	async function start() {
		busy = true;
		error = '';
		try {
			const answer = await api.post<{ run: UpdateRun }>('/cluster/update', {
				nodes: chosen,
				parallel,
			});
			onstarted(answer.run);
			onclose();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('cluster.updateAllTitle')} wide {onclose} locked={busy}>
	<div class="table-wrap">
		<table class="nodes">
			<thead>
				<tr>
					<th><span class="sr-only">{t('cluster.updateChoose')}</span></th>
					<th>{t('cluster.updateNode')}</th>
					<th>{t('cluster.updateNow')}</th>
					<th>{t('cluster.updateNew')}</th>
					<th>{t('cluster.updateKind')}</th>
				</tr>
			</thead>
			<tbody>
				{#each rows as row (row.id)}
					<tr>
						<td
							><input
								type="checkbox"
								aria-label={row.name}
								checked={chosen.includes(row.id)}
								disabled={!row.online}
								onchange={(e) => toggle(row.id, e.currentTarget.checked)}
							/></td
						>
						<td
							>{row.name}{#if row.self}<span class="chip">{t('cluster.thisNode')}</span>{/if}</td
						>
						<td class="mono small">{row.version ?? '–'}</td>
						<td class="mono small">{row.loading ? '…' : row.available || '–'}</td>
						<td class="muted"
							>{#if !row.online}{t('cluster.offline')}{:else if row.loading}{t(
									'common.loading',
								)}{:else if row.failed}{t('cluster.updateUnknown')}{:else if row.available}{t(
									row.kind === 'app' ? 'cluster.updateKindApp' : 'cluster.updateKindSystem',
								)}{:else}{t('cluster.updateCurrent')}{/if}</td
						>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
	<fieldset class="order">
		<legend>{t('cluster.updateOrder')}</legend>
		<label
			><input type="radio" name="update-order" value={false} bind:group={parallel} /><span
				>{t('cluster.updateSerial')}<small>{t('cluster.updateSerialHint')}</small></span
			></label
		>
		<label
			><input type="radio" name="update-order" value={true} bind:group={parallel} /><span
				>{t('cluster.updateParallel')}<small>{t('cluster.updateParallelHint')}</small></span
			></label
		>
	</fieldset>
	<p class="muted small">{t('cluster.updateAllHint')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button
			type="button"
			class="btn btn-primary"
			disabled={busy || chosen.length === 0}
			onclick={start}>{t('cluster.updateStart', { count: chosen.length })}</button
		>
	</div>
</Dialog>

<style>
	.table-wrap {
		overflow-x: auto;
	}
	.nodes {
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
	.order {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin: 14px 0 6px;
		padding: 0;
		border: 0;
	}
	.order legend {
		font-weight: 600;
		margin-bottom: 6px;
		font-size: var(--text-md);
	}
	.order label {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		font-size: var(--text-md);
	}
	.order small {
		display: block;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.small {
		font-size: var(--text-sm);
	}
</style>
