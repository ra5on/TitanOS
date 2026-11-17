<script lang="ts">
	// 1.05, T-Mail (docs/TMAIL.md): on a wide window three columns – mailboxes
	// with their folders | the list | the message. On the phone and in a
	// narrow window the same as levels, one at a time, kept in the address:
	// the phone's arrow and a swipe from the edge go one level up, a narrow
	// window has its own back button. New mail and the mailboxes' state come
	// live (lib/tmail/live.svelte.ts).
	import { onMount, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import FolderColumn from '#lib/tmail/FolderColumn.svelte';
	import { levelOf, mailHref, mailUp, placeOf } from '#lib/tmail/levels.ts';
	import { live, watchMail, type MailEvent } from '#lib/tmail/live.svelte.ts';
	import MessageList from '#lib/tmail/MessageList.svelte';
	import MessageView from '#lib/tmail/MessageView.svelte';
	import { openCompose } from '#lib/tmail/open.ts';
	import {
		applyStatus,
		findFolder,
		folderName,
		inboxOf,
		loadAccounts,
		loadFolders,
		mail,
	} from '#lib/tmail/store.svelte.ts';
	import type { ComposeMode, Flags, Message } from '#lib/tmail/types.ts';

	const WIDE = '(min-width: 900px)';
	let wideView = $state(typeof matchMedia === 'function' ? matchMedia(WIDE).matches : true);
	onMount(() => {
		const query = matchMedia(WIDE);
		const change = () => (wideView = query.matches);
		query.addEventListener('change', change);
		return () => query.removeEventListener('change', change);
	});
	const phone = $derived(phoneView());
	const wide = $derived(wideView && !phone);

	const place = $derived(placeOf(page.url));
	const level = $derived(levelOf(place));
	const account = $derived(mail.accounts?.find((a) => a.id === place.a));
	const folder = $derived(place.a && place.f ? findFolder(place.a, place.f) : undefined);
	const listHeading = $derived(
		folder ? folderName(folder) : place.f ? place.f.split(/[/.]/).at(-1) || place.f : '',
	);

	let list = $state<MessageList>();

	/** Goes to a place of T-Mail; levels are steps in the history, columns replace it. */
	function go(p: { a?: string; f?: string; m?: number | null }, push = !wide) {
		void goto(mailHref(p), { replace: !push, reset: false });
	}

	// The folders of every mailbox, read once each.
	$effect(() => {
		for (const a of mail.accounts ?? [])
			if (!mail.folders[a.id]) untrack(() => void loadFolders(a.id));
	});

	// Wide: with nothing chosen yet the first mailbox's inbox opens.
	$effect(() => {
		const first = mail.accounts?.[0];
		if (!wide || !first || place.a) return;
		untrack(() => go({ a: first.id, f: inboxOf(first.id) }, false));
	});

	// A mailbox that is gone (removed elsewhere) leaves the address.
	$effect(() => {
		if (place.a && mail.accounts && !account) untrack(() => go({}, false));
	});

	function onEvent(e: MailEvent) {
		if (e.type === 'resync') {
			void loadAccounts();
			for (const a of mail.accounts ?? []) void loadFolders(a.id);
			list?.refresh(true);
			return;
		}
		if (e.type === 'status') {
			applyStatus(e.account, e.status);
			return;
		}
		void loadFolders(e.account);
		if (e.type === 'mail') void loadAccounts();
		if (e.account === place.a && e.folder === place.f) list?.refresh(true);
	}
	onMount(() => watchMail(onEvent));

	function refresh() {
		void loadAccounts();
		if (place.a) void loadFolders(place.a);
		list?.refresh(false);
	}

	function compose(mode?: ComposeMode, msg?: Message) {
		const a = place.a || mail.accounts?.[0]?.id;
		if (!a) return;
		const back = page.url.pathname + page.url.search;
		if (mode && msg) openCompose({ a, mode, f: msg.folder || place.f, uid: msg.uid, back });
		else openCompose({ a, back });
	}

	registerMenuCommands({
		'mail.compose': { run: () => compose(), enabled: () => !!mail.accounts?.length },
		// PLAN 87: pulling the list down on the phone (PhoneGestures).
		'mail.refresh': refresh,
	});

	function flagsChanged(uids: number[], change: Partial<Flags>) {
		list?.setFlags(uids, change);
		if ('seen' in change && place.a) void loadFolders(place.a);
	}
	function removed(uid: number) {
		const next = wide ? (list?.neighbour(uid) ?? null) : null;
		list?.drop([uid]);
		if (place.a) void loadFolders(place.a);
		go({ a: place.a, f: place.f, m: next }, false);
	}

	const title = $derived(level === 'folders' || !listHeading ? t('desktop.app.mail') : listHeading);
	// The columns fill the window below the tool bar; each scrolls on its own.
	let grid = $state<HTMLElement>();
	let top = $state(0);
	$effect(() => {
		const el = grid;
		if (!el || !wide) return;
		const measure = () => (top = Math.max(0, el.getBoundingClientRect().top + window.scrollY));
		measure();
		const observer = new ResizeObserver(measure);
		observer.observe(document.body);
		return () => observer.disconnect();
	});
</script>

<PageHead {title}>
	{#snippet actions()}
		<button type="button" class="btn" onclick={refresh}
			><Icon name="refresh" size={16} />{t('mail.action.refresh')}</button
		>
		{#if mail.accounts?.length}
			<button type="button" class="btn btn-primary" onclick={() => compose()}
				><Icon name="edit" size={16} />{t('mail.action.compose')}</button
			>
		{/if}
	{/snippet}
</PageHead>

{#if mail.error}<p class="notice-error" role="alert">{mail.error}</p>{/if}
{#if live.down}
	<p class="notice-warn live" role="status">{t('mail.live.down')}</p>
{/if}

{#if !mail.accounts?.length}
	<EmptyState title={t('mail.empty.title')} text={t('mail.empty.text')}>
		<a class="btn btn-primary" href="/mail/setup">{t('mail.empty.add')}</a>
	</EmptyState>
{:else}
	<div
		class="mail"
		class:wide
		class:levels={!wide}
		data-level={level}
		bind:this={grid}
		style:--mail-top="{top}px"
	>
		{#if wide || level === 'folders'}
			<div class="col col-folders">
				<FolderColumn account={place.a} folder={place.f} onpick={(a, f) => go({ a, f }, !wide)} />
				<a class="btn add" href="/mail/setup"><Icon name="plus" size={16} />{t('mail.side.add')}</a>
			</div>
		{/if}
		{#if place.a && place.f && (wide || level === 'list')}
			<div class="col col-list">
				{#if !wide && !phone}
					<button type="button" class="btn back" onclick={() => void goto(mailUp(page.url))}
						><Icon name="chevronLeft" size={16} />{t('mail.back.folders')}</button
					>
				{/if}
				<MessageList
					bind:this={list}
					accountId={place.a}
					folder={place.f}
					selected={place.m}
					heading={account && mail.accounts.length > 1
						? t('mail.list.heading', {
								folder: listHeading,
								account: account.name || account.email,
							})
						: listHeading}
					onopen={(uid) => go({ a: place.a, f: place.f, m: uid }, !wide)}
				/>
			</div>
		{/if}
		{#if wide || level === 'message'}
			<div class="col col-message">
				{#if place.a && place.f && place.m}
					{#if !wide && !phone}
						<button type="button" class="btn back" onclick={() => void goto(mailUp(page.url))}
							><Icon name="chevronLeft" size={16} />{t('mail.back.list', {
								folder: listHeading,
							})}</button
						>
					{/if}
					<MessageView
						accountId={place.a}
						folder={place.f}
						uid={place.m}
						oncompose={compose}
						onflags={flagsChanged}
						onremoved={removed}
					/>
				{:else}
					<p class="muted none">{t('mail.noneOpen')}</p>
				{/if}
			</div>
		{/if}
	</div>
{/if}

<style>
	.live {
		margin: 0 0 var(--space-2);
	}
	.mail.wide {
		display: grid;
		grid-template-columns: minmax(200px, 250px) minmax(280px, 400px) minmax(0, 1fr);
		height: calc(100dvh - var(--mail-top) - var(--page-pad));
		min-height: 360px;
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
	}
	.wide .col {
		min-width: 0;
		min-height: 0;
		overflow: auto;
	}
	.wide .col + .col {
		border-left: 1px solid var(--border);
	}
	.col-folders {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-1-5);
	}
	.wide .col-message {
		display: flex;
		flex-direction: column;
	}
	.add {
		align-self: flex-start;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		margin: 0 var(--space-2);
	}
	.levels .col {
		min-width: 0;
	}
	.levels .col-list,
	.levels .col-message {
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
	}
	.back {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		margin: var(--space-2);
		min-height: 44px;
		overflow-wrap: anywhere;
		text-align: left;
	}
	.none {
		margin: var(--space-4);
	}
	@media (max-width: 899px) {
		.add {
			min-height: 44px;
		}
	}
</style>
