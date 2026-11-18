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
	import { mergeGroup, orderOf, setOrder } from '../side-order.svelte.ts';
	import { folderRef, isStrings, readPref, writePref } from './prefs.ts';
	import StatusBadge from './StatusBadge.svelte';
	import { arrangeFolders, folderName, isBelow, mail, mailDrag } from './store.svelte.ts';
	import type { FolderRole, MailAccount, MailFolder } from './types.ts';

	let {
		account = '',
		folder = '',
		sortable = false,
		onpick,
		ondrop,
	}: {
		account?: string;
		folder?: string;
		/** The folders can be sorted (the desktop's columns). */
		sortable?: boolean;
		onpick: (account: string, folder: string) => void;
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
			if (a && f) out.push({ a, f });
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
		return arranged
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
)}
	{@const current = a.id === account && f.path === folder}
	{@const ref = folderRef(a.id, f.path)}
	<button
		type="button"
		class="folder"
		class:current
		class:over={over === ref}
		class:refuse={mailDrag.items.length > 0 && mailDrag.account !== a.id}
		style:padding-left="calc(var(--space-2-5) + {indent} * 14px)"
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
		<Icon name={roleIcon(f.role)} size={16} />
		<span class="name">{label}</span>
		{#if counts(f)}
			<span class="count"
				><span aria-hidden="true">{f.unread}</span><span class="sr-only"
					>{t('mail.unreadCount', { count: f.unread })}</span
				></span
			>
		{/if}
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

<nav class="folders" aria-label={t('mail.folders')}>
	{#if favList.length}
		<section class="box" aria-labelledby="mail-favourites">
			<h2 id="mail-favourites" class="fav-head">
				<Icon name="star" size={14} />{t('mail.fav.title')}
			</h2>
			<ul>
				{#each favList as { a, f } (folderRef(a.id, f.path))}
					<li class="line">
						{@render folderButton(
							a,
							f,
							0,
							(mail.accounts?.length ?? 0) > 1
								? t('mail.list.heading', { folder: folderName(f), account: a.name || a.email })
								: folderName(f),
						)}
						{@render pin(a, f)}
					</li>
				{/each}
			</ul>
		</section>
	{/if}

	{#each mail.accounts ?? [] as a (a.id)}
		<section class="box" aria-labelledby="mail-acc-{a.id}">
			<div class="head">
				<h2 id="mail-acc-{a.id}">{a.name || a.email}</h2>
				{#if a.name && a.name !== a.email}<span class="addr">{a.email}</span>{/if}
				<StatusBadge status={a.status} />
				{#if a.status.state === 'auth_failed'}
					<a class="btn fix" href="/mail/settings?edit={encodeURIComponent(a.id)}"
						>{t('mail.changePassword')}</a
					>
				{/if}
			</div>
			{#if mail.folderErrors[a.id]}
				<p class="notice-error small" role="alert">{mail.folderErrors[a.id]}</p>
			{/if}
			{#if !mail.folders[a.id]}
				<p class="muted small" role="status">{t('common.loading')}</p>
			{:else}
				<ul data-sort-list>
					{#each visible(a.id) as { f, parent, depth, group, keys } (f.path)}
						{@const shut = folded.includes(folderRef(a.id, f.path))}
						<li
							class="line"
							data-sort-group={sortable ? group : undefined}
							data-sort-key={sortable ? f.path : undefined}
						>
							{@render folderButton(a, f, depth, folderName(f), sortable ? keys : null)}
							{#if parent}
								<button
									type="button"
									class="side-btn fold"
									aria-expanded={!shut}
									aria-label={shut
										? t('mail.tree.expand', { folder: folderName(f) })
										: t('mail.tree.collapse', { folder: folderName(f) })}
									title={shut
										? t('mail.tree.expand', { folder: folderName(f) })
										: t('mail.tree.collapse', { folder: folderName(f) })}
									onclick={() => toggleFold(a.id, f.path)}
									><Icon name={shut ? 'chevronRight' : 'chevronDown'} size={14} /></button
								>
							{/if}
							{@render pin(a, f)}
							{#if sortable && keys.length > 1}
								<SortGrip
									{group}
									key={f.path}
									{keys}
									name={folderName(f)}
									onmove={(next) => sorted(a.id, next)}
								/>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/each}
</nav>

<style>
	.folders {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
	}
	.box {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-2);
		padding: 0 var(--space-2-5);
		min-width: 0;
	}
	h2 {
		margin: 0;
		flex: 1 1 100%;
		font-size: var(--text-md);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.fav-head {
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: 0 var(--space-2-5);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.addr {
		flex: 1 1 100%;
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.fix {
		min-height: 32px;
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
	}
	.folder {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex: 1;
		min-width: 0;
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
	}
	.folder:hover {
		background: var(--chip);
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
	.folder :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.name {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.count {
		flex: none;
		min-width: 1.6em;
		padding: 0 var(--space-1-5);
		border-radius: 999px;
		background: var(--accent);
		color: var(--on-accent);
		font-size: var(--text-sm);
		font-weight: 600;
		text-align: center;
	}
	.side-btn {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 28px;
		height: 28px;
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
	/* The pin shows on hover and focus, and always once a folder is a favourite. */
	.pin {
		opacity: 0;
	}
	.line:hover .pin,
	.pin:focus-visible,
	.pin.on {
		opacity: 1;
	}
	.pin.on {
		color: var(--status-warn);
	}
	/* 1.13: the grip shows on hover and focus, like the pin (always with a finger). */
	.line :global(.sort-grip) {
		opacity: 0;
	}
	.line:hover :global(.sort-grip),
	.line:focus-within :global(.sort-grip),
	.line :global(.sort-grip.active) {
		opacity: 1;
	}
	@media (pointer: coarse) {
		.line :global(.sort-grip) {
			opacity: 1;
		}
	}
	.small {
		margin: 0 var(--space-2-5);
		font-size: var(--text-sm);
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
		.pin {
			opacity: 1;
		}
	}
</style>
