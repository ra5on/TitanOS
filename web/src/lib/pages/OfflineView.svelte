<script lang="ts">
	// 1.07: what the installed app shows when the NAS cannot be reached at
	// start – the state this browser saw last (offline.svelte.ts), clearly
	// marked as old, instead of an error page. Nothing can be changed here.
	// It looks again by itself every 15 seconds and with "Jetzt prüfen";
	// when the NAS answers, TitanOS starts as usual.
	import { page } from '$app/state';
	import { isAgentPath } from '#lib/agent-app.ts';
	import { formatBytesOf, formatDateTime, formatDuration, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { snapshotLocked, type OfflineSnapshot } from '#lib/offline.svelte.ts';
	import { refreshSession } from '#lib/session.svelte.ts';
	import AuthCard from './AuthCard.svelte';

	let { snap }: { snap: OfflineSnapshot } = $props();

	const agent = $derived(isAgentPath(page.url.pathname));
	const locked = $derived(snapshotLocked(snap));
	const when = (ms: number) => (ms ? formatDateTime(new Date(ms)) : '–');

	let busy = $state(false);
	async function check() {
		if (busy) return;
		busy = true;
		await refreshSession();
		busy = false;
	}
	$effect(() => {
		const timer = setInterval(() => void check(), 15_000);
		return () => clearInterval(timer);
	});
</script>

<AuthCard title={t('offline.title')} hostname={snap.hostname}>
	<div class="banner notice-warn" role="status">
		<p>
			<strong>{t('offline.lead')}</strong>
			{t('offline.seen', { time: when(snap.savedAt) })}
		</p>
		<button type="button" class="btn" onclick={check} disabled={busy}
			>{busy ? t('offline.checking') : t('offline.check')}</button
		>
	</div>
	{#if locked}
		<p class="locked">{t('offline.locked')}</p>
	{:else}
		{#snippet chats()}
			{#if snap.chats?.list.length}
				<section aria-labelledby="off-chats">
					<h2 id="off-chats">{t('offline.chats')}</h2>
					<ul class="list">
						{#each snap.chats.list as c (c.id)}
							<li>{c.title || t('offline.untitled')}</li>
						{/each}
					</ul>
				</section>
			{/if}
		{/snippet}
		{#if agent}{@render chats()}{/if}
		{#if snap.system}
			<section aria-labelledby="off-system">
				<h2 id="off-system">{t('offline.system')}</h2>
				<dl>
					<dt>{t('offline.cpu')}</dt>
					<dd>{formatPercent(snap.system.cpu)}</dd>
					<dt>{t('offline.memory')}</dt>
					<dd>{formatBytesOf(snap.system.memUsed, snap.system.memTotal)}</dd>
					<dt>{t('offline.uptime')}</dt>
					<dd>{formatDuration(snap.system.uptime)}</dd>
				</dl>
			</section>
		{/if}
		{#if snap.volumes?.list.length}
			<section aria-labelledby="off-volumes">
				<h2 id="off-volumes">{t('offline.volumes')}</h2>
				<ul class="list">
					{#each snap.volumes.list as v (v.name)}
						<li>
							<strong>{v.name}</strong>
							<span
								>{v.state === 'ok'
									? t('offline.volumeOk')
									: t('offline.volumeState', { state: v.state })}{v.size
									? ' · ' + formatBytesOf(v.used, v.size)
									: ''}</span
							>
						</li>
					{/each}
				</ul>
			</section>
		{/if}
		{#if snap.backups?.list.length}
			<section aria-labelledby="off-backups">
				<h2 id="off-backups">{t('offline.backups')}</h2>
				<ul class="list">
					{#each snap.backups.list as b, i (i)}
						<li>
							<strong>{b.name}</strong>
							<span
								>{b.lastRun
									? t(b.lastOk === false ? 'offline.backupFailed' : 'offline.backupOk', {
											time: formatDateTime(new Date(b.lastRun)),
										})
									: t('offline.backupNever')}</span
							>
						</li>
					{/each}
				</ul>
			</section>
		{/if}
		{#if !agent}{@render chats()}{/if}
		<p class="hint">{t('offline.hint')}</p>
	{/if}
</AuthCard>

<style>
	.banner {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2) var(--space-3);
	}
	.banner p {
		margin: 0;
		flex: 1 1 14em;
		overflow-wrap: break-word;
	}
	.banner .btn {
		min-height: 44px;
	}
	section {
		margin-top: var(--space-4);
	}
	h2 {
		margin: 0 0 var(--space-1-5);
		font-size: var(--text-md);
	}
	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-1) var(--space-3);
		margin: 0;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.list li {
		display: flex;
		flex-wrap: wrap;
		gap: 0 var(--space-2);
		overflow-wrap: anywhere;
	}
	.list li span {
		color: var(--muted);
	}
	.hint,
	.locked {
		margin: var(--space-4) 0 0;
		color: var(--muted);
		line-height: 1.45;
	}
</style>
