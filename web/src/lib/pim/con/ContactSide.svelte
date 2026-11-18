<script lang="ts" module>
	// 1.13 (owner 18 Nov 2026): the side bar of Kontakte on the shared
	// ColumnFrame (on the phone its first level): the address books –
	// sorted by dragging their grip or with Alt+↑/↓, in the account's
	// order on the NAS (side-order.svelte.ts; with one book there is
	// nothing to sort and no grip) –, the groups (iOS's group vCards) with
	// "Neue Gruppe", the suggestions of Mail (only with its release) and
	// "Mögliche Dubletten" (only when there are some). A contact dragged
	// from the list onto a group joins it.

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
	import Icon from '#lib/components/Icon.svelte';
	import SortGrip, { sortKey } from '#lib/components/SortGrip.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { applyOrder, orderOf, setOrder } from '#lib/side-order.svelte.ts';
	import type { Book, Group } from '../contacts.ts';
	import { countText } from '../contacts-text.ts';

	type Props = {
		books: Book[];
		groups: Group[];
		place: SidePlace;
		/** Mail is released for the account: the suggestions' entry shows. */
		mail: boolean;
		duplicates: number;
		/** Books can be sorted and contacts dropped (the desktop's columns). */
		wide: boolean;
		/** The overview is still on its way. */
		loading?: boolean;
		onpick: (place: SidePlace) => void;
		onnewgroup: () => void;
		ondropgroup: (group: Group, contact: string) => void;
	};
	let {
		books,
		groups,
		place,
		mail,
		duplicates,
		wide,
		loading = false,
		onpick,
		onnewgroup,
		ondropgroup,
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
</script>

<nav class="side" aria-label={t('desktop.app.contacts')} data-contact-side>
	<section class="box" aria-labelledby="con-books">
		<h2 id="con-books">{t('contacts.books')}</h2>
		<ul data-sort-list>
			{#each shownBooks as b (b.id)}
				{@const current = place.kind === 'book'}
				<li class="line" data-sort-group="books" data-sort-key={b.id}>
					<button
						type="button"
						class="entry"
						class:current
						aria-current={current ? 'page' : undefined}
						data-book={b.id}
						onclick={() => onpick({ kind: 'book' })}
						onkeydown={(e) =>
							wide &&
							keys.length > 1 &&
							sortKey(e, keys, b.id, bookName(b), (next) => setOrder('contacts', next))}
						><Icon name="book" size={16} /><span class="name">{bookName(b)}</span><span
							class="count"
							><span aria-hidden="true">{b.count}</span><span class="sr-only"
								>{countText(b.count)}</span
							></span
						></button
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
				<li class="muted small" role="status">{t('common.loading')}</li>
			{/if}
		</ul>
	</section>

	<section class="box" aria-labelledby="con-groups">
		<div class="head">
			<h2 id="con-groups">{t('contacts.groups.title')}</h2>
			<button
				type="button"
				class="side-btn"
				aria-label={t('contacts.groups.new')}
				title={t('contacts.groups.new')}
				data-new-group
				onclick={onnewgroup}><Icon name="plus" size={15} /></button
			>
		</div>
		{#if !groups.length && !loading}
			<p class="muted small hint">{t('contacts.groups.none')}</p>
		{/if}
		<ul>
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
						><Icon name="users" size={16} /><span class="name"
							>{g.name || t('contacts.groups.unnamed')}</span
						><span class="count"
							><span aria-hidden="true">{g.members.length}</span><span class="sr-only"
								>{countText(g.members.length)}</span
							></span
						></button
					>
				</li>
			{/each}
		</ul>
	</section>

	{#if mail || duplicates > 0}
		<section class="box" aria-label={t('contacts.side.more')}>
			<ul>
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
							><Icon name="mail" size={16} /><span class="name">{t('contacts.suggestions')}</span
							></button
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
							><Icon name="copy" size={16} /><span class="name">{t('contacts.dup.title')}</span
							><span class="count"
								><span aria-hidden="true">{duplicates}</span><span class="sr-only"
									>{duplicates === 1
										? t('contacts.dup.countOne')
										: t('contacts.dup.count', { n: duplicates })}</span
								></span
							></button
						>
					</li>
				{/if}
			</ul>
		</section>
	{/if}
</nav>

<style>
	.side {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
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
		align-items: center;
		gap: var(--space-1);
		padding-right: var(--space-1);
	}
	h2 {
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
		cursor: pointer;
	}
	.entry:hover {
		background: var(--chip);
	}
	.entry:focus-visible,
	.side-btn:focus-visible {
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
	.entry :global(svg) {
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
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 400;
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
	li.small {
		padding: var(--space-1) var(--space-2-5);
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
