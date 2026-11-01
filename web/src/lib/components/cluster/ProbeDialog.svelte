<script lang="ts">
	// "Check ways": every other node asked on each of its addresses, with
	// whether it answered and how fast (POST /cluster/probe).
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { wayLabel, type NodeProbes } from '#lib/cluster.svelte.ts';

	let { onclose }: { onclose: () => void } = $props();

	let nodes = $state<NodeProbes[] | null>(null);
	let checked = $state('');
	let busy = $state(false);
	let error = $state('');

	async function check() {
		busy = true;
		error = '';
		try {
			const answer = await api.post<{ nodes: NodeProbes[]; checked: string }>('/cluster/probe');
			nodes = answer.nodes;
			checked = answer.checked;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	onMount(check);
</script>

<Dialog title={t('cluster.probeTitle')} wide {onclose}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if !nodes}
		<p class="muted" role="status">{t('cluster.probing')}</p>
	{:else if nodes.length === 0}
		<p class="muted">{t('cluster.probeNone')}</p>
	{:else}
		{#each nodes as node (node.id)}
			<h3 class="node">{node.name}</h3>
			<div class="table-wrap">
				<table class="probes">
					<thead
						><tr
							><th>{t('cluster.probeWay')}</th><th>{t('cluster.probeAddress')}</th><th
								>{t('cluster.probeReached')}</th
							><th>{t('cluster.probeTime')}</th></tr
						></thead
					>
					<tbody>
						{#each node.probes as p (p.address)}
							<tr class:used={p.used}>
								<td
									>{wayLabel(p.way)}
									{#if p.used}<span class="chip info">{t('cluster.probeUsed')}</span>{/if}</td
								>
								<td class="mono small">{p.address}</td>
								<td class={p.ok ? 'ok-text' : 'error-text'}
									>{p.ok ? '✓ ' + t('cluster.probeYes') : '✕ ' + t('cluster.probeNo')}</td
								>
								<td class="mono small"
									>{p.ok
										? p.millis + ' ms'
										: t(`cluster.probeProblem.${p.problem ?? 'failed'}` as MessageKey)}</td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/each}
		<p class="muted small">
			{t('cluster.probeChecked', { time: formatTime(new Date(checked)) })}
		</p>
	{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.close')}</button>
		<button type="button" class="btn btn-primary" disabled={busy} onclick={check}
			>{t('cluster.probeAgain')}</button
		>
	</div>
</Dialog>

<style>
	.node {
		margin: 12px 0 4px;
		font-size: var(--text-md);
	}
	.table-wrap {
		overflow-x: auto;
	}
	.probes {
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
		white-space: nowrap;
	}
	tr.used td {
		background: color-mix(in srgb, var(--accent) 7%, transparent);
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
