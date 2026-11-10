<script lang="ts">
	// The release notes of one version, as text (PLAN 78-3): the three tagged
	// blocks Neu / Sicherheit / Behoben, everything else under its own heading.
	// Nothing of the notes is ever inserted as HTML; bold and code become
	// elements here, links only lead to the project's own pages. A long block
	// shows its first points and a button for the rest – no text is cut.
	import { SvelteSet } from 'svelte/reactivity';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import {
		firstItems,
		projectLink,
		releaseUrl,
		type Changelog,
		type Item,
		type Part,
		type Section,
	} from './changelog.ts';

	type Props = {
		log: Changelog;
		/** The version of the release, for the link to its page. */
		version: string;
		/** Points of a block that show before "Alle … anzeigen". */
		limit?: number;
	};
	let { log, version, limit = 6 }: Props = $props();
	const uid = $props.id();

	// Which blocks show all their points (by position in the notes).
	const all = new SvelteSet<number>();
	function toggle(index: number) {
		if (all.has(index)) all.delete(index);
		else all.add(index);
	}
	const TAGS = { new: 'info', security: 'warn', fixed: 'ok' } as const;
	function tagged(section: Section): section is Section & { kind: keyof typeof TAGS } {
		return section.kind in TAGS;
	}
</script>

{#snippet line(parts: Part[])}
	{#each parts as part, i (i)}{#if part.href && projectLink(part.href)}<a
				href={part.href}
				target="_blank"
				rel="noopener noreferrer"
				>{#if part.bold}<strong>{part.text}</strong>{:else}{part.text}{/if}</a
			>{:else if part.code}<code class:strong={part.bold}>{part.text}</code
			>{:else if part.bold}<strong>{part.text}</strong>{:else}{part.text}{/if}{/each}
{/snippet}
{#snippet point(item: Item)}
	<li>
		{@render line(item.parts)}
		{#if item.children?.length}<ul>
				{#each item.children as child, i (i)}{@render point(child)}{/each}
			</ul>{/if}
	</li>
{/snippet}

<div class="notes">
	{#if log.lead}<p class="lead">{log.lead}</p>{/if}
	{#each log.sections as section, index (index)}
		{@const open = all.has(index)}
		{@const long = section.count > limit}
		{@const shown = firstItems(section, open || !long ? section.count : limit)}
		<section class="block" data-kind={section.kind} aria-labelledby="{uid}-h{index}">
			{#if tagged(section)}
				<h4 id="{uid}-h{index}">
					<span class="chip {TAGS[section.kind]}">{t(`update.news.tag.${section.kind}`)}</span>
				</h4>
			{:else if section.title}
				<h4 id="{uid}-h{index}">{section.title}</h4>
			{:else}
				<h4 id="{uid}-h{index}" class="sr-only">{t('update.news.about')}</h4>
			{/if}
			<div id="{uid}-b{index}" class="body">
				{#each shown as part, g (g)}
					{#if part.group.title}<p class="sub">{part.group.title}</p>{/if}
					{#each part.group.text as paragraph, p (p)}<p>{@render line(paragraph)}</p>{/each}
					{#if part.items.length}<ul>
							{#each part.items as item, i (i)}{@render point(item)}{/each}
						</ul>{/if}
				{/each}
			</div>
			{#if long}
				<button
					type="button"
					class="more"
					aria-expanded={open}
					aria-controls="{uid}-b{index}"
					onclick={() => toggle(index)}
					>{open ? t('update.news.less') : t('update.news.all', { count: section.count })}</button
				>
			{/if}
		</section>
	{/each}
	<p class="source">
		<a href={releaseUrl(version)} target="_blank" rel="noopener noreferrer"
			>{t('update.news.github')}<Icon name="external" size={13} /></a
		>
	</p>
</div>

<style>
	.notes {
		display: flex;
		flex-direction: column;
		min-width: 0;
		font-size: var(--text-md);
		line-height: 1.5;
		overflow-wrap: anywhere;
	}
	.lead {
		margin: 0 0 var(--space-2);
		font-weight: 600;
	}
	.block {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-1-5);
		min-width: 0;
		padding: var(--space-2-5) 0;
	}
	.block + .block {
		border-top: 1px solid var(--srow-line);
	}
	.block:first-child {
		padding-top: 0;
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		align-self: stretch;
		min-width: 0;
	}
	h4 {
		margin: 0;
		font-size: var(--text-md);
		font-weight: 600;
	}
	h4 .chip {
		font-weight: 600;
	}
	/* The tag of "Neu" and "Behoben" in the text colour: an accent or status
	   tint behind it, the word itself readable on every theme. */
	h4 .chip.info {
		color: var(--text);
	}
	p {
		margin: 0;
		max-width: 60rem;
	}
	.sub {
		margin-top: var(--space-1);
		font-weight: 600;
	}
	ul {
		margin: 0;
		padding-left: 1.2rem;
		max-width: 60rem;
	}
	li {
		margin: 3px 0;
	}
	li ul {
		margin-top: 3px;
	}
	code {
		font-family: var(--font-mono);
		font-size: 0.92em;
		padding: 0 3px;
		border-radius: 3px;
		background: var(--track);
	}
	code.strong {
		font-weight: 600;
	}
	.more {
		padding: 0;
		border: 0;
		background: none;
		font: inherit;
		cursor: pointer;
		text-align: left;
	}
	.more:hover {
		text-decoration: underline;
	}
	.more:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: var(--radius-bar);
	}
	.source {
		padding-top: var(--space-2);
	}
	/* Links on the tinted row: the accent darkened or lightened towards the
	   text colour, so that it keeps 4.5:1 on the light themes too. */
	.more,
	.notes a {
		color: color-mix(in srgb, var(--accent) 65%, var(--text));
	}
	.source a {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
	}
</style>
