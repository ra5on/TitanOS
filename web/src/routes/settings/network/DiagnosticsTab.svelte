<script lang="ts">
	// Checks link, addresses, gateways and name resolution of a network the
	// NAS runs. A draft is only checkable after "Test" put it in place.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { NASNetwork } from '#lib/network.ts';

	let {
		networks,
		pending,
		selected = $bindable(''),
	}: { networks: NASNetwork[]; pending: boolean; selected?: string } = $props();

	type Check = { name: string; ok: boolean; detail: string };
	let checks = $state<Check[]>([]),
		busy = $state(false),
		error = $state(''),
		checked = $state('');

	async function run() {
		if (!selected) return;
		busy = true;
		error = '';
		checked = selected;
		try {
			checks = (await api.post<{ checks: Check[] }>('/network/diagnostics', { network: selected }))
				.checks;
		} catch (e) {
			checks = [];
			error = apiErrorText(e, { detail: true });
		} finally {
			busy = false;
		}
	}
	// Opened from a network's menu: check that one right away.
	$effect(() => {
		if (selected && selected !== checked && !busy && networks.some((n) => n.id === selected))
			void run();
	});
</script>

<section class="tile panel" aria-labelledby="net-diagnostics">
	<h2 class="tile-title" id="net-diagnostics">{t('netx.tab.diagnostics')}</h2>
	{#if pending}<p class="notice-warn">{t('netx.diagnosticsPending')}</p>{/if}
	<div class="row">
		<label class="stack"
			><span>{t('netx.network')}</span><select
				class="field"
				bind:value={selected}
				disabled={pending}
				><option value="">{t('netx.choose')}</option>{#each networks as n (n.id)}<option
						value={n.id}>{n.name}</option
					>{/each}</select
			></label
		>
		<div class="row-actions">
			<button class="btn" disabled={busy || pending || !selected} onclick={run}
				>{t('netx.check')}</button
			>
		</div>
	</div>
	<p class="muted">{t('netx.diagnosticsDraft')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if checks.length}
		<div class="table-wrap">
			<table class="data-table">
				<tbody>
					{#each checks as check, i (i)}
						<tr>
							<td
								><span class="chip" class:ok={check.ok} class:warn={!check.ok}
									>{check.ok ? t('netx.ok') : t('netx.warning')}</span
								></td
							>
							<td
								><strong>{check.name}</strong>
								<pre class="mono muted">{check.detail}</pre></td
							>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>

<style>
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 10px 16px;
	}
	.row .stack {
		flex: 1 1 16rem;
	}
	pre {
		margin: 4px 0 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: var(--text-sm);
	}
	td:first-child {
		width: 1%;
		vertical-align: top;
	}
</style>
