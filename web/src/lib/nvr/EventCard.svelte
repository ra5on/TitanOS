<script lang="ts">
	// PLAN 79-7: one event as a small card – its picture, what was seen, where
	// and when; a recognized face or plate as a second line.
	import { apiPath } from '../cluster-node.ts';
	import Icon from '../components/Icon.svelte';
	import { i18n, t } from '../i18n/index.svelte.ts';
	import { dayClock, labelName } from './texts.ts';
	import type { NvrEvent } from './types.ts';

	let {
		event,
		camera,
		href,
		onopen,
	}: {
		event: NvrEvent;
		/** The camera's name. */
		camera: string;
		href?: string;
		onopen?: () => void;
	} = $props();
	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const extra = $derived(
		[
			event.subLabel,
			event.plate && event.plate !== event.subLabel ? event.plate : '',
			event.end === null ? t('nvr.events.running') : '',
		]
			.filter(Boolean)
			.join(' · '),
	);
	let broken = $state(false);
</script>

{#snippet body()}
	<span class="thumb"
		>{#if !broken}<img
				src={apiPath(`/nvr/events/${encodeURIComponent(event.id)}/thumbnail.jpg`)}
				alt=""
				loading="lazy"
				onerror={() => (broken = true)}
			/>{/if}</span
	>
	<span class="what">
		<b
			>{labelName(event.label)} · {camera}{#if !event.reviewed}<span class="sr-only"
					>, {t('nvr.events.unseen')}</span
				>{/if}</b
		>
		<small>{dayClock(event.start, locale)}{extra ? ` · ${extra}` : ''}</small>
		{#if event.kept}<span class="kept"
				><Icon name="pin" size={13} />{t('nvr.events.keptBadge')}</span
			>{/if}
	</span>
{/snippet}

{#if href}<a class="event-card" {href} class:new={!event.reviewed}>{@render body()}</a>
{:else}<button type="button" class="event-card" class:new={!event.reviewed} onclick={onopen}
		>{@render body()}</button
	>{/if}

<style>
	.event-card {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		width: 100%;
		padding: var(--space-1-5);
		border: 1px solid var(--srow-line, var(--border));
		border-radius: 10px;
		background: var(--srow-bg, var(--surface));
		color: var(--text);
		font: inherit;
		text-align: left;
		text-decoration: none;
		cursor: pointer;
		min-width: 0;
	}
	.event-card:hover {
		background: var(--srow-hover, var(--surface));
	}
	.event-card:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.thumb {
		flex: none;
		width: 72px;
		aspect-ratio: 16 / 10;
		border-radius: 7px;
		overflow: hidden;
		background: #0b0f14;
	}
	.thumb img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.what {
		display: flex;
		flex-direction: column;
		gap: 1px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.what b {
		font-weight: 600;
		font-size: var(--text-base);
	}
	.event-card.new .what b::after {
		content: '';
		display: inline-block;
		width: 7px;
		height: 7px;
		margin-left: 6px;
		border-radius: 50%;
		background: var(--accent);
		vertical-align: middle;
	}
	.what small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	/* PLAN 88: a kept event says so in words, not only with a mark. */
	.kept {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		align-self: flex-start;
		color: var(--text);
		font-size: var(--text-sm);
		font-weight: 600;
	}
</style>
