<script lang="ts">
	// 1.05, T-Mail (docs/TMAIL.md): on a wide window three columns – mailboxes
	// with their folders | the list | the message. On the phone and in a
	// narrow window the same as levels, one at a time, kept in the address:
	// the phone's arrow and a swipe from the edge go one level up, a narrow
	// window has its own back button. New mail and the mailboxes' state come
	// live (lib/tmail/live.svelte.ts).
	// 1.12 (owner 18 Nov 2026): the columns' widths are dragged (per device);
	// no bar above them – "Neue E-Mail" heads the folders, "Aktualisieren" is
	// gone: the live stream brings new mail of the inbox, and without it (or
	// in other folders) the list reloads every 2 minutes while T-Mail is seen;
	// keys (j/k/e/r/Entf, /, ?); with no message open an overview.
	import { onMount, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import ColumnResize, { readWidth } from '#lib/components/ColumnResize.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import FolderColumn from '#lib/tmail/FolderColumn.svelte';
	import KeysHelp from '#lib/tmail/KeysHelp.svelte';
	import { levelOf, mailHref, mailUp, placeOf, type MailPlace } from '#lib/tmail/levels.ts';
	import { live, watchMail, type MailEvent } from '#lib/tmail/live.svelte.ts';
	import MailOverview from '#lib/tmail/MailOverview.svelte';
	import MessageList, { type MailItem } from '#lib/tmail/MessageList.svelte';
	import MessageView from '#lib/tmail/MessageView.svelte';
	import { openCompose } from '#lib/tmail/open.ts';
	import { mailKey } from '#lib/tmail/shortcuts.ts';
	import {
		applyStatus,
		findFolder,
		folderName,
		inboxOf,
		loadAccounts,
		loadAvatars,
		loadFolders,
		mail,
		mailDrag,
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
	/** The folder of the open message: a hit of the search may lie elsewhere. */
	const messageFolder = $derived(place.mf || place.f);

	let list = $state<MessageList>();
	let refreshed = $state<Date | null>(null);
	let helpOpen = $state(false);

	/** Goes to a place of T-Mail; levels are steps in the history, columns replace it. */
	function go(p: Partial<MailPlace>, push = !wide) {
		void goto(mailHref(p), { replace: !push, reset: false });
	}
	function openMessage(uid: number, inFolder: string) {
		go({ a: place.a, f: place.f, m: uid, mf: inFolder }, !wide);
	}

	// The folders of every mailbox, read once each; the contacts' pictures once.
	$effect(() => {
		for (const a of mail.accounts ?? [])
			if (!mail.folders[a.id]) untrack(() => void loadFolders(a.id));
	});
	onMount(() => void loadAvatars());

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

	// No "Aktualisieren" any more: the live stream covers the inbox of a
	// mailbox with IDLE (state ok). Other folders, and mailboxes without it,
	// are read again every 2 minutes while the page is seen; never one whose
	// login failed or whose server is not reached (they wait for the user).
	const POLL = 120_000;
	const liveHere = $derived(
		account?.status.state === 'ok' && folder?.role === 'inbox' && live.connected,
	);
	onMount(() => {
		const timer = setInterval(() => {
			if (document.visibilityState !== 'visible' || !place.a || !account) return;
			if (account.status.state === 'auth_failed' || account.status.state === 'unreachable') return;
			if (liveHere) return;
			void loadFolders(place.a);
			list?.refresh(true);
		}, POLL);
		return () => clearInterval(timer);
	});

	function refresh() {
		void loadAccounts();
		if (place.a) void loadFolders(place.a);
		list?.refresh(false);
	}

	function compose(mode?: ComposeMode, msg?: Message) {
		const a = place.a || mail.accounts?.[0]?.id;
		if (!a) return;
		const back = page.url.pathname + page.url.search;
		if (mode && msg) openCompose({ a, mode, f: msg.folder || messageFolder, uid: msg.uid, back });
		else openCompose({ a, back });
	}

	registerMenuCommands({
		'mail.compose': { run: () => compose(), enabled: () => !!mail.accounts?.length },
		// PLAN 87: pulling the list down on the phone (PhoneGestures).
		'mail.refresh': refresh,
	});

	function flagsChanged(uids: number[], change: Partial<Flags>) {
		list?.setFlags(uids, change, messageFolder);
		if ('seen' in change && place.a) void loadFolders(place.a);
	}
	/** The open message was moved or deleted in the reading pane. */
	function removed(uid: number) {
		const next = wide ? (list?.neighbour(uid, messageFolder) ?? null) : null;
		list?.drop([uid], messageFolder);
		if (place.a) void loadFolders(place.a);
		go({ a: place.a, f: place.f, m: next?.uid ?? null, mf: next?.folder }, false);
	}
	/** Mails left the list (quick actions, the bar, a drop): the open one goes on to the next. */
	function listRemoved(items: MailItem[], next: MailItem | null) {
		if (!place.m || !items.some((i) => i.uid === place.m && i.folder === messageFolder)) return;
		if (wide && next) go({ a: place.a, f: place.f, m: next.uid, mf: next.folder }, false);
		else go({ a: place.a, f: place.f }, false);
	}
	function dropped(a: string, to: string) {
		if (a !== place.a || mailDrag.account !== a) return;
		list?.moveDropped([...mailDrag.items], to);
	}

	// The keys of T-Mail (lib/tmail/shortcuts.ts): never while typing, never
	// with a dialog open; the window is a frame of its own.
	function keydown(e: KeyboardEvent) {
		if (helpOpen || document.querySelector('dialog[open]')) return;
		const action = mailKey(e);
		if (!action || !mail.accounts?.length) return;
		if (action === 'help') {
			e.preventDefault();
			helpOpen = true;
			return;
		}
		if (!list || !place.a || !place.f) return;
		// On the phone and in narrow windows only where the list is seen.
		if (
			!wide &&
			level === 'message' &&
			action !== 'reply' &&
			action !== 'delete' &&
			action !== 'archive'
		)
			return;
		e.preventDefault();
		switch (action) {
			case 'next':
			case 'prev': {
				const to = list.step(action === 'next' ? 1 : -1);
				if (to) go({ a: place.a, f: place.f, m: to.uid, mf: to.folder }, false);
				break;
			}
			case 'archive':
			case 'delete':
				list.actOnCurrent(action);
				break;
			case 'reply':
				if (place.m)
					openCompose({
						a: place.a,
						mode: 'reply',
						f: messageFolder,
						uid: place.m,
						back: page.url.pathname + page.url.search,
					});
				break;
			case 'search':
				list.focusSearch();
				break;
		}
	}

	const title = $derived(level === 'folders' || !listHeading ? t('desktop.app.mail') : listHeading);

	// The columns fill the window; each scrolls on its own.
	let grid = $state<HTMLElement>();
	let top = $state(0);
	let gridWidth = $state(1200);
	$effect(() => {
		const el = grid;
		if (!el || !wide) return;
		const measure = () => {
			top = Math.max(0, el.getBoundingClientRect().top + window.scrollY);
			gridWidth = el.clientWidth;
		};
		measure();
		const observer = new ResizeObserver(measure);
		observer.observe(document.body);
		observer.observe(el);
		return () => observer.disconnect();
	});

	// Widths per device (ColumnResize). The reading pane keeps at least
	// READ_MIN: when the window is narrower, the columns give way.
	const FOLDERS = { min: 170, max: 420, fallback: 240, key: 'titanos.mail.foldersWidth' };
	const LIST = { min: 280, max: 720, fallback: 380, key: 'titanos.mail.listWidth' };
	const READ_MIN = 320;
	let folderWidth = $state(readWidth(FOLDERS.key, FOLDERS.min, FOLDERS.max, FOLDERS.fallback));
	let listWidth = $state(readWidth(LIST.key, LIST.min, LIST.max, LIST.fallback));
	const foldersMax = $derived(
		Math.max(FOLDERS.min, Math.min(FOLDERS.max, gridWidth - READ_MIN - LIST.min)),
	);
	const fw = $derived(Math.min(folderWidth, foldersMax));
	const listMax = $derived(Math.max(LIST.min, Math.min(LIST.max, gridWidth - READ_MIN - fw)));
	const lw = $derived(Math.min(listWidth, listMax));
</script>

<svelte:window onkeydown={keydown} />

<PageHead {title} />

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
		style:--mail-fw="{fw}px"
		style:--mail-lw="{lw}px"
	>
		{#if wide || level === 'folders'}
			<div class="col col-folders">
				<FolderColumn
					account={place.a}
					folder={place.f}
					onpick={(a, f) => go({ a, f }, !wide)}
					oncompose={() => compose()}
					ondrop={wide ? dropped : undefined}
				/>
				<a class="btn add" href="/mail/setup"><Icon name="plus" size={16} />{t('mail.side.add')}</a>
			</div>
		{/if}
		{#if place.a && place.f}
			<!-- Kept while a message is open on the phone: going back finds the list (and a search) as it was. -->
			<div class="col col-list" hidden={!wide && level !== 'list'}>
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
					selectedFolder={messageFolder}
					{phone}
					drag={wide}
					heading={account && mail.accounts.length > 1
						? t('mail.list.heading', {
								folder: listHeading,
								account: account.name || account.email,
							})
						: listHeading}
					oncompose={wide ? undefined : () => compose()}
					onopen={openMessage}
					onchanged={() => place.a && void loadFolders(place.a)}
					onremoved={listRemoved}
					onloaded={(at) => (refreshed = at)}
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
						folder={messageFolder}
						uid={place.m}
						oncompose={compose}
						onflags={flagsChanged}
						onremoved={removed}
					/>
				{:else if place.a && place.f}
					<MailOverview
						accountId={place.a}
						folder={place.f}
						{refreshed}
						live={liveHere}
						onopen={openMessage}
						onpick={(f) => go({ a: place.a, f }, false)}
						onshow={(f) => list?.showFilter(f)}
						oncompose={() => compose()}
					/>
				{:else}
					<p class="muted none">{t('mail.noneOpen')}</p>
				{/if}
			</div>
		{/if}
		{#if wide}
			<ColumnResize
				bind:width={folderWidth}
				min={FOLDERS.min}
				max={foldersMax}
				fallback={FOLDERS.fallback}
				key={FOLDERS.key}
				label={t('mail.resize.folders')}
				hint={t('mail.resize.hint')}
				left="var(--mail-fw)"
			/>
			<ColumnResize
				bind:width={listWidth}
				min={LIST.min}
				max={listMax}
				fallback={LIST.fallback}
				key={LIST.key}
				label={t('mail.resize.list')}
				hint={t('mail.resize.hint')}
				left="calc(var(--mail-fw) + var(--mail-lw))"
			/>
		{/if}
	</div>
{/if}

{#if helpOpen}<KeysHelp onclose={() => (helpOpen = false)} />{/if}

<style>
	.live {
		margin: 0 0 var(--space-2);
	}
	.mail.wide {
		position: relative;
		display: grid;
		grid-template-columns: var(--mail-fw) var(--mail-lw) minmax(0, 1fr);
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
	.col[hidden] {
		display: none;
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
