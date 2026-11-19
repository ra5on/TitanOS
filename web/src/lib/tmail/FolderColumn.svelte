<script lang="ts">
	// The first column (on the phone the first level): "Neue E-Mail" on top
	// (1.12: the bar above the columns is gone), the favourite folders of all
	// mailboxes, then every mailbox with its state and its folders. A mailbox
	// whose login failed offers "Passwort ändern" right there. 1.12: folders
	// are pinned as favourites and folders with subfolders fold (both per
	// device); spam and trash show no unread count; mails dragged from the
	// list drop onto a folder of the same mailbox.
	// 1.13: "Neue E-Mail" is the frame's main action (ColumnFrame); the
	// folders are sorted by dragging their grip or with the keys (Alt+↑/↓
	// on a folder), among their siblings, in the account's order on the NAS
	// (side-order.svelte.ts).
	import Icon, { type IconName } from '../components/Icon.svelte';
	import SortGrip, { sortKey } from '../components/SortGrip.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import {
		mergeGroup,
		orderOf,
		setMailAccounts,
		setMailHidden,
		setOrder,
	} from '../side-order.svelte.ts';
	import { folderRef, isStrings, readPref, writePref } from './prefs.ts';
	import StatusBadge from './StatusBadge.svelte';
	import {
		ALL,
		arrangeFolders,
		folderName,
		hiddenFolders,
		isBelow,
		mail,
		mailDrag,
		orderedAccounts,
	} from './store.svelte.ts';
	import type { FolderRole, MailAccount, MailFolder } from './types.ts';

	let {
		account = '',
		folder = '',
		sortable = false,
		onpick,
		oncompose,
		ondrop,
	}: {
		account?: string;
		folder?: string;
		/** Unused since 1.15: sorting is in "Bearbeiten" everywhere. */
		sortable?: boolean;
		onpick: (account: string, folder: string) => void;
		/** 1.15 (draft 20): compact views end with a bar – state and "Neue E-Mail". */
		oncompose?: () => void;
		/** Mails dragged from the list onto a folder of their mailbox. */
		ondrop?: (account: string, folder: string) => void;
	} = $props();

	const ICONS: Record<string, IconName> = {
		inbox: 'mail',
		drafts: 'edit',
		sent: 'send',
		archive: 'archive',
		junk: 'shield',
		trash: 'trash',
	};

	const roleIcon = (role: FolderRole): IconName => ICONS[role] ?? 'folder';
	/** Indent by the folder's depth (IMAP names it with its delimiter). */
	const levels = (f: MailFolder) => f.path.split(f.delim || /[/.]/).length - 1;
	/** Spam and trash count nothing unread (owner 18 Nov 2026). */
	const counts = (f: MailFolder) => f.unread > 0 && f.role !== 'junk' && f.role !== 'trash';

	let favourites = $state(readPref('favourites', [] as string[], isStrings));
	let folded = $state(readPref('folded', [] as string[], isStrings));
	let over = $state('');
	// 1.15 (115-4, drafts 20–24): mailboxes fold to their head line (per
	// device); "Bearbeiten" sorts mailboxes and folders and takes folders
	// out of the side bar (both kept on the NAS for the account).
	let shutBoxes = $state(readPref('foldedBoxes', [] as string[], isStrings));
	let editing = $state(false);
	const accounts = $derived(orderedAccounts());
	const boxIds = $derived(accounts.map((a) => a.id));
	const many = $derived(accounts.length > 1);

	function toggleBox(id: string) {
		shutBoxes = shutBoxes.includes(id) ? shutBoxes.filter((x) => x !== id) : [...shutBoxes, id];
		writePref('foldedBoxes', shutBoxes, []);
	}
	function toggleHidden(id: string, path: string) {
		const now = hiddenFolders(id);
		setMailHidden(id, now.includes(path) ? now.filter((p) => p !== path) : [...now, path], boxIds);
	}
	const unreadOf = (id: string) =>
		(mail.folders[id] ?? []).reduce((n, f) => n + (counts(f) ? f.unread : 0), 0);
	const allUnread = $derived(
		accounts.reduce(
			(n, a) => n + (mail.folders[a.id]?.find((f) => f.role === 'inbox')?.unread ?? 0),
			0,
		),
	);

	function toggleFavourite(a: string, path: string) {
		const ref = folderRef(a, path);
		favourites = favourites.includes(ref)
			? favourites.filter((x) => x !== ref)
			: [...favourites, ref];
		writePref('favourites', favourites, []);
	}
	function toggleFold(a: string, path: string) {
		const ref = folderRef(a, path);
		folded = folded.includes(ref) ? folded.filter((x) => x !== ref) : [...folded, ref];
		writePref('folded', folded, []);
	}

	const favList = $derived.by(() => {
		const out: { a: MailAccount; f: MailFolder }[] = [];
		for (const ref of favourites) {
			const [id, path] = ref.split('\u0000');
			const a = mail.accounts?.find((x) => x.id === id);
			const f = mail.folders[id]?.find((x) => x.path === path);
			if (a && f && !hiddenFolders(id).includes(path)) out.push({ a, f });
		}
		return out;
	});

	/**
	 * The folders shown: those below a folded one are left out. Folders
	 * with a role (Posteingang, Gesendet …) stand on top and never fold
	 * away, and the inbox folds nothing: servers with the namespace
	 * "INBOX." (Courier, cPanel) name every folder below it, which would
	 * otherwise hide them all with the inbox. There the levels count from
	 * below the inbox.
	 */
	function visible(
		id: string,
	): { f: MailFolder; parent: boolean; depth: number; group: string; keys: string[] }[] {
		const arranged = arrangeFolders(mail.folders[id] ?? [], orderOf('mail', id));
		const list = arranged.map((x) => x.f);
		const groups = new Map<string, string[]>();
		for (const x of arranged) groups.set(x.group, [...(groups.get(x.group) ?? []), x.f.path]);
		const inbox = list.find((p) => p.role === 'inbox');
		const shut = list.filter((p) => p.role !== 'inbox' && folded.includes(folderRef(id, p.path)));
		const hidden = editing ? [] : hiddenFolders(id);
		return arranged
			.filter(({ f }) => !hidden.includes(f.path))
			.filter(({ f }) => f.role || !shut.some((p) => isBelow(f, p)))
			.map(({ f, group }) => ({
				f,
				group,
				keys: groups.get(group) ?? [],
				parent: f.role !== 'inbox' && list.some((c) => !c.role && isBelow(c, f)),
				depth: f.role
					? 0
					: Math.max(0, Math.min(4, levels(f) - (inbox && isBelow(f, inbox) ? 1 : 0))),
			}));
	}

	/** A group of a mailbox's folders was sorted: saved for the account, other groups keep theirs. */
	function sorted(id: string, keys: string[]) {
		const known = new Set((mail.folders[id] ?? []).map((f) => f.path));
		const saved = orderOf('mail', id).filter((p) => known.has(p));
		setOrder('mail', mergeGroup(saved, keys), {
			mailbox: id,
			mailboxes: (mail.accounts ?? []).map((a) => a.id),
		});
	}

	// Dragging: only folders of the dragged mails' mailbox, not their own folder.
	const takes = (a: string, f: MailFolder) =>
		!!ondrop &&
		mailDrag.items.length > 0 &&
		mailDrag.account === a &&
		!mailDrag.items.every((i) => i.folder === f.path);
	function dragOver(e: DragEvent, a: string, f: MailFolder) {
		if (!takes(a, f)) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
		over = folderRef(a, f.path);
	}
	function dropped(e: DragEvent, a: string, f: MailFolder) {
		over = '';
		if (!takes(a, f)) return;
		e.preventDefault();
		ondrop?.(a, f.path);
	}
</script>

{#snippet folderButton(
	a: MailAccount,
	f: MailFolder,
	indent: number,
	label: string,
	keys: string[] | null = null,
	star = false,
)}
	{@const current = a.id === account && f.path === folder}
	{@const ref = folderRef(a.id, f.path)}
	<button
		type="button"
		class="folder"
		class:current
		class:over={over === ref}
		class:refuse={mailDrag.items.length > 0 && mailDrag.account !== a.id}
		style:padding-left={editing
			? `calc(var(--space-1) + ${indent} * 10px)`
			: `calc(var(--space-2-5) + ${indent} * 14px)`}
		aria-current={current ? 'page' : undefined}
		title={mailDrag.items.length > 0 && mailDrag.account !== a.id
			? t('mail.drop.other')
			: undefined}
		onclick={() => onpick(a.id, f.path)}
		onkeydown={(e) => keys && sortKey(e, keys, f.path, folderName(f), (next) => sorted(a.id, next))}
		ondragover={(e) => dragOver(e, a.id, f)}
		ondragleave={() => over === ref && (over = '')}
		ondrop={(e) => dropped(e, a.id, f)}
	>
		<Icon name={star ? 'star' : roleIcon(f.role)} size={18} />
		<span class="name">{label}</span>
		{#if counts(f)}
			<span class="num"
				><span aria-hidden="true">{f.unread}</span><span class="sr-only"
					>{t('mail.unreadCount', { count: f.unread })}</span
				></span
			>
		{/if}
		{#if !keys}<span class="chev" aria-hidden="true"><Icon name="chevronRight" size={14} /></span
			>{/if}
	</button>
{/snippet}

{#snippet pin(a: MailAccount, f: MailFolder)}
	{@const on = favourites.includes(folderRef(a.id, f.path))}
	<button
		type="button"
		class="side-btn pin"
		class:on
		aria-pressed={on}
		aria-label={on
			? t('mail.fav.remove', { folder: folderName(f) })
			: t('mail.fav.add', { folder: folderName(f) })}
		title={on
			? t('mail.fav.remove', { folder: folderName(f) })
			: t('mail.fav.add', { folder: folderName(f) })}
		onclick={() => toggleFavourite(a.id, f.path)}><Icon name="star" size={14} /></button
	>
{/snippet}

{#snippet allRow(kind: 'inbox' | 'flagged', label: string, icon: IconName, n: number)}
	{@const current = account === ALL && folder === kind}
	<li class="line">
		<button
			type="button"
			class="folder"
			class:current
			aria-current={current ? 'page' : undefined}
			onclick={() => onpick(ALL, kind)}
		>
			<Icon name={icon} size={18} />
			<span class="name">{label}</span>
			{#if n > 0}<span class="num"
					><span aria-hidden="true">{n}</span><span class="sr-only"
						>{t('mail.unreadCount', { count: n })}</span
					></span
				>{/if}
			<span class="chev" aria-hidden="true"><Icon name="chevronRight" size={14} /></span>
		</button>
	</li>
{/snippet}

<nav class="folders" class:editing aria-label={t('mail.folders')} data-sort-list>
	<div class="nav">
		<button
			type="button"
			class="link"
			class:strong={editing}
			aria-pressed={editing}
			onclick={() => (editing = !editing)}
			>{editing ? t('mail.box.done') : t('mail.box.edit')}</button
		>
	</div>
	<h2 class="big">{t('mail.box.title')}</h2>
	{#if editing}<p class="hint">{t('mail.box.hint')}</p>{/if}
	<section class="box top" aria-label={t('mail.box.smart')}>
		<ul>
			{#if many}{@render allRow('inbox', t('mail.box.allInboxes'), 'mail', allUnread)}{/if}
			{@render allRow('flagged', t('mail.box.flagged'), 'flag', 0)}
			{#each favList as { a, f } (folderRef(a.id, f.path))}
				<li class="line">
					{@render folderButton(
						a,
						f,
						0,
						many
							? t('mail.list.heading', { folder: folderName(f), account: a.name || a.email })
							: folderName(f),
						null,
						true,
					)}
					{#if editing}{@render pin(a, f)}{/if}
				</li>
			{/each}
		</ul>
	</section>
	{#each accounts as a (a.id)}
		{@const shut = !editing && shutBoxes.includes(a.id)}
		<section
			class="box acc"
			aria-labelledby="mail-acc-{a.id}"
			data-sort-group="mailboxes"
			data-sort-key={a.id}
		>
			<div class="head">
				<button
					type="button"
					class="acc-btn"
					aria-expanded={!shut}
					disabled={editing}
					onclick={() => toggleBox(a.id)}
				>
					<span class="acc-text">
						<span class="acc-name" id="mail-acc-{a.id}">{a.name || a.email}</span>
						{#if a.name && a.name !== a.email}<span class="addr">{a.email}</span>{/if}
					</span>
					{#if shut && unreadOf(a.id) > 0}<span class="acc-unread"
							>{t('mail.box.unread', { count: unreadOf(a.id) })}</span
						>{/if}
					{#if !editing}<span class="acc-arrow" class:shut aria-hidden="true"
							><Icon name="chevronDown" size={18} /></span
						>{/if}
				</button>
				{#if editing && accounts.length > 1}
					<SortGrip
						group="mailboxes"
						key={a.id}
						keys={boxIds}
						name={a.name || a.email}
						onmove={(next) => setMailAccounts(next)}
					/>
				{/if}
				{#if a.status.state !== 'ok'}<StatusBadge status={a.status} />{/if}
				{#if a.status.state === 'auth_failed'}
					<a class="btn fix" href="/mail/settings?edit={encodeURIComponent(a.id)}"
						>{t('mail.changePassword')}</a
					>
				{/if}
			</div>
			{#if !shut}
				{#if mail.folderErrors[a.id]}
					<p class="notice-error small" role="alert">{mail.folderErrors[a.id]}</p>
				{/if}
				{#if !mail.folders[a.id]}
					<p class="muted small" role="status">{t('common.loading')}</p>
				{:else}
					<ul data-sort-list>
						{#each visible(a.id) as { f, parent, depth, group, keys } (f.path)}
							{@const folded_ = folded.includes(folderRef(a.id, f.path))}
							{@const hidden = hiddenFolders(a.id).includes(f.path)}
							<li
								class="line"
								class:off={editing && hidden}
								data-sort-group={editing ? group : undefined}
								data-sort-key={editing ? f.path : undefined}
							>
								{#if editing}
									<button
										type="button"
										class="circ"
										class:on={!hidden}
										role="checkbox"
										aria-checked={!hidden}
										aria-label={t('mail.box.show', { folder: folderName(f) })}
										onclick={() => toggleHidden(a.id, f.path)}
										>{#if !hidden}<Icon name="check" size={14} />{/if}</button
									>
								{/if}
								{@render folderButton(a, f, depth, folderName(f), editing ? keys : null)}
								{#if parent && !editing}
									<button
										type="button"
										class="side-btn fold"
										aria-expanded={!folded_}
										aria-label={folded_
											? t('mail.tree.expand', { folder: folderName(f) })
											: t('mail.tree.collapse', { folder: folderName(f) })}
										title={folded_
											? t('mail.tree.expand', { folder: folderName(f) })
											: t('mail.tree.collapse', { folder: folderName(f) })}
										onclick={() => toggleFold(a.id, f.path)}
										><Icon name={folded_ ? 'chevronRight' : 'chevronDown'} size={14} /></button
									>
								{/if}
								{#if editing}
									{@render pin(a, f)}
									{#if keys.length > 1}
										<SortGrip
											{group}
											key={f.path}
											{keys}
											name={folderName(f)}
											onmove={(next) => sorted(a.id, next)}
										/>
									{/if}
								{/if}
							</li>
						{/each}
					</ul>
				{/if}
			{/if}
		</section>
	{/each}
	{#if oncompose}
		<div class="foot" role="toolbar" aria-label={t('mail.listActions')}>
			<span></span>
			<span class="state"
				>{t('mail.box.title')}{#if allUnread > 0}<small
						>{t('mail.box.unread', { count: allUnread })}</small
					>{/if}</span
			>
			<button
				type="button"
				class="round"
				aria-label={t('mail.action.compose')}
				title={t('mail.action.compose')}
				onclick={oncompose}><Icon name="edit" size={22} /></button
			>
		</div>
	{/if}
</nav>

<style>
	/* 1.15 (115-4, drafts 20–24): the mailboxes like Apple Mail – a big
	   title, plain rows with a line between them, the folder's icon in the
	   accent, the count as a plain number; each mailbox folds to its head
	   line. The size follows the column (container units), so the same
	   look fits a narrow side bar and the phone. */
	.folders {
		container-type: inline-size;
		display: flex;
		flex-direction: column;
		min-width: 0;
		padding-bottom: var(--space-3);
	}
	.nav {
		display: flex;
		justify-content: flex-end;
		min-height: 44px;
		padding: 0 var(--space-1);
	}
	.link {
		min-height: 44px;
		padding: 0 var(--space-2);
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: clamp(15px, 5cqi, 17px);
		cursor: pointer;
	}
	.link.strong {
		font-weight: 600;
	}
	.link:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
		border-radius: var(--radius-small);
	}
	.big {
		margin: 0;
		padding: 0 var(--space-3) var(--space-2);
		font-size: clamp(22px, 10cqi, 32px);
		font-weight: 700;
		line-height: 1.15;
		overflow-wrap: anywhere;
	}
	.hint {
		margin: 0;
		padding: 0 var(--space-3) var(--space-2-5);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.box {
		display: flex;
		flex-direction: column;
		min-width: 0;
		border-top: 1px solid var(--border);
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		position: relative;
		gap: var(--space-1) var(--space-2);
		padding: 0 var(--space-1) 0 0;
		min-width: 0;
	}
	.acc-btn {
		flex: 1 1 0;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 56px;
		padding: var(--space-2) var(--space-3);
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.acc-btn:disabled {
		cursor: default;
	}
	.acc-btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.acc-text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.acc-name {
		font-size: clamp(16px, 7cqi, 20px);
		font-weight: 700;
		overflow-wrap: anywhere;
	}
	.addr {
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.acc-unread {
		flex: none;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.acc-arrow {
		flex: none;
		display: grid;
		color: var(--accent);
		transition: transform 0.2s ease;
	}
	.acc-arrow.shut {
		transform: rotate(-90deg);
	}
	:global(:root:not([data-fx-transitions='on'])) .acc-arrow {
		transition: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.acc-arrow {
			transition: none;
		}
	}
	.fix {
		min-height: 36px;
		margin: 0 var(--space-3) var(--space-2);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.line {
		position: relative;
		display: flex;
		align-items: center;
		padding-left: var(--space-1);
	}
	.line.off .folder {
		color: var(--muted);
	}
	.folder {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		flex: 1;
		min-width: 0;
		min-height: 44px;
		padding: var(--space-2) var(--space-2-5);
		border: 0;
		border-bottom: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		font-size: clamp(15px, 6cqi, 17px);
		text-align: left;
		cursor: pointer;
	}
	.line:last-child .folder {
		border-bottom: 0;
	}
	.folder:hover {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.folder.current {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		font-weight: 600;
	}
	.folder.over {
		outline: 2px dashed var(--accent);
		outline-offset: -2px;
		background: color-mix(in srgb, var(--accent) 24%, transparent);
	}
	.folder.refuse {
		cursor: not-allowed;
	}
	.folder:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.folder > :global(svg) {
		flex: none;
		color: var(--accent);
	}
	.name {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.num {
		flex: none;
		color: var(--muted);
		font-size: var(--text-md);
	}
	.chev {
		flex: none;
		display: grid;
		color: var(--muted);
	}
	.circ {
		flex: none;
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		padding: 0;
		border: 0;
		background: none;
		color: var(--on-accent);
		cursor: pointer;
	}
	.circ::before {
		content: '';
		grid-area: 1 / 1;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		border: 1.8px solid var(--muted);
		box-sizing: border-box;
	}
	.circ.on::before {
		background: var(--accent);
		border-color: var(--accent);
	}
	.circ :global(svg) {
		grid-area: 1 / 1;
		position: relative;
	}
	.circ:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -4px;
		border-radius: 50%;
	}
	.side-btn {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.side-btn:hover,
	.side-btn:focus-visible {
		background: var(--chip);
		color: var(--text);
	}
	.pin.on {
		color: var(--status-warn);
	}
	.foot {
		position: sticky;
		bottom: 0;
		z-index: 5;
		margin-top: auto;
		display: grid;
		grid-template-columns: 56px minmax(0, 1fr) 56px;
		align-items: center;
		min-height: 56px;
		padding: var(--space-1) var(--space-1) calc(var(--space-1) + env(safe-area-inset-bottom));
		border-top: 1px solid var(--border);
		background: var(--surface);
	}
	.state {
		display: flex;
		flex-direction: column;
		align-items: center;
		font-size: var(--text-sm);
		text-align: center;
		overflow-wrap: anywhere;
	}
	.state small {
		color: var(--muted);
	}
	.round {
		display: grid;
		place-items: center;
		width: 48px;
		height: 48px;
		justify-self: center;
		border: 0;
		border-radius: 50%;
		background: none;
		color: var(--accent);
		cursor: pointer;
	}
	.round:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.small {
		margin: var(--space-1) var(--space-3);
		font-size: var(--text-sm);
	}
	/* A narrow side bar while sorting: the circle, the name, star and grip
	   need the room; the folder's icon gives way. */
	@container (max-width: 300px) {
		.editing .folder > :global(svg) {
			display: none;
		}
		.editing .circ {
			width: 36px;
		}
		.editing .folder {
			padding-left: var(--space-1);
			padding-right: var(--space-1);
		}
	}
	@media (pointer: fine) {
		.editing .side-btn {
			width: 28px;
		}
	}
	@media (max-width: 899px), (pointer: coarse) {
		.folder {
			min-height: 48px;
		}
		.fix {
			min-height: 44px;
		}
		.side-btn {
			width: 44px;
			height: 44px;
		}
	}
</style>
