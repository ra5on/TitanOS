<script lang="ts">
	import { api, type FileRoot, type FileEntry } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { MB, formatBytes, formatDateTime } from '#lib/format.ts';
	import { onMount, tick, untrack } from 'svelte';
	import type { SavedSearch, SearchPosition, SearchFilters } from '#lib/file-search.ts';
	import type { FilePreferenceStore } from './preferences.svelte.ts';
	import { highlight } from './excerpt.ts';
	let {
		root,
		dir,
		showHidden,
		roots,
		prefs,
		initial,
		onclose,
		onopen,
		onpreview,
	}: {
		root: FileRoot;
		dir: string;
		showHidden: boolean;
		onclose: () => void;
		roots: FileRoot[];
		prefs: FilePreferenceStore;
		initial?: SearchPosition;
		onopen: (position: SearchPosition, hitPath: string, name?: string) => void;
		onpreview: (root: string, entry: FileEntry & { path: string }) => void;
	} = $props();
	const defaults = untrack(
		() =>
			initial ?? {
				root: root.id,
				dir,
				query: '',
				extension: '',
				content: !!root.indexed,
				after: '',
				before: '',
			},
	);
	let filters = $state<SearchFilters>({ ...defaults });
	const scope = $derived(roots.find((item) => item.id === filters.root));
	let saveName = $state('');
	let saved = $state('');
	let searched = $state<SearchFilters>();
	function choose(value: string) {
		saved = value;
		const choice = prefs.searches.find((item) => item.name === saved);
		if (!choice) return;
		filters = { ...choice };
		saveName = '';
		void search();
	}
	function saveSearch() {
		const name = saveName.trim();
		if (!name || prefs.searches.some((s) => s.name === name) || prefs.searches.length >= 40) return;
		const next: SavedSearch = { ...filters, name };
		prefs.searches = [...prefs.searches, next];
		prefs.save();
		saved = name;
		saveName = '';
	}
	function forget() {
		prefs.searches = prefs.searches.filter((s) => s.name !== saved);
		prefs.save();
		saved = '';
	}
	type SearchResult = {
		entries: (FileEntry & { path: string; excerpt?: string })[];
		scanned: number;
		truncated: boolean;
	};
	let asked = $state(''),
		result = $state<SearchResult | null>(null),
		busy = $state(false),
		error = $state('');
	let resultsBox = $state<HTMLElement>();
	let controller: AbortController | undefined;
	$effect(() => () => controller?.abort());
	onMount(() => {
		if (initial) void search(initial.scroll);
	});
	async function search(scroll = 0) {
		controller?.abort();
		const current = new AbortController();
		controller = current;
		busy = true;
		error = '';
		result = null;
		const request = { ...filters, content: !!scope?.indexed && filters.content };
		searched = request;
		asked = request.query;
		if (!scope) {
			error = t('files.searchScopeMissing');
			busy = false;
			return;
		}
		try {
			const response = await api.post<SearchResult>(
				'/files/search',
				{
					root: request.root,
					path: request.dir,
					query: request.query,
					...(request.content ? { content: true } : {}),
					extension: request.extension,
					minSize: Math.round((request.minimum ?? 0) * MB),
					maxSize: Math.round((request.maximum ?? 0) * MB),
					...(request.after ? { after: new Date(request.after).toISOString() } : {}),
					...(request.before ? { before: new Date(request.before).toISOString() } : {}),
					showHidden,
				},
				current.signal,
			);
			if (current.signal.aborted) return;
			result = response;
			await tick();
			if (resultsBox && !current.signal.aborted) {
				resultsBox.scrollIntoView({ block: 'nearest' });
				resultsBox.scrollTop = scroll;
			}
		} catch (err) {
			if (!current.signal.aborted) error = apiErrorText(err);
		} finally {
			if (controller === current) busy = false;
		}
	}
</script>

<Dialog title={t('files.deepSearch')} {onclose} wide>
	{#if prefs.searches.length}
		<label
			>{t('files.savedSearches')}
			<select class="field" value={saved} onchange={(e) => choose(e.currentTarget.value)}>
				<option value="">—</option>
				{#each prefs.searches as item (item.name)}<option value={item.name}>{item.name}</option
					>{/each}
			</select>
		</label>
		{#if saved}<button class="btn" onclick={forget}>{t('files.forgetSearch')}</button>{/if}
	{/if}
	<p class="mono">{scope?.name ?? filters.root}/{filters.dir}</p>
	<form
		onsubmit={(e) => {
			e.preventDefault();
			void search();
		}}
	>
		<fieldset disabled={busy}>
			<label class="wide"
				><span
					><input type="checkbox" bind:checked={filters.content} disabled={!scope?.indexed} />
					{t('files.searchContent')}</span
				><small class="muted"
					>{scope?.indexed ? t('files.searchContentHint') : t('files.searchContentOff')}</small
				></label
			>
			<label
				>{filters.content && scope?.indexed ? t('files.searchWords') : t('files.searchName')}<input
					class="field"
					bind:value={filters.query}
					required={filters.content && !!scope?.indexed}
				/></label
			>
			<label
				>{t('files.searchExtension')}<input
					class="field"
					bind:value={filters.extension}
					placeholder="jpg"
				/></label
			>
			<label
				>{t('files.searchMin')}<input
					class="field"
					type="number"
					min="0"
					step="any"
					bind:value={filters.minimum}
				/></label
			>
			<label
				>{t('files.searchMax')}<input
					class="field"
					type="number"
					min="0"
					step="any"
					bind:value={filters.maximum}
				/></label
			>
			<label
				>{t('files.searchAfter')}<input
					class="field"
					type="datetime-local"
					bind:value={filters.after}
				/></label
			>
			<label
				>{t('files.searchBefore')}<input
					class="field"
					type="datetime-local"
					bind:value={filters.before}
				/></label
			>
		</fieldset>
		<button class="btn btn-primary" disabled={busy}>{t('files.searchStart')}</button>
	</form>
	{#if prefs.ready}<details>
			<summary>{t('files.saveSearch')}</summary>
			<form
				onsubmit={(e) => {
					e.preventDefault();
					saveSearch();
				}}
			>
				<label
					>{t('files.searchLabel')}<input
						class="field"
						bind:value={saveName}
						maxlength="80"
						required
					/></label
				>
				<button
					class="btn"
					disabled={!saveName.trim() ||
						prefs.searches.some((s) => s.name === saveName.trim()) ||
						prefs.searches.length >= 40}>{t('common.save')}</button
				>
			</form>
		</details>{/if}
	{#if prefs.error}<p class="notice-error" role="alert">{prefs.error}</p>{/if}
	{#if busy}<p role="status">{t('files.searchRunning')}</p>
		<button
			class="btn"
			onclick={() => {
				controller?.abort();
				busy = false;
			}}>{t('common.cancel')}</button
		>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if result}<p role="status">
			{searched?.content
				? t('files.searchContentCount', { count: result.entries.length })
				: t('files.searchCount', { count: result.entries.length, scanned: result.scanned })}
		</p>
		{#if result.truncated}<p class="notice-warn">{t('files.searchLimited')}</p>{/if}
		<div class="results" bind:this={resultsBox}>
			{#each result.entries as entry (entry.path)}<div class="result">
					<span class="what"
						><strong>{entry.name}</strong>
						<small class="muted mono"
							>{scope?.name}/{entry.path.split('/').slice(0, -1).join('/')}</small
						></span
					>
					{#if entry.excerpt}<p class="excerpt">
							{#each highlight(entry.excerpt, asked) as part, i (i)}{#if part.hit}<mark
										>{part.text}</mark
									>{:else}{part.text}{/if}{/each}
						</p>{/if}
					<span class="muted"
						>{formatBytes(entry.size)} · {formatDateTime(new Date(entry.modified))}</span
					>{#if !entry.dir}<button class="btn" onclick={() => onpreview(filters.root, entry)}
							>{t('files.preview')}</button
						>{/if}
					<button
						class="btn"
						onclick={() =>
							searched && onopen({ ...searched, scroll: resultsBox?.scrollTop ?? 0 }, entry.path)}
						>{t('files.openFolder')}</button
					>
					<button
						class="btn"
						onclick={() =>
							searched &&
							onopen({ ...searched, scroll: resultsBox?.scrollTop ?? 0 }, entry.path, entry.name)}
					>
						{t('files.showHit')}
					</button>
				</div>{/each}
		</div>{/if}
	<div class="dialog-actions">
		<button class="btn" onclick={onclose}>{t('files.editClose')}</button>
	</div>
</Dialog>

<style>
	fieldset {
		border: 0;
		padding: 0;
		margin: 0 0 var(--space-3);
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: var(--space-3);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.wide {
		grid-column: 1/-1;
	}
	.results {
		/* In the dialog's column it would otherwise shrink to nothing. */
		flex-shrink: 0;
		max-height: 45vh;
		overflow: auto;
	}
	.result {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2-5);
		padding: var(--space-3) 0;
		border-top: 1px solid var(--border);
	}
	.what {
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: 100%;
		overflow-wrap: anywhere;
	}
	.excerpt {
		width: 100%;
		margin: 0;
		color: var(--muted);
		font-size: var(--text-base);
		overflow-wrap: anywhere;
	}
	mark {
		background: color-mix(in srgb, var(--accent) 35%, transparent);
		color: var(--text);
		border-radius: 3px;
		padding: 0 2px;
	}
	@media (max-width: 600px) {
		fieldset {
			grid-template-columns: 1fr;
		}
	}
</style>
