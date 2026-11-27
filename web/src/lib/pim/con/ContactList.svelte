<script lang="ts">
	// 1.11 (step 16): the middle of Kontakte: the search over names,
	// numbers, addresses and companies, the contacts by letter, a birthday
	// of this week in words and – while searching – the matching
	// suggestions of Mail with "Als Kontakt übernehmen". Hits are marked
	// where the row shows them (name; while searching also the company or
	// the e-mail address).
	// 1.15 (PLAN 115-6, drafts 40/41 in Entwuerfe/115-mail-handy): like
	// Apple's Kontakte – on the phone "‹ Listen" and "+" on top, the big
	// title, the search (it stays in sight while the list scrolls),
	// "Meine Karte", the contacts in one section per letter of the family
	// name (titand's sort key is "family given"), the family name bold, the
	// count at the end, and the register A–Z at the right edge: one strip
	// that is tapped or dragged (one target of 44 px, not 27 tiny ones),
	// each letter also a button for the keyboard. A letter without
	// contacts jumps to the next one that has some. In the columns of the
	// desktop the same list without "‹ Listen", "+" next to the title.
	// A letter's rows are painted only near the screen (content-visibility),
	// so a book of thousands scrolls without windowing; every row stays in
	// the page (find in page, dragging onto a group of the side bar).
	// 115-7: one section per letter ("#" once and last), and the rows stand
	// in a boundary: an error there shows a line with "Erneut versuchen",
	// not the error page instead of the whole program.
	import { onMount, tick, type Snippet } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import {
		groupByLetter,
		INDEX_LETTERS,
		markParts,
		nameParts,
		subline,
		type Item,
		type Suggestion,
	} from '../contacts.ts';
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
		/** The compact view (phone, under 620 px): one level at a time. */
		compact: boolean;
		taking: string;
		/** The list's title: "Kontakte" or the group. */
		heading: string;
		/** Next to "+" (the group's menu, the program's ⋯). */
		tools?: Snippet;
		/** "‹ Listen" (compact). */
		onback?: () => void;
		/** "+" (Neuer Kontakt); disabled while the page is not ready. */
		onnew: () => void;
		ready: boolean;
		/** "Meine Karte" above the list (only when one is chosen). */
		me?: Item | null;
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
		compact,
		taking,
		heading,
		tools,
		onback,
		onnew,
		ready,
		me = null,
		empty,
		drag = false,
		onquery,
		onopen,
		ontake,
	}: Props = $props();

	const groups = $derived(groupByLetter(items));
	const q = $derived(answered.trim());
	const present = $derived(new Set(groups.map((g) => g.letter)));

	let field = $state<HTMLInputElement>();
	/** Puts the focus into the search (the page's "/" or the menu). */
	export function focusSearch() {
		field?.focus();
		field?.select();
	}

	// ---- The sticky search and the register's place ----
	let root = $state<HTMLElement>();
	let searchBar = $state<HTMLElement>();
	let rowsEl = $state<HTMLElement>();
	/** The phone's bar stays on top of the page: the search sticks below it. */
	let stickTop = $state(0);
	let searchH = $state(52);
	/**
	 * The free area at the list's right edge (fixed to the window): below
	 * the sticky search and never beside the title or the search, so the
	 * register covers neither.
	 */
	let area = $state<{ top: number; bottom: number; right: number } | null>(null);
	let scroller: HTMLElement | null = null;
	function measure() {
		if (!root || !rowsEl) return;
		const bar = document.querySelector<HTMLElement>('header.phone-bar');
		scroller = root.closest<HTMLElement>('.cf.columns .cf-list');
		stickTop = scroller ? 0 : bar ? Math.round(bar.getBoundingClientRect().height) : 0;
		searchH = searchBar ? Math.round(searchBar.getBoundingClientRect().height) : 52;
		const own = root.getBoundingClientRect();
		const box = scroller?.getBoundingClientRect();
		const rows = rowsEl.getBoundingClientRect();
		const top = Math.max((box ? box.top : stickTop) + searchH, rows.top);
		const bottom = Math.min(box ? box.bottom : window.innerHeight, rows.bottom);
		area = own.width && bottom > top ? { top, bottom, right: window.innerWidth - own.right } : null;
	}
	let frame = 0;
	const later = () => {
		cancelAnimationFrame(frame);
		frame = requestAnimationFrame(measure);
	};
	onMount(() => {
		measure();
		const ro = new ResizeObserver(later);
		if (root) ro.observe(root);
		const host = root?.closest('.cf-list');
		if (host) ro.observe(host);
		const target: HTMLElement | Window = scroller ?? window;
		target.addEventListener('scroll', later, { passive: true });
		window.addEventListener('resize', later);
		return () => {
			cancelAnimationFrame(frame);
			ro.disconnect();
			target.removeEventListener('scroll', later);
			window.removeEventListener('resize', later);
		};
	});
	// New rows (a search, a group) move the area.
	$effect(() => {
		void items.length;
		void me;
		later();
	});

	// ---- The register A–Z ----
	/** Each letter needs this much height; with less, only the letters in the list, else none. */
	const LETTER_H = 15;
	const room = $derived(area ? area.bottom - area.top - 16 : 0);
	const letters = $derived(
		q || groups.length < 2
			? []
			: room >= INDEX_LETTERS.length * LETTER_H
				? INDEX_LETTERS
				: room >= present.size * LETTER_H
					? INDEX_LETTERS.filter((l) => present.has(l))
					: [],
	);
	let current = $state('');
	async function jump(letter: string) {
		const at = INDEX_LETTERS.indexOf(letter);
		const target = INDEX_LETTERS.slice(at).find((l) => present.has(l)) ?? [...present].at(-1);
		if (!target || !root) return;
		current = target;
		const el = root.querySelector<HTMLElement>(`[data-letter="${CSS.escape(target)}"]`);
		el?.scrollIntoView({ block: 'start' });
		// Rows laid out only now (content-visibility) may move the section: once more.
		await tick();
		requestAnimationFrame(() => el?.scrollIntoView({ block: 'start' }));
	}
	let strip = $state<HTMLElement>();
	let scrubbing = false;
	function letterAt(y: number): string {
		const buttons = strip ? [...strip.querySelectorAll<HTMLElement>('[data-index]')] : [];
		for (const b of buttons) {
			const r = b.getBoundingClientRect();
			if (y < r.bottom) return b.dataset.index ?? '';
		}
		return buttons.at(-1)?.dataset.index ?? '';
	}
	function scrub(e: PointerEvent) {
		if (e.type === 'pointerdown') {
			scrubbing = true;
			strip?.setPointerCapture?.(e.pointerId);
		} else if (!scrubbing) return;
		e.preventDefault();
		const l = letterAt(e.clientY);
		if (l && l !== current) void jump(l);
	}
	function endScrub() {
		scrubbing = false;
		current = '';
	}
</script>

{#snippet hl(text: string)}{#each markParts(text, q) as [part, hit], i (i)}{#if hit}<mark
				>{part}</mark
			>{:else}{part}{/if}{/each}{/snippet}

{#snippet rowName(item: Item)}
	{@const [before, family, after] = nameParts(item.name || t('contacts.unnamed'), item.sortKey)}
	{#if before}{@render hl(before)}{/if}{#if family}<b>{@render hl(family)}</b
		>{/if}{#if after}{@render hl(after)}{/if}
{/snippet}

<section
	class="list"
	class:compact
	bind:this={root}
	style:--stick-top="{stickTop}px"
	style:--search-h="{searchH}px"
	aria-labelledby="con-list-head"
	data-contact-list
>
	{#if compact}
		<div class="nav">
			{#if onback}<button type="button" class="link back" onclick={onback} data-back
					><Icon name="chevronLeft" size={22} />{t('contacts.lists.title')}</button
				>{:else}<span></span>{/if}
			<span class="navr">
				{#if tools}{@render tools()}{/if}
				<button
					type="button"
					class="link plus"
					aria-label={t('contacts.new')}
					title={t('contacts.new')}
					disabled={!ready}
					data-new-contact
					onclick={onnew}><Icon name="plus" size={24} /></button
				>
			</span>
		</div>
	{/if}
	<div class="headline">
		<h2 id="con-list-head" class="big">{heading}</h2>
		{#if !compact}
			<span class="navr">
				{#if tools}{@render tools()}{/if}
				<button
					type="button"
					class="link plus"
					aria-label={t('contacts.new')}
					title={t('contacts.new')}
					disabled={!ready}
					data-new-contact
					onclick={onnew}><Icon name="plus" size={22} /></button
				>
			</span>
		{/if}
	</div>
	<div class="searchbar" bind:this={searchBar}>
		<label class="search">
			<Icon name="search" size={17} />
			<input
				bind:this={field}
				type="search"
				placeholder={t('contacts.list.search')}
				aria-label={t('contacts.searchLabel')}
				value={query}
				oninput={(e) => onquery(e.currentTarget.value)}
			/>
		</label>
	</div>
	{#if me && !q}
		<button
			type="button"
			class="me"
			class:sel={me.id === selected}
			aria-current={me.id === selected ? 'true' : undefined}
			data-my-card={me.id}
			onclick={() => onopen(me)}
		>
			<Avatar
				id={me.id}
				name={me.name}
				initials={me.initials}
				hasPhoto={me.hasPhoto}
				version={pictures[me.id] ?? me.etag}
				size={56}
			/>
			<span class="mt"
				><b>{me.name || t('contacts.unnamed')}</b><small>{t('contacts.me.label')}</small></span
			>
		</button>
	{/if}
	<svelte:boundary>
		<div class="rows" class:indexed={letters.length > 0} bind:this={rowsEl}>
			{#if !items.length}
				{#if !q && empty}
					{@render empty()}
				{:else}
					<p class="muted empty">{q ? t('contacts.noHits', { q }) : t('contacts.groups.empty')}</p>
				{/if}
			{/if}
			{#each groups as group (group.letter)}
				<!-- A letter's rows are laid out only near the screen; their height is kept once seen. -->
				<section class="letter-block" style:--rows={group.items.length} data-letter={group.letter}>
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
							<span class="ct"
								><span class="nm">{@render rowName(item)}</span>{#if q && subline(item)}<small
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
					<SuggestionRow
						suggestion={s}
						query={q}
						phone={compact}
						busy={taking === s.addr}
						{ontake}
					/>
				{/each}
			{/if}
			{#if items.length || q}
				<p class="count muted" role="status">
					{q ? foundText(items.length, suggestions.length, q) : countText(total)}
				</p>
			{/if}
		</div>
		{#snippet failed(_error, reset)}
			<div class="failed" role="alert" data-list-failed>
				<p>{t('contacts.list.failed')}</p>
				<button type="button" class="btn" onclick={reset}>{t('common.retry')}</button>
			</div>
		{/snippet}
	</svelte:boundary>
	{#if letters.length && area}
		<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
		<nav
			class="index"
			bind:this={strip}
			aria-label={t('contacts.index.label')}
			style:top="{area.top}px"
			style:height="{area.bottom - area.top}px"
			style:right="{area.right}px"
			style:--n={letters.length}
			onpointerdown={scrub}
			onpointermove={scrub}
			onpointerup={endScrub}
			onpointercancel={endScrub}
			data-contact-index
		>
			{#each letters as l (l)}
				<button
					type="button"
					class:on={current === l}
					data-index={l}
					aria-label={t('contacts.index.jump', { letter: l })}
					onclick={(e) => {
						// A click of the pointer is the strip's; the keyboard's is the button's.
						if (e.detail === 0) void jump(l);
					}}>{l}</button
				>
			{/each}
		</nav>
	{/if}
</section>

<style>
	.list {
		background-color: var(--surface);
		position: relative;
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.nav {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-1);
		min-height: 44px;
		padding: 0 var(--space-2);
	}
	.navr {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		flex: none;
	}
	.link {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 2px;
		min-width: 44px;
		min-height: 44px;
		padding: 0 var(--space-1-5);
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.compact .link {
		font-size: 1.0625rem;
	}
	.link.back {
		padding-left: 0;
		text-align: left;
		overflow-wrap: anywhere;
	}
	.link:disabled {
		color: var(--muted);
		cursor: default;
	}
	.link:focus-visible,
	.crow:focus-visible,
	.me:focus-visible,
	.index button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.headline {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-3) var(--space-1) var(--space-4);
	}
	.compact .headline {
		padding-top: 0;
	}
	.big {
		flex: 1 1 8em;
		margin: 0;
		font-size: 1.75rem;
		font-weight: 700;
		line-height: 1.15;
		overflow-wrap: anywhere;
	}
	.compact .big {
		font-size: 2rem;
	}
	/* The search stays in sight (below the phone's bar). */
	.searchbar {
		position: sticky;
		top: var(--stick-top, 0px);
		z-index: 3;
		padding: var(--space-1-5) var(--space-3) var(--space-2) var(--space-4);
		background: var(--bg);
	}
	.search {
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		min-height: 40px;
		padding: 0 var(--space-2-5);
		border-radius: calc(10px * var(--radius-scale, 1));
		background: var(--chip);
		color: var(--muted);
	}
	.search :global(svg) {
		flex: none;
	}
	.search input {
		flex: 1;
		min-width: 0;
		min-height: 40px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
	}
	.search input::placeholder {
		color: var(--muted);
	}
	.search input:focus {
		outline: none;
	}
	.search:focus-within {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.compact .search,
	.compact .search input {
		min-height: 44px;
	}
	.me {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		min-height: 72px;
		padding: var(--space-2) var(--space-4) var(--space-3);
		border: 0;
		border-bottom: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.mt {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.mt b {
		font-size: 1.25rem;
		font-weight: 600;
	}
	.mt small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.rows {
		display: flex;
		flex-direction: column;
		padding-bottom: var(--space-4);
	}
	/* Room for the register at the right edge (the lines still reach it). */
	.rows.indexed .crow,
	.rows.indexed .letter {
		padding-right: 36px;
	}
	.letter-block {
		display: flex;
		flex-direction: column;
		content-visibility: auto;
		contain-intrinsic-size: auto calc(var(--rows) * 45px + 30px);
		scroll-margin-top: calc(var(--stick-top, 0px) + var(--search-h, 52px));
	}
	.letter {
		margin: 0;
		padding: var(--space-1-5) var(--space-4) 2px;
		background: color-mix(in srgb, var(--text) 6%, var(--bg));
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 600;
	}
	.crow {
		display: flex;
		align-items: center;
		width: 100%;
		min-height: 44px;
		padding: var(--space-2) var(--space-3) var(--space-2) var(--space-4);
		border: 0;
		border-bottom: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
	}
	.crow:hover,
	.me:hover {
		background: var(--srow-hover);
	}
	.crow.sel,
	.me.sel {
		background: var(--accent);
		color: var(--on-accent);
	}
	.crow.sel small,
	.me.sel small {
		color: inherit;
	}
	.ct {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow-wrap: anywhere;
	}
	.nm b {
		font-weight: 700;
	}
	.ct small,
	.small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.small {
		margin: 0;
		padding: var(--space-1) var(--space-4);
		overflow-wrap: break-word;
	}
	.ct .bday {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		color: inherit;
	}
	.count {
		margin: 0;
		padding: var(--space-3) var(--space-4);
		text-align: center;
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.empty {
		margin: var(--space-3) var(--space-4);
		overflow-wrap: break-word;
	}
	.failed {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		margin: var(--space-3) var(--space-4);
	}
	.failed p {
		margin: 0;
		overflow-wrap: break-word;
	}
	/* The register: one strip of 44 px to tap or drag; the letters stand in its middle. */
	.index {
		position: fixed;
		z-index: 4;
		display: flex;
		flex-direction: column;
		justify-content: center;
		align-items: flex-end;
		width: 44px;
		padding: var(--space-2) 2px;
		touch-action: none;
		user-select: none;
		cursor: pointer;
	}
	/* Opaque behind the letters, so they stay readable over a chosen row. */
	.index::before {
		content: '';
		position: absolute;
		right: 2px;
		top: 50%;
		width: 24px;
		height: calc(var(--n) * 15px + 8px);
		transform: translateY(-50%);
		border-radius: 12px;
		background: var(--bg);
	}
	.index button {
		position: relative;
		display: block;
		width: 24px;
		min-height: 0;
		height: 15px;
		padding: 0;
		border: 0;
		border-radius: 4px;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: 11px;
		font-weight: 700;
		line-height: 15px;
		text-align: center;
		pointer-events: none;
	}
	.index button.on {
		background: var(--accent);
		color: var(--on-accent);
	}
	mark {
		background: color-mix(in srgb, var(--accent) 28%, transparent);
		color: inherit;
		border-radius: 2px;
	}
	@media (pointer: coarse) {
		.crow {
			min-height: 48px;
		}
	}
</style>
