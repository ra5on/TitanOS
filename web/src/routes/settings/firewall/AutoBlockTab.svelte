<script lang="ts">
	// The automatic blocking: saved on its own and applied at once (no
	// trial), with the list of blocked senders.
	import { api, type FirewallAutoBlock, type FirewallStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { validSource } from '#lib/firewall.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
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

{#if draft}
	{@const d = draft}
	<!-- PLAN 78-1: every value is a row; one Save for all of them (one request,
	     and the list of exempt networks is typed text). -->
	<form
		class="rows"
		onsubmit={(event) => {
			event.preventDefault();
			void save();
		}}
		oninput={() => {
			edited = true;
			notice = '';
		}}
	>
		<SettingGroup title={t('firewall.autoBlock')} id="firewall-autoblock">
			<SettingRow
				icon="shield"
				title={t('firewall.autoBlockEnable')}
				desc={t('firewall.autoBlockText')}
				for="fw-ab-enabled"
			>
				{#snippet control()}
					<input
						id="fw-ab-enabled"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={d.enabled}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="key" title={t('firewall.attempts')} for="fw-ab-attempts">
				{#snippet control()}
					<input
						id="fw-ab-attempts"
						class="field"
						type="number"
						min="2"
						max="100"
						bind:value={d.attempts}
						disabled={!d.enabled}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="clock" title={t('firewall.within')} for="fw-ab-within">
				{#snippet control()}
					<input
						id="fw-ab-within"
						class="field"
						type="number"
						min="1"
						max="1440"
						bind:value={d.within}
						disabled={!d.enabled}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="lock" title={t('firewall.blockMinutes')} for="fw-ab-minutes">
				{#snippet control()}
					<input
						id="fw-ab-minutes"
						class="field"
						type="number"
						min="1"
						max="525600"
						bind:value={d.minutes}
						disabled={!d.enabled || d.permanent}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="lock" title={t('firewall.permanent')} for="fw-ab-permanent">
				{#snippet control()}
					<input
						id="fw-ab-permanent"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={d.permanent}
						disabled={!d.enabled}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="home" title={t('firewall.exemptLocal')} for="fw-ab-local">
				{#snippet control()}
					<input
						id="fw-ab-local"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={d.exemptLocal}
						disabled={!d.enabled}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="list" title={t('firewall.exempt')} for="fw-ab-exempt">
				{#snippet control()}
					<textarea
						id="fw-ab-exempt"
						class="field mono"
						rows="3"
						spellcheck="false"
						placeholder="203.0.113.0/24"
						bind:value={exemptText}
						disabled={!d.enabled}></textarea>
				{/snippet}
			</SettingRow>
			{#if !exemptValid}<p class="notice-warn" role="alert">{t('firewall.ruleSource')}</p>{/if}
			<p class="muted small">{t('firewall.autoBlockSources')}</p>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={busy || !edited || !exemptValid}
					>{t('common.save')}</button
				>
				{#if notice}<span class="muted" role="status">{notice}</span>{/if}
			</div>
		</SettingGroup>
	</form>

	<!-- The blocked senders stay a table. -->
	<SettingGroup title={t('firewall.blocked')} id="firewall-blocked">
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
	</SettingGroup>
{/if}

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
	form.rows {
		display: contents;
	}
	textarea {
		width: 20rem;
		max-width: 100%;
	}
	.row-actions {
		margin-top: var(--space-2);
	}
	.small {
		font-size: var(--text-base);
	}
</style>
