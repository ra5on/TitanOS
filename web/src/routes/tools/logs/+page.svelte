<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { parseLogs, type LogEntry as Entry } from '#lib/logs.ts';
	import { api, ApiError } from '#lib/api.ts';
	import { apiPath } from '#lib/cluster-node.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';

	const SOURCES = [
		'',
		'titand',
		'kernel',
		'storage',
		'shares',
		'docker',
		'vms',
		'network',
	] as const;
	// Time ranges in seconds; '' is everything since the NAS started (the journal lives in RAM).
	const RANGES = { '': 0, '15m': 900, '1h': 3600, '24h': 86400 } as const;
	type Range = keyof typeof RANGES;
	const KEEP = 2000;

	const askedSource = untrack(() => page.url.searchParams.get('source') ?? '');
	let source = $state<(typeof SOURCES)[number]>(
		SOURCES.includes(askedSource as (typeof SOURCES)[number])
			? (askedSource as (typeof SOURCES)[number])
			: '',
	);
	const askedPriority = Number(untrack(() => page.url.searchParams.get('priority') ?? '6'));
	let priority = $state([3, 4, 6, 7].includes(askedPriority) ? askedPriority : 6);
	let search = $state(untrack(() => page.url.searchParams.get('search') ?? ''));
	const askedRange = untrack(() => page.url.searchParams.get('range') ?? '');
	let range = $state<Range>(askedRange in RANGES ? (askedRange as Range) : '');
	let follow = $state(true);
	let entries = $state<Entry[]>([]);
	let cursor = '';
	let error = $state('');
	let loading = $state(true);
	let list = $state<HTMLElement>();
	// Following only scrolls while the reader is at the end of the list.
	let atEnd = $state(true);
	let generation = 0;
	let fetchingMore = false;

	function query(after = ''): string {
		const params = new URLSearchParams({ source, priority: String(priority), lines: '500' });
		if (search.trim()) params.set('search', search.trim());
		if (RANGES[range]) params.set('since', String(Math.floor(Date.now() / 1000) - RANGES[range]));
		if (after) params.set('after', after);
		return '/logs?' + params;
	}

	async function load() {
		const request = ++generation;
		loading = true;
		error = '';
		try {
			const res = parseLogs(await api.get<unknown>(query()));
			if (request !== generation) return;
			entries = res.entries;
			cursor = res.cursor;
			atEnd = true;
			toEnd();
		} catch (err) {
			if (request !== generation) return;
			error = errorText(err instanceof ApiError ? err.code : 'logs_failed');
		}
		if (request === generation) loading = false;
	}

	async function more() {
		if (!cursor || document.hidden || loading || fetchingMore) return;
		const request = generation;
		fetchingMore = true;
		try {
			const res = parseLogs(await api.get<unknown>(query(cursor)));
			if (request !== generation) return;
			error = '';
			cursor = res.cursor;
			if (res.entries.length > 0) {
				entries = [...entries, ...res.entries].slice(-KEEP);
				toEnd();
			}
		} catch (err) {
			if (request === generation)
				error = errorText(err instanceof ApiError ? err.code : 'logs_failed');
		} finally {
			fetchingMore = false;
		}
	}

	function toEnd(force = false) {
		if (force) atEnd = true;
		if ((follow && atEnd) || force)
			requestAnimationFrame(() => list?.scrollTo({ top: list.scrollHeight }));
	}

	function scrolled() {
		if (!list) return;
		atEnd = list.scrollHeight - list.scrollTop - list.clientHeight < 24;
	}

	// A new choice loads anew; typing waits a moment.
	$effect(() => {
		source;
		priority;
		search;
		range;
		untrack(() => {
			generation++;
		});
		const timer = setTimeout(() => untrack(load), 250);
		return () => clearTimeout(timer);
	});

	$effect(() => {
		if (!follow) return;
		return poll(more, { interval: 3000, immediate: false });
	});

	const clock = $derived(
		new Intl.DateTimeFormat(i18n.locale, {
			day: '2-digit',
			month: '2-digit',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit',
		}),
	);
	/** The lines as text, oldest first. */
	function logText(from: number, to: number): string {
		return entries
			.slice(Math.max(0, from), to)
			.map(
				(e) =>
					`${e.time} ${e.source} ${e.priority <= 3 ? 'ERROR ' : e.priority === 4 ? 'WARNING ' : ''}${e.message}`,
			)
			.join('\n');
	}

	let copied = $state<'' | 'ok' | 'failed'>('');
	let copiedTimer: ReturnType<typeof setTimeout> | undefined;
	async function copyLines() {
		clearTimeout(copiedTimer);
		try {
			await navigator.clipboard.writeText(logText(0, entries.length) + '\n');
			copied = 'ok';
		} catch {
			copied = 'failed';
		}
		copiedTimer = setTimeout(() => (copied = ''), 4000);
	}
	/** Downloads up to 50,000 lines of this start for the chosen filter and
	 *  range, not only the lines on the page. */
	function exportLines() {
		const params = new URLSearchParams({ source, priority: String(priority) });
		if (RANGES[range]) params.set('since', String(Math.floor(Date.now() / 1000) - RANGES[range]));
		const link = document.createElement('a');
		link.href = apiPath('/logs/export?' + params);
		link.click();
	}

	let noPrevious = $state(false);
	/** The kept log of the start before this one, from the volume. */
	async function exportPrevious() {
		noPrevious = false;
		try {
			await api.download('/logs/export?boot=previous', 'titanos-log-previous-start.txt');
		} catch {
			noPrevious = true;
		}
	}

	// Messages known to be harmless: shown, but quiet, with a note why.
	const HARMLESS = [/portmapper: failed to get PCP mapping/, /PCP is implemented but not enabled/];
	const harmless = (entry: Entry) => HARMLESS.some((pattern) => pattern.test(entry.message));

	function level(priority: number): 'error' | 'warn' | '' {
		return priority <= 3 ? 'error' : priority === 4 ? 'warn' : '';
	}
</script>

<PageHead title={t('tools.logs')} parent={{ href: '/tools', label: t('nav.tools') }} />

<section class="tile logs" aria-labelledby="tools-logs">
	<h2 class="sr-only" id="tools-logs">{t('tools.logs')}</h2>
	<div class="filters">
		<label
			><span>{t('tools.source')}</span>
			<select class="field" bind:value={source}>
				{#each SOURCES as name (name)}<option value={name}
						>{t(`tools.source.${name || 'all'}`)}</option
					>{/each}
			</select>
		</label>
		<label
			><span>{t('tools.level')}</span>
			<select class="field" bind:value={priority}>
				<option value={3}>{t('tools.level.error')}</option>
				<option value={4}>{t('tools.level.warning')}</option>
				<option value={6}>{t('tools.level.info')}</option>
				<option value={7}>{t('tools.level.debug')}</option>
			</select>
		</label>
		<label
			><span>{t('tools.range')}</span>
			<select class="field" bind:value={range}>
				<option value="">{t('tools.range.boot')}</option>
				<option value="24h">{t('tools.range.24h')}</option>
				<option value="1h">{t('tools.range.1h')}</option>
				<option value="15m">{t('tools.range.15m')}</option>
			</select>
		</label>
		<label class="grow"
			><span>{t('tools.search')}</span>
			<input class="field" type="search" bind:value={search} maxlength="200" spellcheck="false" />
		</label>
		<label class="check"
			><input type="checkbox" bind:checked={follow} /><span>{t('tools.follow')}</span></label
		>
	</div>
	<div class="actions">
		<button type="button" class="btn" disabled={entries.length === 0} onclick={copyLines}
			>{t('tools.copyLines')}</button
		>
		<button type="button" class="btn" disabled={entries.length === 0} onclick={exportLines}
			>{t('tools.exportLines')}</button
		>
		<button type="button" class="btn" onclick={exportPrevious}>{t('tools.exportPrevious')}</button>
		<span class="muted count" role="status"
			>{#if copied === 'ok'}{t('tools.copied')}{:else if copied === 'failed'}{t(
					'tools.copyFailed',
				)}{:else if entries.length}{t('tools.lineCount', { count: entries.length })}{/if}</span
		>
	</div>
	{#if error}<p class="notice-error" role="alert">
			{error}
			<button type="button" class="btn small" onclick={load} disabled={loading}
				>{t('ui.retry')}</button
			>
		</p>{/if}
	{#if noPrevious}<p class="notice-warn" role="status">{t('tools.noPrevious')}</p>{/if}
	<div class="frame">
		<!-- A region that scrolls must be reachable with the keyboard. -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			class="lines mono"
			bind:this={list}
			onscroll={scrolled}
			role="log"
			aria-label={t('tools.logs')}
			tabindex="0"
		>
			{#each entries as entry, index (index)}
				<div
					class="line {level(entry.priority)}"
					class:harmless={harmless(entry)}
					title={harmless(entry) ? t('tools.harmless') : undefined}
				>
					<time datetime={entry.time}>{clock.format(new Date(entry.time))}</time>
					<span class="source">{entry.source}</span>
					<span class="message"
						>{#if level(entry.priority)}<span class="tag"
								>{t(
									entry.priority <= 3 ? 'notify.severity.error' : 'notify.severity.warning',
								)}</span
							>
						{/if}{entry.message}</span
					>
				</div>
			{:else}
				<EmptyState inline title={loading ? t('tools.loading') : t('tools.noLines')} />
			{/each}
		</div>
		{#if !atEnd && entries.length > 0}
			<button type="button" class="btn small to-end" onclick={() => toEnd(true)}
				>{t('tools.toEnd')} ↓</button
			>
		{/if}
	</div>
	<p class="muted note">{t('tools.logsNote')}</p>
</section>

<style>
	section {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: 0.875rem;
	}
	section p {
		margin: 0;
	}
	.filters,
	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 12px 16px;
	}
	.actions {
		align-items: center;
		gap: 8px 10px;
	}
	.count {
		font-size: 0.8125rem;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	label select {
		min-width: 11rem;
	}
	.grow {
		flex: 1 1 220px;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 8px;
		height: 2.75rem;
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.frame {
		position: relative;
	}
	.to-end {
		position: absolute;
		right: 16px;
		bottom: 12px;
	}
	.line.harmless {
		opacity: 0.55;
	}
	.lines {
		height: min(60vh, 640px);
		overflow: auto;
		padding: 10px 12px;
		background: var(--track);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		font-size: 0.75rem;
		line-height: 1.55;
		scrollbar-width: thin;
		scrollbar-color: var(--muted) transparent;
	}
	.line {
		display: grid;
		grid-template-columns: 9.5rem 9rem 1fr auto;
		gap: 0 12px;
	}
	.line time,
	.source {
		color: var(--muted);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.message {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.line.error .message {
		color: var(--error-text);
	}
	.line.warn .message {
		color: var(--warn-text);
	}
	.tag {
		font-weight: 600;
	}
	.note {
		font-size: 0.75rem;
	}
	/* Narrow screens: time and source above the message instead of a wide row. */
	@media (max-width: 760px) {
		.line {
			grid-template-columns: auto 1fr auto;
			padding: 4px 0;
			border-bottom: 1px solid var(--border);
		}
		.message {
			grid-column: 1 / -1;
			grid-row: 2;
		}
	}
</style>
