<script lang="ts">
	// 1.11 (step 16): the middle of Kontakte (drafts 10, 13, 50, 53): the
	// search over names, numbers, addresses and companies, the count, the
	// contacts by letter with picture or initials and a birthday of this
	// week in words, and – while searching – the matching suggestions of
	// T-Mail with "Als Kontakt übernehmen". Hits are marked where the row
	// shows them (name, company, e-mail address; the list has no numbers).
	// 1.13 (owner 18 Nov 2026): the list column of the shared ColumnFrame,
	// like Mail's: the heading (the book or the group, with the group's
	// menu) and the search on top of the column, sticky while it scrolls.
	// A letter's rows are painted only when near the screen
	// (content-visibility), so a book of thousands scrolls smoothly without
	// windowing – every row stays in the page (find in page, the chosen row
	// scrolled into view, dragging). On the desktop a row is dragged onto a
	// group of the side bar.
	import type { Snippet } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { groupByLetter, markParts, subline, type Item, type Suggestion } from '../contacts.ts';
	import { countText, foundText, soonText } from '../contacts-text.ts';
	import Avatar from './Avatar.svelte';
	import { contactDrag } from './ContactSide.svelte';
	import SuggestionRow from './SuggestionRow.svelte';

	type Props = {
		items: Item[];
		/** All contacts shown without a search (the count). */
		total: number;
		query: string;
		/** The query the shown items answer (the field may be ahead). */
		answered: string;
		suggestions: Suggestion[];
		selected: string;
		/** Pictures changed on this page (id → etag), loaded anew. */
		pictures?: Record<string, string>;
		today: string;
		phone: boolean;
		taking: string;
		/** The list's heading: the address book or the group. */
		heading: string;
		/** Next to the heading (the group's menu). */
		tools?: Snippet;
		/** Above the heading in the compact view: "‹ Listen". */
		back?: Snippet;
		/** Shown when there are no contacts at all (no search). */
		empty?: Snippet;
		/** Rows can be dragged onto a group. */
		drag?: boolean;
		onquery: (q: string) => void;
		onopen: (item: Item) => void;
		ontake: (s: Suggestion) => void;
	};
	let {
		items,
		total,
		query,
		answered,
		suggestions,
		selected,
		pictures = {},
		today,
		phone,
		taking,
		heading,
		tools,
		back,
		empty,
		drag = false,
		onquery,
		onopen,
		ontake,
	}: Props = $props();

	const groups = $derived(groupByLetter(items));
	const q = $derived(answered.trim());

	let field = $state<HTMLInputElement>();
	/** Puts the focus into the search (the page's "/" or the menu). */
	export function focusSearch() {
		field?.focus();
		field?.select();
	}
</script>

{#snippet hl(text: string)}{#each markParts(text, q) as [part, hit], i (i)}{#if hit}<mark
				>{part}</mark
			>{:else}{part}{/if}{/each}{/snippet}

<section class="list" class:phone aria-labelledby="con-list-head" data-contact-list>
	<div class="tools">
		{#if back}{@render back()}{/if}
		<div class="headline">
			<h2 id="con-list-head">{heading}</h2>
			{#if tools}{@render tools()}{/if}
		</div>
		<label class="search">
			<Icon name="search" size={16} />
			<input
				bind:this={field}
				class="field"
				type="search"
				placeholder={t('contacts.search')}
				aria-label={t('contacts.searchLabel')}
				value={query}
				oninput={(e) => onquery(e.currentTarget.value)}
			/>
		</label>
		<p class="count muted" role="status">
			{q ? foundText(items.length, suggestions.length, q) : countText(total)}
		</p>
	</div>
	<div class="rows">
		{#if !items.length}
			{#if !q && empty}
				{@render empty()}
			{:else}
				<p class="muted empty">{q ? t('contacts.noHits', { q }) : t('contacts.groups.empty')}</p>
			{/if}
		{/if}
		{#each groups as group (group.letter)}
			<!-- A letter's rows are laid out only near the screen; their height is kept once seen. -->
			<section class="letter-block" style:--rows={group.items.length}>
				<h3 class="letter">{group.letter}</h3>
				{#each group.items as item (item.id)}
					<button
						type="button"
						class="crow"
						class:sel={item.id === selected}
						aria-current={item.id === selected ? 'true' : undefined}
						data-contact={item.id}
						draggable={drag ? 'true' : undefined}
						ondragstart={(e) => {
							if (!drag || !e.dataTransfer) return;
							contactDrag.id = item.id;
							e.dataTransfer.effectAllowed = 'copy';
							e.dataTransfer.setData('text/plain', item.name || item.id);
						}}
						ondragend={() => (contactDrag.id = '')}
						onclick={() => onopen(item)}
					>
						<Avatar
							id={item.id}
							name={item.name}
							initials={item.initials}
							hasPhoto={item.hasPhoto}
							version={pictures[item.id] ?? item.etag}
							size={36}
						/>
						<span class="ct"
							><b>{@render hl(item.name || t('contacts.unnamed'))}</b>{#if subline(item)}<small
									>{@render hl(subline(item))}</small
								>{/if}{#if item.birthdaySoon}<small class="bday"
									><Icon name="cake" size={13} />{soonText(item.birthdaySoon, today)}</small
								>{/if}</span
						>
					</button>
				{/each}
			</section>
		{/each}
		{#if q && suggestions.length}
			<h3 class="letter">{t('contacts.sug.title')}</h3>
			<p class="muted small">{t('contacts.sug.text')}</p>
			{#each suggestions as s (s.addr)}
				<SuggestionRow suggestion={s} query={q} {phone} busy={taking === s.addr} {ontake} />
			{/each}
		{/if}
	</div>
</section>

<style>
	.list {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.tools {
		position: sticky;
		top: 0;
		z-index: 2;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-2-5) var(--space-3) var(--space-1-5);
		border-bottom: 1px solid var(--border);
		background: var(--bg);
	}
	.headline {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	h2 {
		flex: 1 1 10em;
		margin: 0;
		font-size: var(--text-lg);
		overflow-wrap: anywhere;
	}
	.rows {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-1) var(--space-2) var(--space-3);
	}
	.letter-block {
		display: flex;
		flex-direction: column;
		gap: 2px;
		content-visibility: auto;
		contain-intrinsic-size: auto calc(var(--rows) * 54px + 30px);
	}
	.search {
		position: relative;
		display: flex;
		align-items: center;
		color: var(--muted);
	}
	.search :global(svg) {
		position: absolute;
		left: 10px;
	}
	.search .field {
		width: 100%;
		padding-left: 34px;
		min-height: 40px;
	}
	.phone .search .field {
		min-height: 44px;
	}
	.count {
		margin: 0;
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.empty {
		margin: var(--space-2) var(--space-1);
		overflow-wrap: break-word;
	}
	.letter {
		margin: var(--space-2) var(--space-1) 2px;
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 600;
	}
	.crow {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		min-height: 52px;
		padding: var(--space-1-5) var(--space-2);
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.phone .crow {
		min-height: 56px;
	}
	.crow:hover {
		background: var(--srow-hover);
	}
	.crow:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.crow.sel {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
	}
	.ct {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow-wrap: anywhere;
	}
	.ct b {
		font-weight: 600;
		overflow-wrap: break-word;
	}
	.ct small,
	.small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.small {
		margin: 0 0 var(--space-1);
		overflow-wrap: break-word;
	}
	.ct .bday {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		color: var(--text);
	}
	mark {
		background: color-mix(in srgb, var(--accent) 28%, transparent);
		color: inherit;
		border-radius: 2px;
	}
</style>
