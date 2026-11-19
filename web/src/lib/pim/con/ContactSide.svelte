<script lang="ts" module>
	// 1.13 (owner 18 Nov 2026): the side bar of Kontakte on the shared
	// ColumnFrame (on the phone its first level): the address books –
	// sorted by dragging their grip or with Alt+↑/↓, in the account's
	// order on the NAS (side-order.svelte.ts; with one book there is
	// nothing to sort and no grip) –, the groups (iOS's group vCards) with
	// "Neue Gruppe", the suggestions of Mail (only with its release) and
	// "Mögliche Dubletten" (only when there are some). A contact dragged
	// from the list onto a group joins it.
	// 1.15 (PLAN 115-6, draft 43): Apple's "Listen" – "Alle Kontakte" on
	// top, then the address books, the groups (with "+") and "Mehr": the
	// suggestions of Mail, the possible duplicates, "Import und Export"
	// and "Am iPhone einrichten". Compact (the phone, under 620 px) with the
	// big title "Listen" and grouped rows of 48 px; in the columns of the
	// desktop the same entries flat in the side bar.

	/** The contact being dragged from the list (its id), for the groups as drop targets. */
	export const contactDrag = $state({ id: '' });

	/** What the side bar points at. */
	export type SidePlace =
		| { kind: 'book' }
		| { kind: 'group'; id: string }
		| { kind: 'suggestions' }
		| { kind: 'duplicates' };
</script>

<script lang="ts">
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import SortGrip, { sortKey } from '#lib/components/SortGrip.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { applyOrder, orderOf, setOrder } from '#lib/side-order.svelte.ts';
	import type { Book, Group } from '../contacts.ts';
	import { countText } from '../contacts-text.ts';

	type Props = {
		books: Book[];
		groups: Group[];
		place: SidePlace;
		/** All contacts of the account ("Alle Kontakte"). */
		total: number;
		/** Mail is released for the account: the suggestions' entry shows. */
		mail: boolean;
		duplicates: number;
		/** Books can be sorted and contacts dropped (the desktop's columns). */
		wide: boolean;
		/** The phone's look: big title, grouped rows. */
		compact?: boolean;
		/** The overview is still on its way. */
		loading?: boolean;
		onpick: (place: SidePlace) => void;
		onnewgroup: () => void;
		ondropgroup: (group: Group, contact: string) => void;
		/** "Import und Export". */
		ontransfer: () => void;
	};
	let {
		books,
		groups,
		place,
		total,
		mail,
		duplicates,
		wide,
		compact = false,
		loading = false,
		onpick,
		onnewgroup,
		ondropgroup,
		ontransfer,
	}: Props = $props();

	const shownBooks = $derived(applyOrder(books, orderOf('contacts'), (b) => b.id));
	const keys = $derived(shownBooks.map((b) => b.id));
	const bookName = (b: Book) =>
		b.id === 'kontakte' ? t('contacts.side.contacts') : b.name || b.id;

	let over = $state('');
	const takes = (g: Group) => wide && !!contactDrag.id && !g.members.includes(contactDrag.id);
	function dragOver(e: DragEvent, g: Group) {
		if (!takes(g)) return;
		e.preventDefault();
		if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
		over = g.id;
	}
	function drop(e: DragEvent, g: Group) {
		over = '';
		if (!takes(g)) return;
		e.preventDefault();
		ondropgroup(g, contactDrag.id);
	}
	const allHere = $derived(place.kind === 'book');
</script>

{#snippet inner(icon: IconName, name: string, n: number | null, label: string)}
	<span class="ic"><Icon name={icon} size={compact ? 22 : 17} /></span><span class="name"
		>{name}</span
	>{#if n !== null}<span class="count"
			><span aria-hidden="true">{n}</span><span class="sr-only">{label}</span></span
		>{/if}{#if compact}<span class="chev"><Icon name="chevronRight" size={16} /></span>{/if}
{/snippet}

<nav class="side" class:compact aria-label={t('contacts.lists.title')} data-contact-side>
	{#if compact}
		<div class="nav">
			<button
				type="button"
				class="link"
				aria-label={t('contacts.lists.newGroup')}
				title={t('contacts.lists.newGroup')}
				data-new-group
				onclick={onnewgroup}><Icon name="plus" size={24} /></button
			>
		</div>
		<h2 class="big">{t('contacts.lists.title')}</h2>
	{/if}
	<ul class="box">
		<li class="line">
			<button
				type="button"
				class="entry"
				class:current={allHere}
				aria-current={allHere ? 'page' : undefined}
				data-side-all
				onclick={() => onpick({ kind: 'book' })}
				>{@render inner('users', t('contacts.lists.all'), total, countText(total))}</button
			>
		</li>
	</ul>

	<section class="part" aria-labelledby="con-books">
		<h3 id="con-books" class="head">{t('contacts.books')}</h3>
		<ul class="box" data-sort-list>
			{#each shownBooks as b (b.id)}
				<li class="line" data-sort-group="books" data-sort-key={b.id}>
					<button
						type="button"
						class="entry"
						data-book={b.id}
						onclick={() => onpick({ kind: 'book' })}
						onkeydown={(e) =>
							wide &&
							keys.length > 1 &&
							sortKey(e, keys, b.id, bookName(b), (next) => setOrder('contacts', next))}
						>{@render inner('book', bookName(b), b.count, countText(b.count))}</button
					>
					{#if wide && keys.length > 1}
						<SortGrip
							group="books"
							key={b.id}
							{keys}
							name={bookName(b)}
							onmove={(next) => setOrder('contacts', next)}
						/>
					{/if}
				</li>
			{/each}
			{#if loading && !books.length}
				<li class="muted small wait" role="status">{t('common.loading')}</li>
			{/if}
		</ul>
	</section>

	<section class="part" aria-labelledby="con-groups">
		<div class="headrow">
			<h3 id="con-groups" class="head">{t('contacts.groups.title')}</h3>
			{#if !compact}
				<button
					type="button"
					class="side-btn"
					aria-label={t('contacts.groups.new')}
					title={t('contacts.groups.new')}
					data-new-group
					onclick={onnewgroup}><Icon name="plus" size={15} /></button
				>
			{/if}
		</div>
		{#if !groups.length && !loading}
			<p class="muted small hint">{t('contacts.groups.none')}</p>
		{:else}
			<ul class="box">
				{#each groups as g (g.id)}
					{@const current = place.kind === 'group' && place.id === g.id}
					<li class="line">
						<button
							type="button"
							class="entry"
							class:current
							class:over={over === g.id}
							aria-current={current ? 'page' : undefined}
							data-group={g.id}
							onclick={() => onpick({ kind: 'group', id: g.id })}
							ondragover={(e) => dragOver(e, g)}
							ondragleave={() => over === g.id && (over = '')}
							ondrop={(e) => drop(e, g)}
							>{@render inner(
								'users',
								g.name || t('contacts.groups.unnamed'),
								g.members.length,
								countText(g.members.length),
							)}</button
						>
					</li>
				{/each}
			</ul>
		{/if}
	</section>

	<section class="part" aria-labelledby="con-more">
		<h3 id="con-more" class="head">{t('contacts.lists.more')}</h3>
		<ul class="box">
			{#if mail}
				{@const current = place.kind === 'suggestions'}
				<li class="line">
					<button
						type="button"
						class="entry"
						class:current
						aria-current={current ? 'page' : undefined}
						data-side-suggestions
						onclick={() => onpick({ kind: 'suggestions' })}
						>{@render inner('mail', t('contacts.suggestions'), null, '')}</button
					>
				</li>
			{/if}
			{#if duplicates > 0}
				{@const current = place.kind === 'duplicates'}
				<li class="line">
					<button
						type="button"
						class="entry"
						class:current
						aria-current={current ? 'page' : undefined}
						data-side-duplicates
						onclick={() => onpick({ kind: 'duplicates' })}
						>{@render inner(
							'copy',
							t('contacts.dup.title'),
							duplicates,
							duplicates === 1
								? t('contacts.dup.countOne')
								: t('contacts.dup.count', { n: duplicates }),
						)}</button
					>
				</li>
			{/if}
			<li class="line">
				<button type="button" class="entry" data-side-transfer onclick={ontransfer}
					>{@render inner('download', t('contacts.side.transfer'), null, '')}</button
				>
			</li>
			<li class="line">
				<a class="entry" href="/contacts/iphone" data-side-iphone
					>{@render inner('device', t('calendar.side.iphone'), null, '')}</a
				>
			</li>
		</ul>
	</section>
</nav>

<style>
	.side {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
	}
	.part {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.headrow {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding-right: var(--space-1);
	}
	.head {
		flex: 1;
		margin: 0;
		padding: 0 var(--space-2-5);
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.line {
		display: flex;
		align-items: center;
	}
	.entry {
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
		text-decoration: none;
		cursor: pointer;
	}
	.entry:hover {
		background: var(--chip);
	}
	.entry:focus-visible,
	.side-btn:focus-visible,
	.link:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.entry.current {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		font-weight: 600;
	}
	.entry.over {
		outline: 2px dashed var(--accent);
		outline-offset: -2px;
		background: color-mix(in srgb, var(--accent) 24%, transparent);
	}
	.ic {
		flex: none;
		display: inline-flex;
		color: var(--accent);
	}
	.name {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.count {
		flex: none;
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 400;
	}
	.chev {
		flex: none;
		display: inline-flex;
		color: var(--muted);
	}
	.side-btn {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.side-btn:hover {
		background: var(--chip);
		color: var(--text);
	}
	.small {
		font-size: var(--text-sm);
	}
	.hint {
		margin: 0;
		padding: 0 var(--space-2-5);
		overflow-wrap: break-word;
	}
	li.wait {
		padding: var(--space-1) var(--space-2-5);
	}
	/* Compact: Apple's "Listen" – the big title, grouped rows on the surface. */
	.compact {
		gap: 0;
		padding-bottom: var(--space-5);
	}
	.nav {
		display: flex;
		justify-content: flex-end;
		min-height: 44px;
		padding: 0 var(--space-2);
	}
	.link {
		display: inline-grid;
		place-items: center;
		min-width: 44px;
		min-height: 44px;
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--accent);
		cursor: pointer;
	}
	.big {
		margin: 0;
		padding: 0 var(--space-4) var(--space-2);
		font-size: 2rem;
		font-weight: 700;
		line-height: 1.15;
		overflow-wrap: anywhere;
	}
	.compact .part {
		gap: 0;
		margin-top: var(--space-4);
	}
	.compact .head {
		padding: 0 var(--space-4) var(--space-1-5) calc(var(--space-4) * 2);
		font-size: var(--text-sm);
		font-weight: 500;
		text-transform: uppercase;
		letter-spacing: 0.03em;
	}
	.compact .box {
		margin: 0 var(--space-4);
		border-radius: calc(12px * var(--radius-scale, 1));
		background: var(--surface);
		overflow: clip;
	}
	.compact > .box {
		margin-top: var(--space-3);
	}
	.compact .entry {
		gap: var(--space-3);
		min-height: 48px;
		padding: var(--space-2) var(--space-3) var(--space-2) var(--space-4);
		border-radius: 0;
		border-bottom: 1px solid var(--border);
		font-size: 1.0625rem;
	}
	.compact .line:last-child .entry {
		border-bottom: 0;
	}
	.compact .entry.current {
		font-weight: 400;
	}
	.compact .count {
		font-size: var(--text-md);
	}
	.compact .hint {
		padding: 0 var(--space-4) 0 calc(var(--space-4) * 2);
	}
	@media (pointer: coarse) {
		.entry {
			min-height: 44px;
		}
		.side-btn {
			width: 44px;
			height: 44px;
		}
	}
</style>
