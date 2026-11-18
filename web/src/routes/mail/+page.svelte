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
	// 1.13 (owner 18 Nov 2026): the columns are the shared ColumnFrame –
	// edge to edge, the window's height, the side bar in the window side
	// bar's look, folding to a rail; folders sorted per account. The
	// folders' counts change at once and the NAS is asked for them once the
	// changes rest (store.svelte.ts refreshFolders), not on every event.
	import { onMount, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import BottomBar from '#lib/components/BottomBar.svelte';
	import ColumnFrame, { type FrameLevel } from '#lib/components/ColumnFrame.svelte';
	import { compactView } from '#lib/components/compact.svelte.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import FolderColumn from '#lib/tmail/FolderColumn.svelte';
	import FolderRail from '#lib/tmail/FolderRail.svelte';
	import KeysHelp from '#lib/tmail/KeysHelp.svelte';
	import { levelOf, mailHref, mailUp, placeOf, type MailPlace } from '#lib/tmail/levels.ts';
	import { live, watchMail, type MailEvent } from '#lib/tmail/live.svelte.ts';
	import MailOverview from '#lib/tmail/MailOverview.svelte';
	import MessageList, { type MailItem } from '#lib/tmail/MessageList.svelte';
	import MessageView from '#lib/tmail/MessageView.svelte';
	import { openCompose } from '#lib/tmail/open.ts';
	import { mailKey } from '#lib/tmail/shortcuts.ts';
	import {
		adjustFolders,
		applyStatus,
		ensureFolders,
		findFolder,
		folderName,
		inboxOf,
		loadAccounts,
		loadAvatars,
		loadFolders,
		mail,
		mailDrag,
		refreshFolders,
		type FolderDelta,
	} from '#lib/tmail/store.svelte.ts';
	import type { ComposeMode, Flags, Message } from '#lib/tmail/types.ts';
	import AgentPanel from '#lib/components/agent/AgentPanel.svelte';
	import AgentSheet from '#lib/components/agent/BottomSheet.svelte';
	import type { AgentContext } from '#lib/agent.ts';
	import { mailPanelActions } from '#lib/agent/panel-actions.ts';
	import { session } from '#lib/session.svelte.ts';

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
	// 1.13: the phone and windows under 620 px – levels edge to edge, the
	// actions in a bar at the bottom, "Neue E-Mail" at its right.
	const compact = $derived(!wide && compactView());

	const place = $derived(placeOf(page.url));
	const level = $derived(levelOf(place));
	const account = $derived(mail.accounts?.find((a) => a.id === place.a));
	const folder = $derived(place.a && place.f ? findFolder(place.a, place.f) : undefined);
	const listHeading = $derived(
		folder ? folderName(folder) : place.f ? place.f.split(/[/.]/).at(-1) || place.f : '',
	);
	/** The folder of the open message: a hit of the search may lie elsewhere. */
	const messageFolder = $derived(place.mf || place.f);

	// 1.13 (owner 18 Nov 2026): the agent's panel – right of the message
	// (a column of the frame), compact a sheet from below; open or not is
	// kept per device. The open mail goes with each message as a reference.
	const AGENT_KEY = 'titanos.mail.agentPanel';
	const withAgent = $derived(!!session.services.agent);
	let agentOpen = $state(readAgentOpen());
	/** The panel stands as a column (enough room next to it); else a sheet. */
	let agentColumn = $state(false);
	function readAgentOpen(): boolean {
		try {
			return localStorage.getItem(AGENT_KEY) === '1';
		} catch {
			return false;
		}
	}
	function setAgent(on: boolean) {
		agentOpen = on;
		try {
			if (on) localStorage.setItem(AGENT_KEY, '1');
			else localStorage.removeItem(AGENT_KEY);
		} catch {
			// Kept for this visit only.
		}
	}
	const agentContext = $derived<AgentContext | null>(
		place.a && place.m
			? { app: 'mail', mail: { a: place.a, f: place.mf || place.f, uid: place.m } }
			: null,
	);
	const agentActions = $derived(session.services.mail ? mailPanelActions((k) => t(k)) : []);

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
			if (!mail.folders[a.id]) untrack(() => ensureFolders(a.id));
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

	// 1.13: a burst of events (ten new mails, marks set on another device)
	// reads the list once, a moment after the last of them.
	let listTimer: ReturnType<typeof setTimeout> | undefined;
	function listSoon() {
		clearTimeout(listTimer);
		listTimer = setTimeout(() => list?.refresh(true), 800);
	}
	onMount(() => () => clearTimeout(listTimer));

	function onEvent(e: MailEvent) {
		if (e.type === 'resync') {
			void loadAccounts();
			for (const a of mail.accounts ?? []) refreshFolders(a.id, true);
			list?.refresh(true);
			return;
		}
		if (e.type === 'status') {
			applyStatus(e.account, e.status);
			return;
		}
		// New mail and changed marks: the counts once things rest; the
		// mailboxes' list is not read again (their state comes as "status").
		refreshFolders(e.account);
		if (e.account === place.a && e.folder === place.f) listSoon();
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
			refreshFolders(place.a, true);
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

	/** Counts changed by this page: shown at once, read from the NAS once things rest. */
	function counted(deltas: FolderDelta[]) {
		if (!place.a) return;
		adjustFolders(place.a, deltas);
		refreshFolders(place.a);
	}
	function flagsChanged(uids: number[], change: Partial<Flags>) {
		list?.setFlags(uids, change, messageFolder);
		if ('seen' in change)
			counted([{ folder: messageFolder, unread: change.seen ? -uids.length : uids.length }]);
	}
	/** The open message was moved or deleted in the reading pane. */
	function removed(uid: number) {
		const next = wide ? (list?.neighbour(uid, messageFolder) ?? null) : null;
		list?.drop([uid], messageFolder);
		counted([{ folder: messageFolder, total: -1 }]);
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
		if (!action || action === 'help' || !mail.accounts?.length) return;
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

	/**
	 * "?" shows T-Mail's keys here instead of the shortcuts of the whole UI
	 * (Shell): taken before the Shell's own listener on the window sees it.
	 */
	function helpKey(e: KeyboardEvent) {
		if (helpOpen || document.querySelector('dialog[open]') || !mail.accounts?.length) return;
		if (mailKey(e) !== 'help') return;
		e.preventDefault();
		e.stopPropagation();
		helpOpen = true;
	}

	// On the phone the bar above names the program and each level its place
	// itself (the list's heading, the subject): no second strip (1.13).
	const title = $derived(
		level === 'folders' || !listHeading || phone ? t('desktop.app.mail') : listHeading,
	);

	// Opening a message on a level of its own starts at its top (the subject).
	$effect(() => {
		if (wide || !place.m) return;
		void place.m;
		untrack(() => window.scrollTo(0, 0));
	});

	// The parts of the frame by the level of the narrow view.
	const frameLevel = $derived<FrameLevel>(
		level === 'folders' ? 'side' : level === 'list' ? 'list' : 'content',
	);
	// In columns the list's column stands from the start (also while the
	// mailboxes load), so that nothing moves when the first folder opens.
	const hasList = $derived(wide || (!!place.a && !!place.f));
</script>

<svelte:window onkeydown={keydown} onkeydowncapture={helpKey} />

<PageHead {title} />

{#snippet notices()}
	{#if mail.error}<p class="notice-error notice" role="alert">{mail.error}</p>{/if}
	{#if live.down}
		<p class="notice-warn notice" role="status">{t('mail.live.down')}</p>
	{/if}
{/snippet}

{#snippet compose_(folded: boolean)}
	<!-- Already there while the mailboxes load (disabled): nothing moves when they arrive. -->
	{#if mail.accounts === null || mail.accounts.length}
		<button
			type="button"
			class="btn btn-primary compose"
			disabled={mail.accounts === null}
			class:icon={folded}
			title={folded ? t('mail.action.compose') : undefined}
			aria-label={folded ? t('mail.action.compose') : undefined}
			onclick={() => compose()}
			><Icon name="edit" size={16} />{#if !folded}{t('mail.action.compose')}{/if}</button
		>
	{/if}
{/snippet}

{#snippet folders()}
	{#if mail.accounts === null}
		<p class="muted side-loading" role="status">{t('common.loading')}</p>
	{:else}
		<FolderColumn
			account={place.a}
			folder={place.f}
			sortable={wide}
			onpick={(a, f) => go({ a, f }, !wide)}
			ondrop={wide ? dropped : undefined}
		/>
	{/if}
{/snippet}

{#snippet rail()}
	<FolderRail account={place.a} folder={place.f} onpick={(a, f) => go({ a, f }, false)} />
{/snippet}

{#snippet foot(folded: boolean)}
	{#if withAgent}
		<button
			type="button"
			class="side-link agent-toggle"
			class:icon={folded}
			aria-pressed={agentOpen}
			title={folded ? t('agentpim.panel.toggle') : undefined}
			aria-label={folded ? t('agentpim.panel.toggle') : undefined}
			onclick={() => setAgent(!agentOpen)}
			><Icon name="sparkle" size={17} />{#if !folded}<span>{t('agentpim.panel.toggle')}</span
				>{/if}</button
		>
	{/if}
	{#each [{ href: '/mail/setup', icon: 'plus' as const, label: t('mail.side.add') }, { href: '/mail/settings', icon: 'gear' as const, label: t('mail.side.settings') }] as link (link.href)}
		<a
			class="side-link"
			class:icon={folded}
			href={link.href}
			title={folded ? link.label : undefined}
			aria-label={folded ? link.label : undefined}
			><Icon name={link.icon} size={17} />{#if !folded}<span>{link.label}</span>{/if}</a
		>
	{/each}
{/snippet}

{#snippet messages()}
	{#if place.a && place.f}
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
			heading={account && (mail.accounts?.length ?? 0) > 1
				? t('mail.list.heading', {
						folder: listHeading,
						account: account.name || account.email,
					})
				: listHeading}
			oncompose={wide || compact ? undefined : () => compose()}
			onopen={openMessage}
			onchanged={counted}
			onremoved={listRemoved}
			onloaded={(at) => (refreshed = at)}
		/>
		{#if compact}
			<BottomBar label={t('mail.listActions')}>
				<button
					type="button"
					class="bar-btn bar-new"
					aria-label={t('mail.action.compose')}
					title={t('mail.action.compose')}
					onclick={() => compose()}><Icon name="edit" size={20} /></button
				>
				{#if withAgent}
					<button
						type="button"
						class="bar-btn"
						aria-label={t('agentpim.panel.toggle')}
						title={t('agentpim.panel.toggle')}
						onclick={() => setAgent(true)}><Icon name="sparkle" size={20} /></button
					>
				{/if}
			</BottomBar>
		{/if}
	{/if}
{/snippet}

{#if mail.accounts !== null && !mail.accounts.length}
	{@render notices()}
	<EmptyState title={t('mail.empty.title')} text={t('mail.empty.text')}>
		<a class="btn btn-primary" href="/mail/setup">{t('mail.empty.add')}</a>
	</EmptyState>
{:else if mail.accounts === null && !wide}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else}
	<ColumnFrame
		storage="titanos.mail"
		sideKey="titanos.mail.foldersWidth"
		listKey="titanos.mail.listWidth"
		columns={wide}
		level={frameLevel}
		class={wide ? 'mail wide' : 'mail levels'}
		sideClass="col-folders"
		listClass="col-list"
		contentClass="col-message"
		sideWidth={{ min: 170, max: 420, fallback: 240 }}
		listWidth={{ min: 280, max: 720, fallback: 380 }}
		contentMin={320}
		sideResizeLabel={t('mail.resize.folders')}
		listResizeLabel={t('mail.resize.list')}
		resizeHint={t('mail.resize.hint')}
		action={compose_}
		side={folders}
		{rail}
		sideFoot={foot}
		notices={mail.error || live.down ? notices : undefined}
		list={hasList ? messages : undefined}
		aside={withAgent && agentOpen && wide ? agentAside : undefined}
		asideKey="titanos.agentPanel.width"
		asideResizeLabel={t('agentpim.panel.resize')}
		bind:asideShown={agentColumn}
	>
		{#if place.a && place.f && place.m}
			{#if !wide && !phone}
				<button type="button" class="btn back" onclick={() => void goto(mailUp(page.url))}
					><Icon name="chevronLeft" size={16} />{t('mail.back.list', {
						folder: listHeading,
					})}</button
				>
			{/if}
			<MessageView
				{compact}
				accountId={place.a}
				folder={messageFolder}
				uid={place.m}
				oncompose={compose}
				onflags={flagsChanged}
				onremoved={removed}
				onagent={withAgent ? () => setAgent(true) : undefined}
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
			<p class="muted none">{mail.accounts === null ? t('common.loading') : t('mail.noneOpen')}</p>
		{/if}
	</ColumnFrame>
{/if}

{#snippet agentAside()}
	<AgentPanel
		app="mail"
		context={agentContext}
		actions={agentActions}
		onclose={() => setAgent(false)}
	/>
{/snippet}

{#if withAgent && agentOpen && !(wide && agentColumn)}
	<AgentSheet label={t('agentpim.panel.title')} onclose={() => setAgent(false)}>
		<AgentPanel
			app="mail"
			phone
			context={agentContext}
			actions={agentActions}
			onclose={() => setAgent(false)}
		/>
	</AgentSheet>
{/if}

{#if helpOpen}<KeysHelp onclose={() => (helpOpen = false)} />{/if}

<style>
	.notice {
		margin: var(--space-2) var(--space-3);
	}
	:global(.mail.levels) .notice {
		margin: 0 0 var(--space-2);
	}
	.compose {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-1-5);
		min-height: 40px;
	}
	.compose.icon {
		width: 44px;
		min-height: 44px;
		padding: 0;
	}
	.side-loading {
		margin: var(--space-2) var(--space-2-5);
	}
	.side-link {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border-radius: calc(9px * var(--radius-scale, 1));
		color: var(--text);
		text-decoration: none;
	}
	.side-link.icon {
		justify-content: center;
		width: 44px;
		padding: 0;
	}
	button.side-link {
		width: 100%;
		border: 0;
		background: none;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	button.side-link.icon {
		width: 44px;
	}
	.agent-toggle[aria-pressed='true'] {
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.side-link :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.side-link span {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.side-link:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.side-link:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	/* The columns: the reading pane is a column of its own. */
	:global(.mail.wide .col-message) {
		display: flex;
		flex-direction: column;
	}
	/* The narrow view and the phone: one level at a time, as before 1.13. */
	:global(.mail.levels) {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	:global(.mail.levels .col) {
		min-width: 0;
	}
	:global(.mail.levels:not(.compact) .col-list),
	:global(.mail.levels:not(.compact) .col-message) {
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
		.compose,
		.side-link {
			min-height: 44px;
		}
	}
</style>
