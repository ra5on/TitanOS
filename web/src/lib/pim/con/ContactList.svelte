<script lang="ts">
	// 1.11 (step 16): the middle of Kontakte (drafts 10, 13, 50, 53): the
	// search over names, numbers, addresses and companies, the count, the
	// contacts by letter with picture or initials and a birthday of this
	// week in words, and – while searching – the matching suggestions of
	// T-Mail with "Als Kontakt übernehmen". Hits are marked where the row
	// shows them (name, company, e-mail address; the list has no numbers).
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { groupByLetter, markParts, subline, type Item, type Suggestion } from '../contacts.ts';
	import { countText, foundText, soonText } from '../contacts-text.ts';
	import Avatar from './Avatar.svelte';
	import SuggestionRow from './SuggestionRow.svelte';

	type Props = {
		items: Item[];
		/** All contacts (the count without a search). */
		total: number;
		query: string;
		/** The query the shown items answer (the field may be ahead). */
		answered: string;
		suggestions: Suggestion[];
		selected: string;
		today: string;
		phone: boolean;
		taking: string;
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
		today,
		phone,
		taking,
		onquery,
		onopen,
		ontake,
	}: Props = $props();

	const groups = $derived(groupByLetter(items));
	const q = $derived(answered.trim());
</script>

{#snippet hl(text: string)}{#each markParts(text, q) as [part, hit], i (i)}{#if hit}<mark
				>{part}</mark
			>{:else}{part}{/if}{/each}{/snippet}

<div class="list" class:phone data-contact-list>
	<label class="search">
		<Icon name="search" size={16} />
		<input
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
	{#if !items.length}
		<p class="muted empty">{q ? t('contacts.noHits', { q }) : t('contacts.none')}</p>
	{/if}
	{#each groups as group (group.letter)}
		<h3 class="letter">{group.letter}</h3>
		{#each group.items as item (item.id)}
			<button
				type="button"
				class="crow"
				class:sel={item.id === selected}
				aria-current={item.id === selected ? 'true' : undefined}
				data-contact={item.id}
				onclick={() => onopen(item)}
			>
				<Avatar
					id={item.id}
					name={item.name}
					initials={item.initials}
					hasPhoto={item.hasPhoto}
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
	{/each}
	{#if q && suggestions.length}
		<h3 class="letter">{t('contacts.sug.title')}</h3>
		<p class="muted small">{t('contacts.sug.text')}</p>
		{#each suggestions as s (s.addr)}
			<SuggestionRow suggestion={s} query={q} {phone} busy={taking === s.addr} {ontake} />
		{/each}
	{/if}
</div>

<style>
	.list {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-3);
	}
	.list.phone {
		padding: 0;
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
		margin: var(--space-1) 0;
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
