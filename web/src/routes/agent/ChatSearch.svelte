<script lang="ts">
	import { onMount } from 'svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import { t, apiErrorText } from '#lib/i18n/index.svelte.ts';
	import type { Project } from '#lib/agent.ts';
	type Hit = {
		chat: string;
		entry: string;
		author?: string;
		title: string;
		text: string;
		archived: boolean;
	};
	type Results = { hits: Hit[]; after: string; offset: number };
	let {
		initial,
		projects,
		onopen,
		onclose,
	}: { initial: string; projects: Project[]; onopen: (hit: Hit) => void; onclose: () => void } =
		$props();
	let query = $state(''),
		project = $state(''),
		hits = $state<Hit[]>([]),
		after = $state(''),
		offset = $state(0),
		searched = $state(false),
		busy = $state(false),
		error = $state('');
	let request = 0;
	function reset() {
		request++;
		hits = [];
		after = '';
		offset = 0;
		searched = false;
		busy = false;
		error = '';
	}
	async function search(more = false) {
		if (query.trim().length < 2 || busy) return;
		const serial = ++request;
		if (!more) {
			hits = [];
			after = '';
			offset = 0;
		}
		const params = new URLSearchParams({ q: query.trim(), project, after, offset: String(offset) });
		busy = true;
		error = '';
		try {
			const res = await api.get<Results>('/agent/text-search?' + params);
			if (serial !== request) return;
			hits = [...hits, ...res.hits];
			after = res.after;
			offset = res.offset;
			searched = true;
		} catch (e) {
			if (serial === request) error = apiErrorText(e);
		} finally {
			if (serial === request) busy = false;
		}
	}
	onMount(() => {
		query = initial;
		if (initial.trim().length >= 2) void search();
		return () => {
			request++;
		};
	});
</script>

<Dialog title={t('agent.library.search')} {onclose}>
	<form
		onsubmit={(e) => {
			e.preventDefault();
			void search();
		}}
	>
		<label
			>{t('tools.search')}<input
				class="field"
				type="search"
				bind:value={query}
				oninput={reset}
				maxlength="120"
			/></label
		>
		<label
			>{t('agent.project')}<select class="field" bind:value={project} onchange={reset}
				><option value="">{t('agent.library.all')}</option>{#each projects as p (p.id)}<option
						value={p.id}>{p.name}</option
					>{/each}</select
			></label
		>
		<button class="btn" disabled={busy || query.trim().length < 2}>{t('tools.search')}</button>
	</form>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if busy}<p role="status">
			{t('common.loading')}
		</p>{:else if searched && !hits.length && !after}<p role="status">
			{t('agent.library.noHits')}
		</p>{/if}
	<ul>
		{#each hits as h (h.chat + ':' + (h.author ?? '') + ':' + h.entry)}<li>
				<button class="result" type="button" onclick={() => onopen(h)}
					><strong>{h.title}</strong>{#if h.archived}<small>{t('agent.archive.title')}</small
						>{/if}<span>{h.text}</span></button
				>
			</li>{/each}
	</ul>
	{#if after}<button class="btn" disabled={busy} onclick={() => search(true)}
			>{t('agent.library.more')}</button
		>{/if}
</Dialog>

<style>
	form,
	label {
		display: grid;
		gap: 0.45rem;
	}
	form {
		gap: 0.7rem;
	}
	ul {
		padding: 0;
		list-style: none;
	}
	.result {
		display: grid;
		gap: 0.25rem;
		text-align: start;
		width: 100%;
		padding: 0.7rem 0.25rem;
		overflow-wrap: anywhere;
		color: inherit;
		background: transparent;
		border: 0;
		border-bottom: 1px solid var(--line);
		cursor: pointer;
	}
	.result:hover,
	.result:focus-visible {
		background: var(--hover);
	}
	small {
		color: var(--muted);
	}
</style>
