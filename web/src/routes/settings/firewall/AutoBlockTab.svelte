<script lang="ts">
	// The automatic blocking: saved on its own and applied at once (no
	// trial), with the list of blocked senders.
	import { api, type FirewallAutoBlock, type FirewallStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { validSource } from '#lib/firewall.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import { parseNetworks } from '#lib/ssh-networks.ts';

	let { status, ontake }: { status: FirewallStatus; ontake: (next: FirewallStatus) => void } =
		$props();

	let edited = $state(false);
	let draft = $state<FirewallAutoBlock | null>(null);
	let exemptText = $state('');
	let busy = $state(false);
	let error = $state('');
	let notice = $state('');
	$effect(() => {
		const saved = status.config.autoBlock;
		if (!edited) {
			draft = { ...saved, exempt: [...saved.exempt] };
			exemptText = saved.exempt.join('\n');
		}
	});
	const exemptList = $derived(parseNetworks(exemptText));
	const exemptValid = $derived(exemptList.every(validSource));

	async function save() {
		if (!draft || busy) return;
		busy = true;
		error = notice = '';
		try {
			const body = { ...$state.snapshot(draft), exempt: exemptList };
			const next = await api.put<FirewallStatus>('/firewall/autoblock', body);
			edited = false;
			ontake(next);
			notice = t('ui.saved');
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	let releasing = $state<string | null>(null);
	let releaseBusy = $state(false);
	let releaseError = $state('');
	async function release() {
		if (!releasing || releaseBusy) return;
		releaseBusy = true;
		releaseError = '';
		try {
			ontake(await api.post<FirewallStatus>('/firewall/unblock', { address: releasing }));
			releasing = null;
		} catch (err) {
			releaseError = apiErrorText(err);
		} finally {
			releaseBusy = false;
		}
	}
	const sourceLabel = (source: string) => t(`firewall.source.${source}` as MessageKey);
</script>

{#if draft}<form
		class="tile panel"
		aria-labelledby="firewall-autoblock"
		onsubmit={(event) => {
			event.preventDefault();
			void save();
		}}
		oninput={() => {
			edited = true;
			notice = '';
		}}
	>
		<h2 class="tile-title" id="firewall-autoblock" use:revealTarget>{t('firewall.autoBlock')}</h2>
		<label class="check"
			><input type="checkbox" bind:checked={draft.enabled} /><span
				>{t('firewall.autoBlockEnable')}</span
			></label
		>
		<p class="muted">{t('firewall.autoBlockText')}</p>
		<div class="numbers">
			<label class="stack"
				><span>{t('firewall.attempts')}</span><input
					class="field"
					type="number"
					min="2"
					max="100"
					bind:value={draft.attempts}
					disabled={!draft.enabled}
				/></label
			>
			<label class="stack"
				><span>{t('firewall.within')}</span><input
					class="field"
					type="number"
					min="1"
					max="1440"
					bind:value={draft.within}
					disabled={!draft.enabled}
				/></label
			>
			<label class="stack"
				><span>{t('firewall.blockMinutes')}</span><input
					class="field"
					type="number"
					min="1"
					max="525600"
					bind:value={draft.minutes}
					disabled={!draft.enabled || draft.permanent}
				/></label
			>
		</div>
		<label class="check"
			><input type="checkbox" bind:checked={draft.permanent} disabled={!draft.enabled} /><span
				>{t('firewall.permanent')}</span
			></label
		>
		<label class="check"
			><input type="checkbox" bind:checked={draft.exemptLocal} disabled={!draft.enabled} /><span
				>{t('firewall.exemptLocal')}</span
			></label
		>
		<label class="stack"
			><span>{t('firewall.exempt')}</span><textarea
				class="field mono"
				rows="3"
				spellcheck="false"
				placeholder="203.0.113.0/24"
				bind:value={exemptText}
				disabled={!draft.enabled}></textarea></label
		>
		{#if !exemptValid}<p class="notice-warn" role="alert">{t('firewall.ruleSource')}</p>{/if}
		<p class="muted small">{t('firewall.autoBlockSources')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button class="btn btn-primary" disabled={busy || !edited || !exemptValid}
				>{t('common.save')}</button
			>
			{#if notice}<span class="muted" role="status">{notice}</span>{/if}
		</div>

		<h3 class="divided">{t('firewall.blocked')}</h3>
		{#if status.blocked.length}<div class="table-wrap">
				<table class="data-table">
					<thead
						><tr
							><th>{t('firewall.address')}</th><th>{t('firewall.blockSource')}</th><th
								>{t('firewall.since')}</th
							><th>{t('firewall.until')}</th><th></th></tr
						></thead
					>
					<tbody>
						{#each status.blocked as block (block.address)}
							<tr>
								<td class="mono">{block.address}</td>
								<td>{sourceLabel(block.source)} ({block.attempts})</td>
								<td>{formatDateTime(new Date(block.since))}</td>
								<td
									>{block.until ? formatDateTime(new Date(block.until)) : t('firewall.forever')}</td
								>
								<td class="actions"
									><button
										class="btn small"
										type="button"
										onclick={() => (releasing = block.address)}>{t('firewall.release')}</button
									></td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>{:else}<p class="muted">{t('firewall.noBlocked')}</p>{/if}
	</form>{/if}

{#if releasing}
	<Dialog
		title={t('firewall.releaseTitle')}
		locked={releaseBusy}
		onclose={() => (releasing = null)}
	>
		<p>{t('firewall.releaseText', { address: releasing })}</p>
		{#if releaseError}<p class="notice-error" role="alert">{releaseError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={releaseBusy} onclick={() => (releasing = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" disabled={releaseBusy} onclick={release}
				>{t('firewall.release')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.numbers {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
	}
	.numbers .stack {
		flex: 1 1 10rem;
	}
	.small {
		font-size: 0.8125rem;
	}
</style>
