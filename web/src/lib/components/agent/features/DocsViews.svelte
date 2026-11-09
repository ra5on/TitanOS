<script lang="ts">
	// Views of the group "Dokumente und Fristen" opened from the agent
	// (PLAN 54-D): 'paper' is the paperwork overview (drafts 04/05: months,
	// sums by type, open payments, recently filed with Rückgängig and
	// Zuordnen), 'deadlines' the deadlines found (draft 03).
	import type { FeatureViewId } from '#lib/agent-features.ts';
	import DocsSheet from './DocsSheet.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import type { InboxItem } from '#lib/agent.ts';
	import {
		PAPER_TYPES,
		euro,
		fileLink,
		paperAssign,
		paperPaid,
		paperSummary,
		paperUndo,
		type PaperEntry,
		type PaperSummary,
		type PaperType,
	} from '#lib/features/docs.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	type Props = { id: FeatureViewId; onclose: () => void; calm?: boolean };
	let { id, onclose, calm = false }: Props = $props();

	let error = $state('');

	// ---------- Papierkram ----------
	let sum = $state<PaperSummary | null>(null);
	let month = $state('');
	async function loadPaper() {
		error = '';
		try {
			sum = await paperSummary(month);
			month = sum.month;
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		if (id === 'paper') void loadPaper();
	});
	const loc = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const monthName = (m: string, year = false) =>
		new Date(m + '-15T12:00:00').toLocaleDateString(loc, {
			month: year ? 'long' : 'short',
			...(year ? { year: 'numeric' } : {}),
		});
	const shortDay = (d: string) =>
		new Date(d + 'T12:00:00').toLocaleDateString(loc, { day: '2-digit', month: '2-digit' });
	const typeLook: Record<PaperType, { icon: string; color: string }> = {
		energy: { icon: '⚡', color: '#eab308' },
		telecom: { icon: '📡', color: '#3b82f6' },
		insurance: { icon: '🛡️', color: '#8b5cf6' },
		shopping: { icon: '🛒', color: '#10b981' },
		health: { icon: '🩺', color: '#f43f5e' },
		housing: { icon: '🏠', color: '#f97316' },
		tax: { icon: '🏛️', color: '#64748b' },
		other: { icon: '📄', color: '#94a3b8' },
	};
	const typeName = (k: PaperType) => t(`agent.f.docs.type.${k}` as MessageKey);
	function ago(at: string): string {
		const min = Math.max(0, Math.round((Date.now() - new Date(at).getTime()) / 60000));
		if (min < 60) return t('agent.f.docs.agoMin', { n: Math.max(1, min) });
		if (min < 48 * 60) return t('agent.f.docs.agoH', { n: Math.round(min / 60) });
		return t('agent.f.docs.agoD', { n: Math.round(min / 1440) });
	}
	function entryIcon(e: PaperEntry): string {
		if (e.state === 'ask') return '❓';
		if (e.state === 'waiting') return '⏳';
		return e.origin === 'inbox' ? '🧾' : '✉️';
	}
	function entrySub(e: PaperEntry): string {
		if (e.state === 'waiting') return t('agent.f.docs.waitingModel');
		if (e.state === 'ask') {
			if (e.doubt?.length === 2)
				return t('agent.f.docs.unsureOf', { a: typeName(e.doubt[0]), b: typeName(e.doubt[1]) });
			return e.error ? t('agent.f.docs.unsureError') : t('agent.f.docs.unsure');
		}
		const from =
			e.origin === 'mailtext'
				? t('agent.f.docs.fromMailText')
				: e.origin === 'mail'
					? t('agent.f.docs.fromMail')
					: t('agent.f.docs.fromInbox');
		return `${from} → ${e.to ?? ''} · ${ago(e.at)}`;
	}
	async function undo(e: PaperEntry) {
		try {
			await paperUndo(e.id);
			await loadPaper();
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	async function paid(e: PaperEntry) {
		try {
			await paperPaid(e.id);
			await loadPaper();
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	let assigning = $state<{
		e: PaperEntry;
		sender: string;
		type: PaperType;
		kind: string;
		date: string;
	} | null>(null);
	function startAssign(e: PaperEntry) {
		assigning = {
			e,
			sender: e.sender ?? '',
			type: e.doubt?.[0] ?? e.type ?? 'other',
			kind: e.kind && e.kind !== 'Dokument' ? e.kind : '',
			date: '',
		};
	}
	async function assign() {
		if (!assigning) return;
		try {
			await paperAssign(assigning.e.id, {
				sender: assigning.sender,
				type: assigning.type,
				kind: assigning.kind,
				date: assigning.date,
			});
			assigning = null;
			await loadPaper();
		} catch (err) {
			error = apiErrorText(err);
		}
	}

	// ---------- Fristen ----------
	let items = $state<InboxItem[] | null>(null);
	async function loadDeadlines() {
		error = '';
		try {
			const r = await api.get<{ items: InboxItem[] }>('/agent/inbox');
			items = (r.items ?? []).filter((i) => (i.kind as string) === 'deadline').reverse();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		if (id === 'deadlines') void loadDeadlines();
	});
	const kindIcon: Record<string, string> = {
		cancel: '⏰',
		warranty: '🛠️',
		payment: '💶',
		due: '📅',
	};
	async function decide(item: InboxItem, accept: boolean) {
		error = '';
		try {
			await api.post(`/agent/inbox/${item.id}`, { accept });
			items = (items ?? []).filter((x) => x.id !== item.id);
		} catch (e) {
			error = apiErrorText(e);
			await loadDeadlines();
		}
	}
	async function markPaid(item: InboxItem) {
		try {
			if (item.data?.paper) await paperPaid(item.data.paper);
			await decide(item, false);
		} catch (e) {
			error = apiErrorText(e);
		}
	}
</script>

{#if id === 'paper'}
	<DocsSheet title={t('agent.f.menu.paper')} {calm} {onclose}>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if sum}
			<div class="seg" role="group" aria-label={t('agent.f.docs.month')}>
				{#each sum.months as m, i (m)}
					<button
						type="button"
						aria-pressed={m === sum.month}
						onclick={() => ((month = m), loadPaper())}
						>{monthName(m, i === sum.months.length - 1)}</button
					>
				{/each}
			</div>
			<section class="dgrp tile ui-card">
				<div class="drow col">
					<small class="muted"
						>{t('agent.f.docs.spentIn', { month: monthName(sum.month, true).split(' ')[0] })}</small
					>
					<span class="big">{euro(sum.total, i18n.locale)}</span>
					<small class="muted"
						>{t('agent.f.docs.bills', { count: sum.count })} · {monthName(
							sum.months[1] ?? sum.month,
							true,
						).split(' ')[0]}
						{euro(sum.prevTotal, i18n.locale)}</small
					>
				</div>
			</section>
			<section class="dgrp tile ui-card">
				<div class="card-head"><h3>{t('agent.f.docs.byType')}</h3></div>
				{#each sum.types as ty (ty.type)}
					<div class="drow">
						<span class="dic" style:--c={typeLook[ty.type].color} aria-hidden="true"
							>{typeLook[ty.type].icon}</span
						>
						<span class="dt"
							><strong>{typeName(ty.type)}</strong><small
								>{ty.count > 2 || !ty.senders.length
									? t('agent.f.docs.bills', { count: ty.count })
									: ty.senders.join(', ') +
										(ty.count > 1
											? ' · ' + t('agent.f.docs.bills', { count: ty.count })
											: '')}</small
							></span
						>
						<span class="amt">{euro(ty.cents, i18n.locale)}</span>
					</div>
				{:else}
					<div class="drow">
						<span class="dt"><small>{t('agent.f.docs.noneMonth')}</small></span>
					</div>
				{/each}
			</section>
			<section class="dgrp tile ui-card">
				<div class="card-head"><h3>{t('agent.f.docs.open')}</h3></div>
				{#each sum.open as e (e.id)}
					<div class="drow">
						<span class="dt"
							><strong>{[e.sender, e.kind].filter(Boolean).join(' ')}</strong><small
								>{t('agent.f.docs.dueOn', { date: shortDay(e.due ?? '') })} ·
								<button type="button" class="link" onclick={() => paid(e)}
									>{t('agent.f.docs.paid')}</button
								></small
							></span
						>
						<span class="amt warn">{euro(e.cents ?? 0, i18n.locale)}</span>
					</div>
				{:else}
					<div class="drow">
						<span class="dt"><small>{t('agent.f.docs.noneOpen')}</small></span>
					</div>
				{/each}
			</section>
			<section class="dgrp tile ui-card">
				<div class="card-head"><h3>{t('agent.f.docs.recent')}</h3></div>
				{#each sum.recent as e (e.id)}
					<div class="drow">
						<span class="dic" aria-hidden="true">{entryIcon(e)}</span>
						<span class="dt"><strong>{e.name}</strong><small>{entrySub(e)}</small></span>
						{#if e.state === 'filed'}
							<button type="button" class="btn" onclick={() => undo(e)}
								>{t('agent.f.docs.undo')}</button
							>
						{:else}
							<button type="button" class="btn" onclick={() => startAssign(e)}
								>{t('agent.f.docs.assign')}</button
							>
						{/if}
					</div>
				{:else}
					<div class="drow">
						<span class="dt"><small>{t('agent.f.docs.noneRecent')}</small></span>
					</div>
				{/each}
			</section>
		{:else if !error}
			<p class="muted">…</p>
		{/if}
	</DocsSheet>
{:else if id === 'deadlines'}
	<DocsSheet title={t('agent.f.docs.suggestions')} {calm} {onclose}>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#each items ?? [] as item (item.id)}
			<article class="card">
				<strong
					><span aria-hidden="true">{kindIcon[item.data?.deadline ?? ''] ?? '📅'}</span>
					{item.title}</strong
				>
				{#if item.body}<p class="muted">
						{item.body}{#if item.data?.deadline === 'payment'}
							· {item.data?.debit === '1'
								? t('agent.f.docs.debit')
								: t('agent.f.docs.noDebit')}{/if}
					</p>{/if}
				<div class="btns">
					<button type="button" class="btn btn-primary" onclick={() => decide(item, true)}
						>{t('agent.inbox.take')}</button
					>
					{#if item.data?.deadline === 'payment' && item.data?.paper}
						<button type="button" class="btn" onclick={() => markPaid(item)}
							>{t('agent.f.docs.paid')}</button
						>
					{:else if item.data?.root && item.data?.path}
						<a class="btn" href={fileLink(item.data.root, item.data.path)}
							>{t('agent.f.docs.openFile')}</a
						>
					{/if}
					<button type="button" class="btn" onclick={() => decide(item, false)}
						>{t('agent.f.docs.reject')}</button
					>
				</div>
			</article>
		{:else}
			<p class="muted">{items ? t('agent.f.docs.noneDeadlines') : '…'}</p>
		{/each}
	</DocsSheet>
{/if}

{#if assigning}
	<Dialog
		title={t('agent.f.docs.assignTitle', { name: assigning.e.name })}
		onclose={() => (assigning = null)}
	>
		<div class="form">
			<label for="as-sender">{t('agent.f.docs.sender')}</label>
			<input id="as-sender" class="field" bind:value={assigning.sender} />
			<label for="as-type">{t('agent.f.docs.typeLabel')}</label>
			<select id="as-type" class="field" bind:value={assigning.type}>
				{#each PAPER_TYPES as k (k)}<option value={k}>{typeName(k)}</option>{/each}
			</select>
			<label for="as-kind">{t('agent.f.docs.kind')}</label>
			<input
				id="as-kind"
				class="field"
				placeholder={t('agent.f.docs.kindHint')}
				bind:value={assigning.kind}
			/>
			<label for="as-date">{t('agent.f.docs.date')}</label>
			<input id="as-date" class="field" type="date" bind:value={assigning.date} />
		</div>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (assigning = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={!assigning.sender.trim()}
				onclick={assign}>{t('agent.f.docs.file')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.big {
		font-size: 26px;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: 14px;
		border-radius: 14px;
		border: 1px solid var(--warn-border);
		background: var(--warn-bg);
	}
	.card p {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.btns {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.form {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		margin-bottom: var(--space-3);
	}
</style>
