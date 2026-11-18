<script lang="ts">
	// 1.12 (owner 18 Nov 2026, "Leerer Lesebereich als Übersicht"): with no
	// message open the reading pane shows the unread mail of the mailbox by
	// folder, the flagged mails of this folder, when the list was last read
	// (and whether new mail comes by itself) and a few links.
	import Icon from '../components/Icon.svelte';
	import { apiErrorText, i18n, t } from '../i18n/index.svelte.ts';
	import { mailApi } from './api.ts';
	import { findFolder, folderName, mail, sortFolders } from './store.svelte.ts';
	import { senderName } from './text.ts';
	import type { MessageRow } from './types.ts';

	let {
		accountId,
		folder,
		refreshed,
		live,
		onopen,
		onpick,
		onshow,
		oncompose,
	}: {
		accountId: string;
		folder: string;
		refreshed: Date | null;
		/** New mail of the inbox comes over the live stream (IDLE). */
		live: boolean;
		onopen: (uid: number, folder: string) => void;
		onpick: (folder: string) => void;
		onshow: (filter: 'unread' | 'flagged') => void;
		oncompose: () => void;
	} = $props();

	const account = $derived(mail.accounts?.find((a) => a.id === accountId));
	const accountName = $derived(account ? account.name || account.email : '');
	const unreadFolders = $derived(
		sortFolders(mail.folders[accountId] ?? []).filter(
			(f) => f.unread > 0 && f.role !== 'junk' && f.role !== 'trash',
		),
	);
	const unread = $derived(unreadFolders.reduce((n, f) => n + f.unread, 0));
	const here = $derived(findFolder(accountId, folder));

	let flagged = $state<MessageRow[]>([]);
	let flaggedTotal = $state(0);
	let flaggedError = $state('');
	let asked = 0;
	$effect(() => {
		const id = accountId;
		const f = folder;
		void refreshed;
		const n = ++asked;
		const controller = new AbortController();
		mailApi
			.messages(id, { folder: f, flagged: true, limit: 5 }, controller.signal)
			.then((page) => {
				if (n !== asked) return;
				flagged = page.messages;
				flaggedTotal = page.total;
				flaggedError = '';
			})
			.catch((err) => {
				if (n === asked && !controller.signal.aborted) flaggedError = apiErrorText(err);
			});
		return () => controller.abort();
	});

	const time = $derived(
		refreshed
			? new Intl.DateTimeFormat(i18n.locale, {
					hour: '2-digit',
					minute: '2-digit',
					second: '2-digit',
				}).format(refreshed)
			: '',
	);
</script>

<section class="overview" aria-labelledby="mail-overview-head">
	<h2 id="mail-overview-head">{t('mail.overview.title')}</h2>
	<p class="muted">{t('mail.overview.hint')}</p>

	<div class="card">
		<h3>
			<Icon name="mail" size={16} />{unread
				? t('mail.overview.unreadSome', { count: unread, account: accountName })
				: t('mail.overview.unreadNone', { account: accountName })}
		</h3>
		{#if unreadFolders.length}
			<ul class="links">
				{#each unreadFolders as f (f.path)}
					<li>
						<button type="button" class="btn link" onclick={() => onpick(f.path)}
							><span class="name">{folderName(f)}</span><span class="num"
								>{t('mail.unreadCount', { count: f.unread })}</span
							></button
						>
					</li>
				{/each}
			</ul>
		{/if}
		{#if (here?.unread ?? 0) > 0}
			<button type="button" class="btn" onclick={() => onshow('unread')}
				>{t('mail.overview.showUnread')}</button
			>
		{/if}
	</div>

	<div class="card">
		<h3>
			<Icon name="flag" size={16} />{t('mail.overview.flagged', {
				folder: here ? folderName(here) : folder,
			})}
		</h3>
		{#if flaggedError}
			<p class="notice-error" role="alert">{flaggedError}</p>
		{:else if !flagged.length}
			<p class="muted">{t('mail.overview.flaggedNone')}</p>
		{:else}
			<ul class="links">
				{#each flagged as m (m.uid)}
					<li>
						<button type="button" class="btn link" onclick={() => onopen(m.uid, folder)}
							><span class="name">{senderName(m.from) || t('mail.noSender')}</span><span
								class="subj">{m.subject?.trim() || t('mail.noSubject')}</span
							></button
						>
					</li>
				{/each}
			</ul>
			{#if flaggedTotal > flagged.length}
				<button type="button" class="btn" onclick={() => onshow('flagged')}
					>{t('mail.overview.flaggedMore')}</button
				>
			{/if}
		{/if}
	</div>

	<div class="card">
		<h3>
			<Icon name="refresh" size={16} />{refreshed
				? t('mail.overview.refreshed', { time })
				: t('mail.overview.notYet')}
		</h3>
		<p class="muted">{live ? t('mail.overview.live') : t('mail.overview.poll')}</p>
	</div>

	<div class="card">
		<h3>{t('mail.overview.links')}</h3>
		<div class="quick">
			<button type="button" class="btn btn-primary" onclick={oncompose}
				><Icon name="edit" size={16} />{t('mail.action.compose')}</button
			>
			<a class="btn" href="/mail/settings"
				><Icon name="gear" size={16} />{t('mail.overview.settings')}</a
			>
			<a class="btn" href="/mail/setup"><Icon name="plus" size={16} />{t('mail.side.add')}</a>
		</div>
	</div>
</section>

<style>
	.overview {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-4);
		min-width: 0;
	}
	h2 {
		margin: 0;
		font-size: var(--text-lg);
	}
	h3 {
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		margin: 0 0 var(--space-2);
		font-size: var(--text-md);
		overflow-wrap: break-word;
	}
	h3 :global(svg) {
		flex: none;
		color: var(--muted);
	}
	p {
		margin: 0;
		overflow-wrap: break-word;
	}
	.card {
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--srow-bg, var(--surface));
		min-width: 0;
	}
	.links {
		list-style: none;
		margin: 0 0 var(--space-2);
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.link {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: 0 var(--space-2);
		width: 100%;
		text-align: left;
	}
	.name {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.subj,
	.num {
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.subj {
		flex: 1 1 100%;
	}
	.quick {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.quick .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
</style>
