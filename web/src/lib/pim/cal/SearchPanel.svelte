<script lang="ts">
	// 1.15 (PLAN 115-5, the search of drafts 36–39): events by title, place
	// or notes. The NAS has no search of its own, so the page reads one
	// window (three months back, ten ahead: inside the 400 days the API
	// reads at once) of the shown calendars and searches it here; the field
	// says which days it covers. A hit opens the event as from a view.
	import { onMount } from 'svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { addDays } from '../caldates.ts';
	import { eventsByDay, type Calendar, type CalEvent } from '../calendar.ts';
	import { loadEvents } from '../calendar-api.ts';
	import { dateText } from '../calendar-text.ts';
	import AgendaList from './AgendaList.svelte';

	type Props = {
		today: string;
		zone: string;
		calFor: (id: string) => Calendar | undefined;
		phone?: boolean;
		onopen: (ev: CalEvent) => void;
		onclose?: () => void;
	};
	let { today, zone, calFor, phone = false, onopen, onclose }: Props = $props();

	const from = $derived(addDays(today, -92));
	const to = $derived(addDays(today, 304));
	let query = $state('');
	let events = $state<CalEvent[] | null>(null);
	let error = $state('');
	let field = $state<HTMLInputElement>();

	onMount(() => {
		field?.focus();
		loadEvents(from, to)
			.then((got) => (events = got.events))
			.catch((err) => (error = apiErrorText(err)));
	});

	const words = $derived(query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean));
	const hits = $derived.by(() => {
		if (!events || !words.length) return [];
		return events.filter((e) => {
			const hay =
				`${e.title} ${e.location} ${e.notes ?? ''} ${e.birthday?.name ?? ''}`.toLocaleLowerCase();
			return words.every((w) => hay.includes(w));
		});
	});
	const byDay = $derived(eventsByDay(hits, zone, from, to));
	const days = $derived([...byDay.keys()].sort());
</script>

<div class="search" class:phone data-cal-search>
	<div class="sfield">
		<input
			bind:this={field}
			type="search"
			bind:value={query}
			placeholder={t('calendar.search.placeholder')}
			aria-label={t('calendar.search.label')}
			onkeydown={(e) => e.key === 'Escape' && onclose?.()}
		/>
		{#if onclose}<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button
			>{/if}
	</div>
	<p class="muted span">
		{t('calendar.search.span', { from: dateText(from), to: dateText(to) })}
	</p>
	{#if error}
		<p class="notice-error" role="alert">{error}</p>
	{:else if events === null}
		<p class="muted" role="status">{t('common.loading')}</p>
	{:else if words.length && !days.length}
		<p class="muted" role="status">{t('calendar.search.none')}</p>
	{:else if days.length}
		<p class="sr-only" role="status">{t('calendar.search.count', { n: hits.length })}</p>
		<AgendaList {days} {today} {byDay} {calFor} {phone} {onopen} />
	{/if}
</div>

<style>
	.search {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-3);
	}
	.sfield {
		display: flex;
		gap: var(--space-2);
		align-items: center;
	}
	.sfield input {
		flex: 1;
		min-width: 0;
		min-height: 44px;
		padding: 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: 22px;
		background: var(--surface);
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
	}
	.sfield .btn {
		min-height: 44px;
	}
	.span {
		margin: 0;
		font-size: var(--text-sm);
	}
	p {
		margin: 0;
	}
</style>
