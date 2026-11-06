<script lang="ts">
	// Notfallzugang (PLAN 54-S.4, mockup 08). Owners and administrators see
	// the requests (Ablehnen / Jetzt freigeben), the grants and the
	// registered persons; a registered person asks with a reason, sees the
	// waiting time and, once granted, the handbook.
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDate, formatDateTime, formatTime } from '#lib/format.ts';
	import { session } from '#lib/session.svelte.ts';
	import {
		askEmergency,
		emergencyAction,
		emergencyCode,
		loadEmergency,
		loadProtectMe,
		waited,
		type EmergencyRequest,
		type EmergencyView,
		type ProtectMe,
	} from '#lib/features/protect.ts';
	import ProtectHandbook from './ProtectHandbook.svelte';

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	let view = $state<EmergencyView | null>(null);
	let me = $state<ProtectMe | null>(null);
	let error = $state('');
	let busy = $state(false);
	let reason = $state('');
	let code = $state('');
	let confirmGrant = $state<EmergencyRequest | null>(null);
	let printed = $state<{ account: string; code: string } | null>(null);
	let showBook = $state(false);
	let now = $state(new Date());

	async function load() {
		error = '';
		try {
			me = await loadProtectMe();
			if (admin) view = await loadEmergency();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		void admin;
		void load();
	});
	$effect(() => {
		const timer = setInterval(() => (now = new Date()), 30000);
		return () => clearInterval(timer);
	});

	async function act(req: EmergencyRequest, action: 'decline' | 'grant' | 'end' | 'cancel') {
		busy = true;
		error = '';
		confirmGrant = null;
		try {
			await emergencyAction(req.id, action);
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function ask() {
		busy = true;
		error = '';
		try {
			await askEmergency(reason.trim(), code.trim());
			reason = code = '';
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function makeCode(account: string) {
		error = '';
		try {
			printed = { account, code: (await emergencyCode(account)).code };
			await load();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	function day(at: string) {
		const d = new Date(at);
		return d.toDateString() === now.toDateString()
			? `${t('agent.f.protect.today')} ${formatTime(d)}`
			: formatDateTime(d);
	}
	function dueText(at: string) {
		const d = new Date(at);
		return (
			new Intl.DateTimeFormat(i18n.locale, {
				weekday: 'short',
				day: 'numeric',
				month: 'numeric',
			}).format(d) +
			' ' +
			t('agent.f.protect.e.at', { time: formatTime(d) })
		);
	}
	const waiting = $derived(view?.requests.filter((r) => r.state === 'waiting') ?? []);
	const granted = $derived(view?.requests.filter((r) => r.state === 'granted') ?? []);
	const history = $derived(
		view?.requests.filter((r) => r.state !== 'waiting' && r.state !== 'granted').slice(0, 10) ?? [],
	);
	const mine = $derived(me?.request);
</script>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#snippet progress(r: EmergencyRequest)}
	{@const part = waited(r, now)}
	<div
		class="bar"
		role="progressbar"
		aria-valuemin="0"
		aria-valuemax="100"
		aria-valuenow={Math.round(part * 100)}
		aria-label={t('agent.f.protect.e.waited', { percent: Math.round(part * 100) })}
	>
		<span style:width="{part * 100}%"></span>
	</div>
{/snippet}

{#if admin && view}
	{#if !view.enabled}
		<p class="muted">{t('agent.f.notThere')}</p>
	{/if}
	{#each waiting as r (r.id)}
		<div class="pcard" style:--c="var(--warning, #f59e0b)">
			<h3>🔑 {t('agent.f.protect.e.asks', { name: r.account })}</h3>
			<p>{day(r.created)} · {t('agent.f.protect.e.reason', { reason: r.reason || '–' })}</p>
			<p>
				{t('agent.f.protect.e.without', { when: dueText(r.due) })}
				<strong>{r.shares.join(', ')}</strong>
				{t('agent.f.protect.e.andBook')}
			</p>
			{@render progress(r)}
			<div class="acts">
				{#if r.account !== session.data?.user?.name}
					<button
						type="button"
						class="btn btn-warn"
						disabled={busy}
						onclick={() => act(r, 'decline')}>{t('agent.f.protect.e.decline')}</button
					>
					<button type="button" class="btn" disabled={busy} onclick={() => (confirmGrant = r)}
						>{t('agent.f.protect.e.grantNow')}</button
					>
				{/if}
			</div>
		</div>
	{/each}
	{#each granted as r (r.id)}
		<div class="pcard" style:--c="var(--accent)">
			<h3>🔓 {t('agent.f.protect.e.active', { name: r.account })}</h3>
			<p>
				{t('agent.f.protect.e.activeText', {
					shares: (r.added?.length ? r.added : r.shares).join(', '),
					until: r.ends ? formatDate(new Date(r.ends)) : '–',
				})}
			</p>
			<div class="acts">
				<button type="button" class="btn" disabled={busy} onclick={() => act(r, 'end')}
					>{t('agent.f.protect.e.end')}</button
				>
			</div>
		</div>
	{/each}

	<p class="pcap">{t('agent.f.protect.e.persons')}</p>
	<div class="pgrp">
		{#each view.persons as p (p.account)}
			<div class="prow">
				<span class="pic" style:--c="#3b82f6" aria-hidden="true">👤</span>
				<span class="pt"
					><strong>{p.account}</strong><small
						>{t('agent.f.protect.e.personSub', { account: p.account, days: p.waitDays })} · {p.shares.join(
							', ',
						)}</small
					></span
				>
				{#if view.code}
					<button type="button" class="btn" onclick={() => makeCode(p.account)}
						>{p.codeSet ? t('agent.f.protect.e.newCode') : t('agent.f.protect.e.printCode')}</button
					>
				{/if}
			</div>
		{:else}
			<div class="prow">
				<span class="pt"><small>{t('agent.f.protect.e.noPersons')}</small></span>
			</div>
		{/each}
	</div>
	<p class="muted small">{t('agent.f.protect.e.footer')}</p>

	{#if history.length}
		<p class="pcap">{t('agent.f.protect.e.history')}</p>
		<div class="pgrp">
			{#each history as r (r.id)}
				<div class="prow">
					<span class="pt"
						><strong>{r.account} · {day(r.created)}</strong><small
							>{t(`agent.f.protect.e.state.${r.state}` as MessageKey)}{r.by
								? ` · ${r.by}`
								: ''}</small
						></span
					>
				</div>
			{/each}
		</div>
	{/if}
{:else if me}
	{#if !me.emergency}
		<p class="muted">{t('agent.f.protect.e.notRegistered')}</p>
	{:else if mine?.state === 'waiting'}
		<div class="pcard" style:--c="var(--warning, #f59e0b)">
			<h3>🔑 {t('agent.f.protect.e.mineWaiting')}</h3>
			<p>{t('agent.f.protect.e.mineWaitingText', { when: dueText(mine.due) })}</p>
			{@render progress(mine)}
			<div class="acts">
				<button
					type="button"
					class="btn"
					disabled={busy}
					onclick={() => mine && act(mine, 'cancel')}>{t('agent.f.protect.e.withdraw')}</button
				>
			</div>
		</div>
	{:else if mine?.state === 'granted'}
		<div class="pcard" style:--c="var(--accent)">
			<h3>🔓 {t('agent.f.protect.e.mineGranted')}</h3>
			<p>
				{t('agent.f.protect.e.mineGrantedText', {
					shares: mine.shares.join(', '),
					until: mine.ends ? formatDate(new Date(mine.ends)) : '–',
				})}
			</p>
			{#if me.handbook}
				<div class="acts">
					<button type="button" class="btn" onclick={() => (showBook = !showBook)}
						>{t('agent.f.protect.e.openBook')}</button
					>
				</div>
			{/if}
		</div>
		{#if showBook}<ProtectHandbook />{/if}
	{:else}
		{#if mine?.state === 'declined'}
			<p class="notice-error">{t('agent.f.protect.e.mineDeclined')}</p>
		{/if}
		<p>
			{t('agent.f.protect.e.explain', {
				days: me.person?.waitDays ?? 3,
				shares: me.person?.shares.join(', ') ?? '',
			})}
		</p>
		<label class="lbl" for="em-reason">{t('agent.f.protect.e.reasonLabel')}</label>
		<textarea id="em-reason" class="field" rows="3" maxlength="500" bind:value={reason}></textarea>
		{#if me.codes}
			<label class="lbl" for="em-code">{t('agent.f.protect.e.codeLabel')}</label>
			<input id="em-code" class="field" autocomplete="off" spellcheck="false" bind:value={code} />
		{/if}
		<div class="acts">
			<button type="button" class="btn btn-primary" disabled={busy || !reason.trim()} onclick={ask}
				>{t('agent.f.protect.e.ask')}</button
			>
		</div>
	{/if}
	<p class="muted small">{t('agent.f.protect.e.footer')}</p>
{/if}

{#if confirmGrant}
	<Dialog title={t('agent.f.protect.e.grantNow')} onclose={() => (confirmGrant = null)}>
		<p>
			{t('agent.f.protect.e.grantConfirm', {
				name: confirmGrant.account,
				shares: confirmGrant.shares.join(', '),
			})}
		</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirmGrant = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				onclick={() => confirmGrant && act(confirmGrant, 'grant')}
				>{t('agent.f.protect.e.grantNow')}</button
			>
		</div>
	</Dialog>
{/if}
{#if printed}
	<Dialog
		title={t('agent.f.protect.e.codeTitle', { name: printed.account })}
		onclose={() => (printed = null)}
	>
		<p>{t('agent.f.protect.e.codeText')}</p>
		<p class="code">{printed.code}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (printed = null)}>{t('common.close')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => window.print()}
				>{t('agent.f.protect.e.print')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-top: 4px;
	}
	.bar {
		height: 6px;
		border-radius: 3px;
		background: color-mix(in srgb, var(--text) 14%, transparent);
		overflow: hidden;
	}
	.bar span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	.small {
		font-size: var(--text-sm, 0.875rem);
		margin: 4px 2px;
	}
	.lbl {
		font-weight: 600;
		margin: 4px 2px -4px;
	}
	textarea.field,
	input.field {
		width: 100%;
	}
	.code {
		font-family: var(--font-mono, monospace);
		font-size: 1.6rem;
		letter-spacing: 0.12em;
		text-align: center;
		margin: 12px 0;
		overflow-wrap: anywhere;
	}
</style>
