<script lang="ts">
	// Ransomware-Wächter (PLAN 54-S.1, mockup 06): the latest alarm with
	// what, when, device, account and the affected files, and the three
	// ways out: restore from the snapshot (or the backup without Btrfs),
	// open the share again, false alarm. Restoring asks once (Dialog).
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDate, formatNumber, formatTime } from '#lib/format.ts';
	import {
		guardAction,
		loadGuard,
		splitPath,
		type GuardAlert,
		type GuardStatus,
	} from '#lib/features/protect.ts';

	let status = $state<GuardStatus | null>(null);
	let error = $state('');
	let busy = $state('');
	let confirm = $state<'restore' | 'reopen' | 'dismiss' | null>(null);
	let all = $state(false);

	async function load() {
		try {
			status = await loadGuard();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	void load();
	$effect(() => {
		const timer = setInterval(() => {
			if (status?.alerts.some((a) => a.state === 'active')) void load();
		}, 5000);
		return () => clearInterval(timer);
	});

	const alert = $derived(
		status?.alerts.find(
			(a) => a.state === 'active' || a.state === 'stopped' || a.state === 'restoring',
		) ?? status?.alerts[0],
	);
	const earlier = $derived(status?.alerts.filter((a) => a !== alert) ?? []);
	const open = (a: GuardAlert) =>
		a.state === 'active' || a.state === 'stopped' || a.state === 'restoring';

	function sameDay(a: Date, b: Date) {
		return a.toDateString() === b.toDateString();
	}
	function when(a: GuardAlert) {
		const first = new Date(a.first);
		const last = new Date(a.last);
		const day = sameDay(first, new Date()) ? t('agent.f.protect.today') : formatDate(first);
		const from = formatTime(first);
		const to = formatTime(last);
		return from === to ? `${day} ${from}` : `${day} ${from}–${to}`;
	}
	function device(a: GuardAlert) {
		const c = a.client;
		if (c.machine && c.ip && c.machine !== c.ip) return `${c.machine} (${c.ip})`;
		return c.ip || c.machine || c.process || '?';
	}
	function ext(a: GuardAlert) {
		const f = a.files.find((x) => x.now !== x.was);
		if (!f) return '';
		const i = f.now.lastIndexOf('.');
		return i > 0 ? f.now.slice(i) : '';
	}
	const total = (a: GuardAlert) => a.count;
	function whatText(a: GuardAlert) {
		let text = t(`agent.f.protect.g.what.${a.reason}` as MessageKey, {
			when: when(a),
			count: formatNumber(total(a)),
			ext: ext(a),
		});
		text += t('agent.f.protect.g.from', { device: device(a) });
		if (a.client.user) text += t('agent.f.protect.g.account', { user: a.client.user });
		return text + '.';
	}

	async function act(a: GuardAlert, action: 'restore' | 'reopen' | 'dismiss') {
		confirm = null;
		busy = action;
		error = '';
		try {
			await guardAction(a.id, action);
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = '';
		}
	}
	const shown = $derived(alert ? (all ? alert.files : alert.files.slice(0, 2)) : []);
	const rest = $derived(alert ? total(alert) - shown.length : 0);
</script>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if status && !status.enabled}
	<p class="muted">{t('agent.f.notThere')}</p>
{:else if status}
	{#if status.error}
		<p class="notice-error" role="alert">
			{t('agent.f.protect.g.notWatching', { error: status.error })}
		</p>
	{/if}
	{#if !alert}
		<div class="pcard" style:--c="var(--success, #22c55e)">
			<h3>🛡️ {t('agent.f.protect.g.calm')}</h3>
			<p>
				{status.watching ? t('agent.f.protect.g.watching') : t('agent.f.protect.g.starting')}
				{t(`agent.f.protect.g.mode.${status.mode}` as MessageKey)}
			</p>
		</div>
	{:else}
		<div class="pcard" style:--c={open(alert) ? 'var(--danger, #fb7185)' : 'var(--muted, #94a3b8)'}>
			<h3>
				🛡️ {alert.locked
					? t('agent.f.protect.g.locked', { share: alert.share })
					: t('agent.f.protect.g.suspect', { share: alert.share })}
			</h3>
			<p>{whatText(alert)}</p>
			{#if alert.state === 'active'}
				<p><strong>{t('agent.f.protect.g.ongoing')}</strong></p>
			{/if}
			<p>
				{#if alert.locked}
					{alert.snapshot
						? t('agent.f.protect.g.didSnap', {
								time: formatTime(new Date(alert.snapshotAt ?? alert.first)),
							})
						: t('agent.f.protect.g.didLock')}
				{:else if alert.state === 'restored'}
					{t('agent.f.protect.g.didRestore', {
						count: formatNumber(alert.restored?.restored ?? 0),
						bin: formatNumber(alert.restored?.recycled ?? 0),
					})}
				{:else if alert.mode === 'report' && open(alert)}
					{t('agent.f.protect.g.onlyTold')}
				{:else}
					{t(`agent.f.protect.g.state.${alert.state}` as MessageKey)}
				{/if}
			</p>
			{#if alert.error}<p class="muted">{alert.error}</p>{/if}
		</div>

		<p class="pcap">{t('agent.f.protect.g.affected')}</p>
		<div class="pgrp">
			{#each shown as f, i (i)}
				<div class="prow">
					<span class="pt"
						><strong>{f.now}</strong>{#if f.now !== f.was}<small
								>{t('agent.f.protect.g.before', { name: splitPath(f.was).base })}</small
							>{:else}<small>{t('agent.f.protect.g.rewritten')}</small>{/if}</span
					>
				</div>
			{/each}
			{#if rest > 0 && !all}
				<button type="button" class="prow more" onclick={() => (all = true)}
					><span class="pt"
						><strong>{t('agent.f.protect.g.more', { count: formatNumber(rest) })}</strong></span
					><span aria-hidden="true">›</span></button
				>
			{:else if rest > 0}
				<div class="prow">
					<span class="pt"
						><small>{t('agent.f.protect.g.notListed', { count: formatNumber(rest) })}</small></span
					>
				</div>
			{/if}
		</div>

		{#if open(alert) || alert.locked}
			<div class="pgrp">
				{#if alert.btrfs && (alert.snapshot || alert.held)}
					<div class="prow">
						<span class="pic" style:--c="var(--success, #22c55e)" aria-hidden="true">⏪</span>
						<span class="pt"
							><strong
								>{t('agent.f.protect.g.restore', {
									time: formatTime(new Date(alert.snapshotAt ?? alert.first)),
								})}</strong
							><small
								>{alert.state === 'active'
									? t('agent.f.protect.g.restoreWait')
									: t('agent.f.protect.g.restoreSub', {
											count: formatNumber(alert.restorable ?? total(alert)),
										})}</small
							></span
						>
						<button
							type="button"
							class="btn btn-primary"
							disabled={alert.state === 'active' || !!busy || alert.state === 'restored'}
							onclick={() => (confirm = 'restore')}>{t('agent.f.protect.g.restoreBtn')}</button
						>
					</div>
				{:else}
					<div class="prow">
						<span class="pic" style:--c="var(--warning, #f59e0b)" aria-hidden="true">💾</span>
						<span class="pt"
							><strong>{t('agent.f.protect.g.fromBackup')}</strong><small
								>{alert.btrfs
									? t('agent.f.protect.g.noSnapshot')
									: t('agent.f.protect.g.noBtrfs')}</small
							></span
						>
						<a class="btn" href="/settings/backups/restore">{t('agent.f.protect.g.backupBtn')}</a>
					</div>
				{/if}
				{#if alert.locked}
					<div class="prow">
						<span class="pic" style:--c="var(--warning, #f59e0b)" aria-hidden="true">🔓</span>
						<span class="pt"
							><strong>{t('agent.f.protect.g.reopen')}</strong><small
								>{t('agent.f.protect.g.reopenSub')}</small
							></span
						>
						<button
							type="button"
							class="btn"
							disabled={alert.state === 'active' || !!busy}
							onclick={() => (confirm = 'reopen')}>{t('agent.f.protect.g.reopenBtn')}</button
						>
					</div>
				{/if}
				{#if open(alert)}
					<div class="prow">
						<span class="pic" style:--c="var(--muted, #94a3b8)" aria-hidden="true">✔️</span>
						<span class="pt"
							><strong>{t('agent.f.protect.g.wanted')}</strong><small
								>{t('agent.f.protect.g.wantedSub')}</small
							></span
						>
						<button
							type="button"
							class="btn"
							disabled={!!busy}
							onclick={() => (confirm = 'dismiss')}>{t('agent.f.protect.g.falseAlarm')}</button
						>
					</div>
				{/if}
			</div>
		{/if}
	{/if}

	{#if earlier.length}
		<p class="pcap">{t('agent.f.protect.g.earlier')}</p>
		<div class="pgrp">
			{#each earlier as a (a.id)}
				<div class="prow">
					<span class="pt"
						><strong>{a.share} · {when(a)}</strong><small
							>{formatNumber(a.count)} · {device(a)} · {t(
								`agent.f.protect.g.state.${a.state}` as MessageKey,
							)}</small
						></span
					>
				</div>
			{/each}
		</div>
	{/if}
{/if}

{#if confirm && alert}
	<Dialog
		title={t(`agent.f.protect.g.confirm.${confirm}` as MessageKey)}
		onclose={() => (confirm = null)}
	>
		<p>
			{t(`agent.f.protect.g.confirmText.${confirm}` as MessageKey, {
				share: alert.share,
				count: formatNumber(alert.restorable ?? total(alert)),
			})}
		</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirm = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class={confirm === 'restore' ? 'btn btn-danger' : 'btn btn-primary'}
				onclick={() => confirm && act(alert, confirm)}
				>{t(`agent.f.protect.g.confirmBtn.${confirm}` as MessageKey)}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.more {
		cursor: pointer;
	}
	.more:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.prow :global(.btn) {
		flex-shrink: 0;
	}
	@media (max-width: 420px) {
		.prow :global(.btn) {
			padding-inline: var(--space-3);
		}
	}
</style>
