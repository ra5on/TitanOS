<script lang="ts">
	import { untrack } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import DiskTests from './DiskTests.svelte';
	import { askAbout, assistant } from '#lib/assistant.svelte.ts';

	type Entry = { time: string; priority: number; source: string; message: string };
	type Logs = { entries: Entry[]; cursor: string };

	const SOURCES = ['', 'titand', 'kernel', 'storage', 'shares', 'docker', 'vms', 'network'] as const;
	const KEEP = 2000;

	let source = $state<(typeof SOURCES)[number]>('');
	let priority = $state(6);
	let search = $state('');
	let follow = $state(true);
	let entries = $state<Entry[]>([]);
	let cursor = '';
	let error = $state('');
	let loading = $state(true);
	let list = $state<HTMLElement>();

	function query(after = ''): string {
		const params = new URLSearchParams({ source, priority: String(priority), lines: '500' });
		if (search.trim()) params.set('search', search.trim());
		if (after) params.set('after', after);
		return '/logs?' + params;
	}

	async function load() {
		loading = true;
		error = '';
		try {
			const res = await api.get<Logs>(query());
			entries = res.entries;
			cursor = res.cursor;
			toEnd();
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		loading = false;
	}

	async function more() {
		if (!cursor || document.hidden) return;
		try {
			const res = await api.get<Logs>(query(cursor));
			cursor = res.cursor;
			if (res.entries.length > 0) {
				entries = [...entries, ...res.entries].slice(-KEEP);
				toEnd();
			}
		} catch {
			// The next round tries again.
		}
	}

	function toEnd() {
		if (follow) requestAnimationFrame(() => list?.scrollTo({ top: list.scrollHeight }));
	}

	// A new choice loads anew; typing waits a moment.
	$effect(() => {
		source;
		priority;
		search;
		const timer = setTimeout(() => untrack(load), 250);
		return () => clearTimeout(timer);
	});

	$effect(() => {
		if (!follow) return;
		const timer = setInterval(more, 3000);
		return () => clearInterval(timer);
	});

	const clock = $derived(new Intl.DateTimeFormat(i18n.locale, { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }));
	/** The lines as text for the assistant, oldest first. */
	function logText(from: number, to: number): string {
		return entries
			.slice(Math.max(0, from), to)
			.map((e) => `${e.time} ${e.source} ${e.priority <= 3 ? 'ERROR ' : e.priority === 4 ? 'WARNING ' : ''}${e.message}`)
			.join('\n');
	}
	function askLog() {
		askAbout(t('assistant.askLog'), [{ name: 'log.txt', kind: 'text', text: logText(entries.length - 400, entries.length) }]);
	}
	function askLine(index: number) {
		askAbout(t('assistant.askLogLine', { line: entries[index].message.slice(0, 300) }), [{ name: 'log.txt', kind: 'text', text: logText(index - 30, index + 10) }]);
	}

	function level(priority: number): 'error' | 'warn' | '' {
		return priority <= 3 ? 'error' : priority === 4 ? 'warn' : '';
	}

	let packing = $state(false);
	let packError = $state('');
	// The bundle is made while it is being downloaded; the browser saves it.
	async function pack() {
		packing = true;
		packError = '';
		try {
			const res = await fetch('/api/v1/diagnostics/bundle', { credentials: 'same-origin' });
			if (!res.ok) throw new Error(String(res.status));
			const blob = await res.blob();
			const name = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') ?? '')?.[1] ?? 'titanos-diagnose.zip';
			const link = document.createElement('a');
			link.href = URL.createObjectURL(blob);
			link.download = name;
			link.click();
			setTimeout(() => URL.revokeObjectURL(link.href), 10_000);
		} catch {
			packError = t('tools.bundleFailed');
		}
		packing = false;
	}
</script>

<PageHead title={t('nav.tools')} />

<nav class="operation-links">{#each [['tasks','/tools/tasks'],['maintenance','/tools/maintenance'],['monitoring','/tools/monitoring'],['import','/storage/import']] as [key,href](key)}<a class="tile" {href}>{t(`ops.${key}` as 'ops.tasks')} →</a>{/each}</nav>
<DiskTests />

<section class="tile" aria-labelledby="tools-bundle">
	<h2 class="tile-title" id="tools-bundle">{t('tools.bundle')}</h2>
	<p class="muted">{t('tools.bundleText')}</p>
	<div class="lists">
		<div><p class="name">{t('tools.bundleIn')}</p><p class="muted">{t('tools.bundleInText')}</p></div>
		<div><p class="name">{t('tools.bundleOut')}</p><p class="muted">{t('tools.bundleOutText')}</p></div>
	</div>
	<p class="notice-warn">{t('tools.bundleCheck')}</p>
	{#if packError}<p class="notice-error" role="alert">{packError}</p>{/if}
	<div><button type="button" class="btn btn-primary" disabled={packing} onclick={pack}>{t(packing ? 'tools.bundlePacking' : 'tools.bundleMake')}</button></div>
</section>

<section class="tile logs" aria-labelledby="tools-logs">
	<h2 class="tile-title" id="tools-logs">{t('tools.logs')}</h2>
	<div class="filters">
		<label><span>{t('tools.source')}</span>
			<select class="field" bind:value={source}>
				{#each SOURCES as name (name)}<option value={name}>{t(`tools.source.${name || 'all'}`)}</option>{/each}
			</select>
		</label>
		<label><span>{t('tools.level')}</span>
			<select class="field" bind:value={priority}>
				<option value={3}>{t('tools.level.error')}</option>
				<option value={4}>{t('tools.level.warning')}</option>
				<option value={6}>{t('tools.level.info')}</option>
				<option value={7}>{t('tools.level.debug')}</option>
			</select>
		</label>
		<label class="grow"><span>{t('tools.search')}</span>
			<input class="field" type="search" bind:value={search} maxlength="200" spellcheck="false" />
		</label>
		<label class="check"><input type="checkbox" bind:checked={follow} /><span>{t('tools.follow')}</span></label>
		{#if assistant.status?.available && entries.length > 0}
			<button type="button" class="btn" onclick={askLog}>{t('assistant.askLogButton')}</button>
		{/if}
	</div>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<!-- A region that scrolls must be reachable with the keyboard. -->
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<div class="lines mono" bind:this={list} role="log" aria-label={t('tools.logs')} tabindex="0">
		{#each entries as entry, index (index)}
			<div class="line {level(entry.priority)}">
				<time datetime={entry.time}>{clock.format(new Date(entry.time))}</time>
				<span class="source">{entry.source}</span>
				<span class="message">{#if level(entry.priority)}<span class="tag">{t(entry.priority <= 3 ? 'notify.severity.error' : 'notify.severity.warning')}</span> {/if}{entry.message}</span>
				{#if level(entry.priority) && assistant.status?.available}<button type="button" class="ask" title={t('assistant.ask')} aria-label={t('assistant.askLine')} onclick={() => askLine(index)}>?</button>{/if}
			</div>
		{:else}
			<EmptyState inline title={loading ? t('tools.loading') : t('tools.noLines')} />
		{/each}
	</div>
	<p class="muted note">{t('tools.logsNote')}</p>
</section>

<style>
 .operation-links{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:var(--gap);margin-bottom:var(--gap)}.operation-links a{padding:18px;color:var(--text);text-decoration:none;font-weight:600}

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
	.lists {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
		gap: 12px 24px;
	}
	.name {
		font-weight: 600;
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 12px 16px;
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
		min-width: 720px;
	}
	.ask {
		width: 20px;
		height: 20px;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: 50%;
		background: transparent;
		color: var(--muted);
		font-size: 0.6875rem;
		cursor: pointer;
	}
	.ask:hover {
		color: var(--text);
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
	.empty {
		font-family: var(--font-sans, inherit);
		padding: 12px 0;
	}
	.note {
		font-size: 0.75rem;
	}
</style>
