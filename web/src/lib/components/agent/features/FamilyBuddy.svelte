<script lang="ts">
	// Backup-Buddy (PLAN 54-G, mockup 11): the partners, what this NAS keeps
	// at each and each keeps here, the probe restore, and pairing with a
	// one-time code. No token or key ever reaches this page.
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import {
		GiB,
		buddyCode,
		buddyCodeCancel,
		buddyPair,
		buddyPartnerDelete,
		buddyPartnerSet,
		formatCode,
		loadBuddy,
		quotaPercent,
		type BuddyPartner,
		type BuddyView,
	} from '#lib/features/family.ts';

	const tk = (key: string, p?: Record<string, string | number>) => t(key as MessageKey, p);

	let data = $state<BuddyView | null>(null);
	let error = $state('');
	let busy = $state(false);
	// Pairing: what this NAS offers the new partner.
	let quotaGB = $state(500);
	let shares = $state<string[]>([]);
	let address = $state('');
	let code = $state('');
	let editing = $state<BuddyPartner | null>(null);
	let editQuota = $state(0);
	let editShares = $state<string[]>([]);
	let ending = $state<BuddyPartner | null>(null);
	let endData = $state(false);

	async function load() {
		try {
			data = await loadBuddy();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	void load();

	async function run(work: () => Promise<unknown>) {
		error = '';
		busy = true;
		try {
			await work();
			await load();
			return true;
		} catch (e) {
			error = apiErrorText(e);
			return false;
		} finally {
			busy = false;
		}
	}

	const when = (iso: string | null | undefined) => (iso ? formatDateTime(new Date(iso)) : '');
	function toggleIn(list: string[], name: string, on: boolean) {
		return on ? [...new Set([...list, name])] : list.filter((x) => x !== name);
	}
	function lastText(p: BuddyPartner) {
		if (!p.job) return t('agent.f.family.buddy.noJob');
		if (p.job.running) return t('agent.f.family.buddy.running');
		if (p.job.lastOK) return tk('agent.f.family.buddy.last', { when: when(p.job.lastOK) });
		return t('agent.f.family.buddy.never');
	}
	const failing = (p: BuddyPartner) =>
		!!p.job?.lastError && (!p.job.lastOK || (p.job.lastRun ?? '') > p.job.lastOK);
</script>

<div class="buddy">
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if !data}
		<p class="muted">{t('agent.f.family.loading')}</p>
	{:else}
		{#if data.problem}
			<p class="note" role="status">{tk(`agent.f.family.buddy.problem.${data.problem}`)}</p>
		{/if}
		{#each data.partners as p (p.id)}
			<div class="grp">
				<div class="row">
					<span class="ic" aria-hidden="true">🤝</span>
					<span class="t"
						><strong>{tk('agent.f.family.buddy.paired', { name: p.name })}</strong><small
							>{p.address} · {lastText(p)}</small
						></span
					>
					{#if failing(p)}<span class="tag bad">{t('agent.f.family.buddy.failed')}</span
						>{:else if p.job?.lastOK}<span class="tag ok">{t('agent.f.family.ok')}</span>{/if}
				</div>
			</div>
			<h4 class="h">{tk('agent.f.family.buddy.iAtThem', { name: p.name })}</h4>
			<div class="grp">
				{#if p.jobError}
					<div class="row">
						<p class="notice-error" role="alert">
							{tk('agent.f.family.buddy.jobError', { error: p.jobError })}
						</p>
					</div>
				{/if}
				<div class="row">
					<span class="t"
						><strong
							>{p.shares.length
								? p.shares.join(', ')
								: t('agent.f.family.buddy.nothingChosen')}</strong
						><small
							>{tk('agent.f.family.buddy.ofQuota', {
								used: formatBytes(p.job?.bytes ?? 0),
								quota: formatBytes(p.theirQuota),
								name: p.name,
							})}</small
						></span
					>
				</div>
				<div class="row">
					<span class="t"
						><small>{tk('agent.f.family.buddy.space', { name: p.name })}</small>
						<span
							class="bar"
							role="meter"
							aria-valuemin="0"
							aria-valuemax="100"
							aria-valuenow={quotaPercent(p.job?.bytes ?? 0, p.theirQuota)}
							aria-label={tk('agent.f.family.buddy.space', { name: p.name })}
							><i style:width="{quotaPercent(p.job?.bytes ?? 0, p.theirQuota)}%"></i></span
						></span
					>
				</div>
				<div class="row">
					<span class="t"
						><strong>{t('agent.f.family.buddy.probeTitle')}</strong><small
							>{#if p.job?.probe}{tk('agent.f.family.buddy.probeLast', {
									when: when(p.job.probe.at),
									n: p.job.probe.files,
								})}{:else}{t('agent.f.family.buddy.probeNone')}{/if}</small
						></span
					>
					{#if p.job?.probe}{#if p.job.probe.error}<span class="tag bad"
								>{t('agent.f.family.buddy.failed')}</span
							>{:else}<span class="tag ok">{t('agent.f.family.ok')}</span>{/if}{/if}
				</div>
				<div class="row btns">
					<button
						type="button"
						class="btn"
						onclick={() => {
							editing = p;
							editQuota = Math.round(p.quota / GiB);
							editShares = [...p.shares];
						}}>{t('agent.f.family.buddy.change')}</button
					>
					<button
						type="button"
						class="btn btn-danger-outline"
						onclick={() => {
							ending = p;
							endData = false;
						}}>{t('agent.f.family.buddy.end')}</button
					>
				</div>
			</div>
			<h4 class="h">{tk('agent.f.family.buddy.themAtMe', { name: p.name })}</h4>
			<div class="grp">
				<div class="row">
					<span class="t"
						><strong
							>{p.usedKnown
								? tk('agent.f.family.buddy.hereOf', {
										used: formatBytes(p.used),
										quota: formatBytes(p.quota),
										volume: data.settings.volume ?? '',
									})
								: tk('agent.f.family.buddy.hereQuota', {
										quota: formatBytes(p.quota),
										volume: data.settings.volume ?? '',
									})}</strong
						><small>{tk('agent.f.family.buddy.theirKey', { name: p.name })}</small></span
					>
				</div>
			</div>
		{/each}

		<h4 class="h">{t('agent.f.family.buddy.new')}</h4>
		<div class="grp">
			{#if data.code}
				<div class="row">
					<span class="t"
						><small>{t('agent.f.family.buddy.codeHint')}</small>
						<span class="code" aria-live="polite">{data.code.code}</span>
						<small
							>{tk('agent.f.family.buddy.codeUntil', { when: when(data.code.expires) })} · {tk(
								'agent.f.family.buddy.myAddress',
								{ address: data.settings.address || '–' },
							)}</small
						></span
					>
				</div>
				<div class="row btns">
					<button type="button" class="btn" disabled={busy} onclick={() => run(buddyCodeCancel)}
						>{t('agent.f.family.buddy.codeCancel')}</button
					>
				</div>
			{:else}
				<div class="row col">
					<label class="field-row"
						><span>{t('agent.f.family.buddy.offer')}</span>
						<span class="unit"
							><input class="field" type="number" min="1" max="1000000" bind:value={quotaGB} /><span
								>GB</span
							></span
						></label
					>
					<fieldset>
						<legend>{t('agent.f.family.buddy.mine')}</legend>
						{#each data.shares as s (s)}
							<label class="check"
								><input
									type="checkbox"
									checked={shares.includes(s)}
									onchange={(e) => (shares = toggleIn(shares, s, e.currentTarget.checked))}
								/><span>{s}</span></label
							>
						{:else}<small class="muted">{t('agent.f.family.buddy.noShares')}</small>{/each}
					</fieldset>
					<button
						type="button"
						class="btn btn-primary"
						disabled={busy || data.problem === 'off'}
						onclick={() => run(() => buddyCode(quotaGB * GiB, shares))}
						>{t('agent.f.family.buddy.showCode')}</button
					>
				</div>
				<div class="row col">
					<p class="muted">{t('agent.f.family.buddy.enterHint')}</p>
					<label class="field-row"
						><span>{t('agent.f.family.buddy.theirAddress')}</span>
						<input
							class="field"
							bind:value={address}
							placeholder="nas-tom.tail1234.ts.net"
						/></label
					>
					<label class="field-row"
						><span>{t('agent.f.family.buddy.theirCode')}</span>
						<input
							class="field code-in"
							value={code}
							autocomplete="off"
							spellcheck="false"
							oninput={(e) => (code = formatCode(e.currentTarget.value))}
							placeholder="K7Q-4MZ-92X"
						/></label
					>
					<button
						type="button"
						class="btn btn-primary"
						disabled={busy || !address || code.length !== 11 || data.problem === 'off'}
						onclick={async () => {
							let jobError = '';
							const ok = await run(async () => {
								jobError = (await buddyPair(address, code, quotaGB * GiB, shares)).jobError ?? '';
							});
							if (jobError) error = tk('agent.f.family.buddy.jobError', { error: jobError });
							if (ok) {
								address = '';
								code = '';
							}
						}}>{t('agent.f.family.buddy.pair')}</button
					>
				</div>
			{/if}
		</div>
		<p class="muted small">
			{tk('agent.f.family.buddy.footer', { days: data.settings.protectDays ?? 30 })}
		</p>
	{/if}
</div>

{#if editing}
	<Dialog
		title={tk('agent.f.family.buddy.editTitle', { name: editing.name })}
		onclose={() => (editing = null)}
	>
		<div class="buddy">
			<label class="field-row"
				><span>{tk('agent.f.family.buddy.offerTo', { name: editing.name })}</span>
				<span class="unit"
					><input class="field" type="number" min="1" bind:value={editQuota} /><span>GB</span></span
				></label
			>
			<fieldset>
				<legend>{tk('agent.f.family.buddy.iAtThem', { name: editing.name })}</legend>
				{#each data?.shares ?? [] as s (s)}
					<label class="check"
						><input
							type="checkbox"
							checked={editShares.includes(s)}
							onchange={(e) => (editShares = toggleIn(editShares, s, e.currentTarget.checked))}
						/><span>{s}</span></label
					>
				{/each}
			</fieldset>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (editing = null)}
					>{t('agent.f.family.cancel')}</button
				>
				<button
					type="button"
					class="btn btn-primary"
					disabled={busy}
					onclick={async () => {
						const p = editing!;
						if (await run(() => buddyPartnerSet(p.id, editQuota * GiB, editShares))) editing = null;
					}}>{t('agent.f.family.save')}</button
				>
			</div>
		</div>
	</Dialog>
{/if}

{#if ending}
	<Dialog
		title={tk('agent.f.family.buddy.endTitle', { name: ending.name })}
		onclose={() => (ending = null)}
	>
		<p>{tk('agent.f.family.buddy.endText', { name: ending.name })}</p>
		<label class="confirm"
			><input type="checkbox" bind:checked={endData} /><span
				>{tk('agent.f.family.buddy.endData', { name: ending.name })}</span
			></label
		>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (ending = null)}
				>{t('agent.f.family.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				disabled={busy}
				onclick={async () => {
					const p = ending!;
					if (await run(() => buddyPartnerDelete(p.id, endData))) ending = null;
				}}>{t('agent.f.family.buddy.end')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.buddy {
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		min-width: 0;
	}
	.buddy p {
		margin: 0;
	}
	.h {
		margin: var(--space-2) 2px -2px;
		font-size: var(--text-xs, 0.75rem);
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	/* PLAN 78-5: the rows of a group stand together, no box around them. */
	.grp {
		display: flex;
		flex-direction: column;
		gap: 3px;
		flex-shrink: 0;
		min-width: 0;
	}
	.row {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		padding: var(--space-2-5) var(--space-3);
		min-width: 0;
		border: 1px solid var(--srow-line);
		border-radius: var(--srow-radius);
		background: var(--srow-bg);
	}
	.row.col {
		flex-direction: column;
		align-items: stretch;
	}
	.row.btns {
		justify-content: flex-end;
		flex-wrap: wrap;
	}
	.ic {
		width: 30px;
		height: 30px;
		flex-shrink: 0;
		display: grid;
		place-items: center;
		border-radius: 9px;
		background: color-mix(in srgb, #14b8a6 18%, transparent);
	}
	.t {
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.t small {
		color: var(--muted);
		line-height: 1.35;
	}
	.tag {
		flex-shrink: 0;
		font-size: var(--text-xs, 0.75rem);
		padding: 2px 9px;
		border-radius: 999px;
		border: 1px solid currentColor;
	}
	.tag.ok {
		color: var(--success, #22c55e);
	}
	.tag.bad {
		color: var(--danger, #ef4444);
	}
	.bar {
		display: block;
		height: 8px;
		border-radius: 4px;
		background: color-mix(in srgb, var(--text) 14%, transparent);
		overflow: hidden;
	}
	.bar i {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	.code {
		display: block;
		text-align: center;
		font-family: var(--font-mono, monospace);
		font-size: 1.6rem;
		font-weight: 700;
		letter-spacing: 0.12em;
		padding: var(--space-2-5) var(--space-1);
		border: 1px dashed var(--border);
		border-radius: 10px;
		overflow-wrap: anywhere;
	}
	.code-in {
		font-family: var(--font-mono, monospace);
		letter-spacing: 0.1em;
	}
	.note {
		padding: var(--space-2-5) var(--space-3);
		border-radius: 12px;
		border: 1px solid var(--warning, #eab308);
	}
	.field-row {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
	}
	.field-row > span:first-child {
		font-weight: 600;
	}
	.field-row .field {
		width: 100%;
		min-width: 0;
	}
	.unit {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}
	.unit .field {
		max-width: 10rem;
	}
	/* A group of check boxes under its name, no box around it (PLAN 78-5). */
	fieldset {
		border: 0;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		min-width: 0;
	}
	legend {
		font-weight: 600;
		padding: 0 0 var(--space-1);
	}
	.check {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}
	.small {
		font-size: var(--text-sm, 0.85rem);
	}
</style>
