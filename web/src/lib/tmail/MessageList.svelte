<script lang="ts">
	// The list of a folder: sender, subject, date and marks; sender and subject
	// wrap, there is no preview line (it could only be cut). Unread is bold
	// with a dot and says "Ungelesen" to screen readers; marks are symbols
	// with words. More pages load when the end comes into sight (and by the
	// button there); search and the filters ask titand (IMAP SEARCH).
	import { untrack } from 'svelte';
	import Icon from '../components/Icon.svelte';
	import { apiErrorText, i18n, t } from '../i18n/index.svelte.ts';
	import { mailApi } from './api.ts';
	import { listDate, senderName } from './text.ts';
	import type { Flags, MessageRow } from './types.ts';

	type Filter = 'all' | 'unread' | 'flagged';

	let {
		accountId,
		folder,
		selected = null,
		heading,
		onopen,
	}: {
		accountId: string;
		folder: string;
		selected?: number | null;
		heading: string;
		onopen: (uid: number) => void;
	} = $props();

	let rows = $state<MessageRow[]>([]);
	let next = $state<number | null>(null);
	let total = $state(0);
	let loading = $state(false);
	let error = $state('');
	let loaded = $state(false);
	let search = $state('');
	let q = $state('');
	let filter = $state<Filter>('all');
	let asked = 0;
	let more = $state<HTMLElement>();

	async function load(append: boolean, quiet = false) {
		const n = ++asked;
		if (!quiet) loading = true;
		error = '';
		try {
			const page = await mailApi.messages(accountId, {
				folder,
				before: append ? next : null,
				q,
				unread: filter === 'unread',
				flagged: filter === 'flagged',
			});
			if (n !== asked) return;
			if (append) {
				const have = new Set(rows.map((r) => r.uid));
				rows = [...rows, ...page.messages.filter((m) => !have.has(m.uid))];
				next = page.next || null;
			} else {
				// Read again: new mail on top; older rows loaded further down stay,
				// and with them the place of the next page.
				const oldest = page.messages.at(-1)?.uid ?? 0;
				const older = quiet && page.next ? rows.filter((r) => r.uid < oldest) : [];
				rows = [...page.messages, ...older];
				if (!older.length) next = page.next || null;
			}
			total = page.total;
			loaded = true;
		} catch (err) {
			if (n === asked) error = apiErrorText(err);
		} finally {
			if (n === asked) loading = false;
		}
	}

	// A new folder, search or filter starts from the top.
	$effect(() => {
		void accountId;
		void folder;
		void q;
		void filter;
		untrack(() => {
			rows = [];
			next = null;
			loaded = false;
			void load(false);
		});
	});

	// The end of the list in sight loads the next page.
	$effect(() => {
		const el = more;
		if (!el || typeof IntersectionObserver === 'undefined') return;
		const observer = new IntersectionObserver((entries) => {
			if (entries.some((e) => e.isIntersecting) && next !== null && !loading) void load(true);
		});
		observer.observe(el);
		return () => observer.disconnect();
	});

	let typing: ReturnType<typeof setTimeout> | undefined;
	function typed() {
		clearTimeout(typing);
		typing = setTimeout(() => (q = search.trim()), 400);
	}
	function submit(e: SubmitEvent) {
		e.preventDefault();
		clearTimeout(typing);
		q = search.trim();
	}

	/** Reads the first page again (new mail, after an action); `quiet` keeps the rows shown meanwhile. */
	export function refresh(quiet = true) {
		void load(false, quiet);
	}
	/** Marks changed elsewhere (the open message, the live stream). */
	export function setFlags(uids: number[], change: Partial<Flags>) {
		for (const r of rows) if (uids.includes(r.uid)) r.flags = { ...r.flags, ...change };
	}
	/** Messages moved or deleted leave the list. */
	export function drop(uids: number[]) {
		rows = rows.filter((r) => !uids.includes(r.uid));
		total = Math.max(0, total - uids.length);
	}
	/** The uid after (or before) one, for going on after a delete. */
	export function neighbour(uid: number): number | null {
		const at = rows.findIndex((r) => r.uid === uid);
		if (at < 0) return null;
		return rows[at + 1]?.uid ?? rows[at - 1]?.uid ?? null;
	}

	const filters: {
		id: Filter;
		key: 'mail.filter.all' | 'mail.filter.unread' | 'mail.filter.flagged';
	}[] = [
		{ id: 'all', key: 'mail.filter.all' },
		{ id: 'unread', key: 'mail.filter.unread' },
		{ id: 'flagged', key: 'mail.filter.flagged' },
	];
</script>

<section class="list" aria-labelledby="mail-list-head">
	<div class="tools">
		<h2 id="mail-list-head">{heading}</h2>
		<form role="search" onsubmit={submit}>
			<label class="sr-only" for="mail-search">{t('mail.search.label')}</label>
			<input
				id="mail-search"
				class="field"
				type="search"
				placeholder={t('mail.search.placeholder')}
				bind:value={search}
				oninput={typed}
			/>
		</form>
		<div class="filters" role="group" aria-label={t('mail.filter.label')}>
			{#each filters as f (f.id)}
				<button
					type="button"
					class="btn filter"
					aria-pressed={filter === f.id}
					onclick={() => (filter = f.id)}>{t(f.key)}</button
				>
			{/each}
		</div>
		{#if loaded}
			<p class="muted count" role="status">
				{q ? t('mail.list.found', { count: total }) : t('mail.list.total', { count: total })}
			</p>
		{/if}
	</div>

	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if loaded && rows.length === 0 && !error}
		<p class="muted empty">
			{q || filter !== 'all' ? t('mail.list.noneFound') : t('mail.list.empty')}
		</p>
	{/if}
	<ul class="rows">
		{#each rows as m (m.uid)}
			{@const unread = m.flags?.seen === false}
			<li>
				<button
					type="button"
					class="row"
					class:unread
					class:current={m.uid === selected}
					aria-current={m.uid === selected ? 'true' : undefined}
					data-uid={m.uid}
					onclick={() => onopen(m.uid)}
				>
					<span class="top">
						{#if unread}<span class="dot" aria-hidden="true"></span><span class="sr-only"
								>{t('mail.mark.unread')}</span
							>{/if}
						<span class="from">{senderName(m.from) || t('mail.noSender')}</span>
						{#if m.date}<time datetime={m.date}>{listDate(m.date, i18n.locale)}</time>{/if}
					</span>
					{#if m.tooLarge}
						<!-- Its head is too large to list (titand reads only the uid). -->
						<span class="subject large"
							><Icon name="info" size={14} />{t('mail.list.tooLarge')}</span
						>
					{:else}
						<span class="subject">{m.subject?.trim() || t('mail.noSubject')}</span>
					{/if}
					{#if m.flags?.flagged || m.flags?.answered || m.attachments}
						<span class="marks">
							{#if m.flags?.flagged}<span class="mark flagged" title={t('mail.mark.flagged')}
									><Icon name="flag" size={14} /><span class="sr-only"
										>{t('mail.mark.flagged')}</span
									></span
								>{/if}
							{#if m.flags?.answered}<span class="mark" title={t('mail.mark.answered')}
									><Icon name="reply" size={14} /><span class="sr-only"
										>{t('mail.mark.answered')}</span
									></span
								>{/if}
							{#if m.attachments}<span class="mark" title={t('mail.mark.attachment')}
									><Icon name="clip" size={14} /><span class="sr-only"
										>{t('mail.mark.attachment')}</span
									></span
								>{/if}
						</span>
					{/if}
				</button>
			</li>
		{/each}
	</ul>
	{#if loading}<p class="muted loading" role="status">{t('common.loading')}</p>{/if}
	{#if next !== null}
		<button
			type="button"
			class="btn more"
			bind:this={more}
			disabled={loading}
			onclick={() => void load(true)}>{t('mail.list.more')}</button
		>
	{/if}
</section>

<style>
	.list {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.tools {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2-5) var(--space-3);
		border-bottom: 1px solid var(--border);
	}
	h2 {
		flex: 1 1 100%;
		margin: 0;
		font-size: var(--text-lg);
		overflow-wrap: anywhere;
	}
	form {
		flex: 1 1 100%;
		min-width: 0;
	}
	form input {
		width: 100%;
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
	}
	.filter[aria-pressed='true'] {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		font-weight: 600;
	}
	.count {
		margin: 0 0 0 auto;
		font-size: var(--text-sm);
	}
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.rows li {
		border-bottom: 1px solid var(--border);
	}
	.row {
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: 100%;
		min-height: 44px;
		padding: var(--space-2) var(--space-3);
		border: 0;
		border-left: 3px solid transparent;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
	}
	.row:hover {
		background: var(--chip);
	}
	.row.current {
		border-left-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.top {
		display: flex;
		align-items: baseline;
		gap: var(--space-2);
		min-width: 0;
	}
	.from {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	time {
		flex: none;
		color: var(--muted);
		font-size: var(--text-sm);
		white-space: nowrap;
	}
	.subject {
		overflow-wrap: anywhere;
		color: var(--muted);
	}
	.subject.large {
		display: inline-flex;
		align-items: flex-start;
		gap: var(--space-1);
		font-style: italic;
	}
	.unread .from,
	.unread .subject {
		font-weight: 700;
		color: var(--text);
	}
	.dot {
		flex: none;
		align-self: center;
		width: 9px;
		height: 9px;
		border-radius: 50%;
		background: var(--accent);
	}
	.marks {
		display: flex;
		gap: var(--space-2);
		color: var(--muted);
	}
	.mark {
		display: inline-flex;
	}
	.mark.flagged {
		color: var(--status-warn);
	}
	.empty,
	.loading {
		margin: var(--space-3);
	}
	.more {
		margin: var(--space-3);
		align-self: center;
	}
	@media (max-width: 899px) {
		.row {
			min-height: 56px;
		}
		.btn {
			min-height: 44px;
		}
	}
</style>
