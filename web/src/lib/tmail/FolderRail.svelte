<script lang="ts">
	// 1.13: the folded side bar of Mail (ColumnFrame's rail): the inbox of
	// each mailbox and the favourite folders as icons; the name, the
	// mailbox and the unread mails in the label and the tooltip, the count
	// also as a number at the icon (never a dot alone).
	import Icon, { type IconName } from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { folderRef, isStrings, readPref } from './prefs.ts';
	import { folderName, mail, orderedAccounts } from './store.svelte.ts';
	import type { MailAccount, MailFolder } from './types.ts';

	let {
		account = '',
		folder = '',
		onpick,
	}: {
		account?: string;
		folder?: string;
		onpick: (account: string, folder: string) => void;
	} = $props();

	const ICONS: Record<string, IconName> = {
		inbox: 'mail',
		drafts: 'edit',
		sent: 'send',
		archive: 'archive',
		junk: 'shield',
		trash: 'trash',
	};
	// Read when the rail appears (the column, where they change, is gone then).
	const favourites = readPref('favourites', [] as string[], isStrings);

	const entries = $derived.by(() => {
		const out: { a: MailAccount; f: MailFolder }[] = [];
		const seen = new Set<string>();
		const add = (a: MailAccount, f: MailFolder | undefined) => {
			if (!f || seen.has(folderRef(a.id, f.path))) return;
			seen.add(folderRef(a.id, f.path));
			out.push({ a, f });
		};
		for (const a of orderedAccounts())
			add(
				a,
				mail.folders[a.id]?.find((f) => f.role === 'inbox'),
			);
		for (const ref of favourites) {
			const [id, path] = ref.split('\u0000');
			const a = mail.accounts?.find((x) => x.id === id);
			if (a)
				add(
					a,
					mail.folders[id]?.find((x) => x.path === path),
				);
		}
		return out;
	});
	const counts = (f: MailFolder) => f.unread > 0 && f.role !== 'junk' && f.role !== 'trash';
	function label(a: MailAccount, f: MailFolder) {
		const name =
			(mail.accounts?.length ?? 0) > 1
				? t('mail.list.heading', { folder: folderName(f), account: a.name || a.email })
				: folderName(f);
		return counts(f) ? `${name}, ${t('mail.unreadCount', { count: f.unread })}` : name;
	}
</script>

<nav class="rail" aria-label={t('mail.folders')}>
	{#each entries as { a, f } (folderRef(a.id, f.path))}
		{@const current = a.id === account && f.path === folder}
		<button
			type="button"
			class="entry"
			class:current
			aria-current={current ? 'page' : undefined}
			aria-label={label(a, f)}
			title={label(a, f)}
			onclick={() => onpick(a.id, f.path)}
			><Icon name={ICONS[f.role] ?? 'folder'} size={18} />{#if counts(f)}<span
					class="count"
					aria-hidden="true">{f.unread}</span
				>{/if}</button
		>
	{/each}
</nav>

<style>
	.rail {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
	}
	.entry {
		position: relative;
		display: grid;
		place-items: center;
		width: 44px;
		min-height: 44px;
		border: 0;
		border-radius: calc(9px * var(--radius-scale, 1));
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.entry:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
		color: var(--text);
	}
	.entry.current {
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		color: var(--accent);
	}
	.entry:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.count {
		position: absolute;
		top: 2px;
		right: 0;
		min-width: 1.5em;
		padding: 0 4px;
		border-radius: 999px;
		background: var(--accent);
		color: var(--on-accent);
		font-size: var(--text-xs);
		font-weight: 600;
		line-height: 1.5;
		text-align: center;
		white-space: nowrap;
	}
</style>
