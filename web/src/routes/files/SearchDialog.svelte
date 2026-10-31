<script lang="ts">
	import { api, type FileRoot, type FileEntry } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { MB, formatBytes, formatDateTime } from '#lib/format.ts';
	let {
		root,
		dir,
		showHidden,
		onclose,
		onopen,
	}: {
		root: FileRoot;
		dir: string;
		showHidden: boolean;
		onclose: () => void;
		onopen: (path: string) => void;
	} = $props();
	let content = $state(false),
		query = $state(''),
		extension = $state(''),
		minimum = $state<number | undefined>(),
		maximum = $state<number | undefined>(),
		after = $state(''),
		before = $state('');
	type SearchResult = {
		entries: (FileEntry & { path: string })[];
		scanned: number;
		truncated: boolean;
	};
	let result = $state<SearchResult | null>(null),
		busy = $state(false),
		error = $state('');
	let controller: AbortController | undefined;
	$effect(() => () => controller?.abort());
	async function search(event: SubmitEvent) {
		event.preventDefault();
		controller?.abort();
		const current = new AbortController();
		controller = current;
		busy = true;
		error = '';
		result = null;
		try {
			result = await api.post<SearchResult>(
				'/files/search',
				{
					root: root.id,
					path: dir,
					query,
					...(content ? { content: true } : {}),
					extension,
					minSize: Math.round((minimum ?? 0) * MB),
					maxSize: Math.round((maximum ?? 0) * MB),
					...(after ? { after: new Date(after).toISOString() } : {}),
					...(before ? { before: new Date(before).toISOString() } : {}),
					showHidden,
				},
				current.signal,
			);
		} catch (err) {
			if (!current.signal.aborted) error = apiErrorText(err);
		} finally {
			if (controller === current) busy = false;
		}
	}
</script>

<Dialog title={t('files.deepSearch')} {onclose} wide>
	<p class="mono">{root.name}/{dir}</p>
	<form onsubmit={search}>
		<fieldset disabled={busy}>
			<label class="wide"
				><span
					><input type="checkbox" bind:checked={content} disabled={!root.indexed} />
					{t('files.searchContent')}</span
				><small class="muted"
					>{root.indexed ? t('files.searchContentHint') : t('files.searchContentOff')}</small
				></label
			>
			<label
				>{content ? t('files.searchWords') : t('files.searchName')}<input
					class="field"
					bind:value={query}
					required={content}
				/></label
			>
			<label
				>{t('files.searchExtension')}<input
					class="field"
					bind:value={extension}
					placeholder="jpg"
				/></label
			>
			<label
				>{t('files.searchMin')}<input
					class="field"
					type="number"
					min="0"
					step="any"
					bind:value={minimum}
				/></label
			>
			<label
				>{t('files.searchMax')}<input
					class="field"
					type="number"
					min="0"
					step="any"
					bind:value={maximum}
				/></label
			>
			<label
				>{t('files.searchAfter')}<input
					class="field"
					type="datetime-local"
					bind:value={after}
				/></label
			>
			<label
				>{t('files.searchBefore')}<input
					class="field"
					type="datetime-local"
					bind:value={before}
				/></label
			>
		</fieldset>
		<button class="btn btn-primary" disabled={busy}>{t('files.searchStart')}</button>
	</form>
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
			{content
				? t('files.searchContentCount', { count: result.entries.length })
				: t('files.searchCount', { count: result.entries.length, scanned: result.scanned })}
		</p>
		{#if result.truncated}<p class="notice-warn">{t('files.searchLimited')}</p>{/if}
		<div class="results">
			{#each result.entries as entry (entry.path)}<div class="result">
					<span class="mono">{root.name}/{entry.path}</span><span class="muted"
						>{formatBytes(entry.size)} · {formatDateTime(new Date(entry.modified))}</span
					><button class="btn" onclick={() => onopen(entry.path.split('/').slice(0, -1).join('/'))}
						>{t('files.openFolder')}</button
					>
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
		margin: 0 0 12px;
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.wide {
		grid-column: 1/-1;
	}
	.results {
		max-height: 45vh;
		overflow: auto;
	}
	.result {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
		padding: 12px 0;
		border-top: 1px solid var(--border);
	}
	.result .mono {
		width: 100%;
		overflow-wrap: anywhere;
	}
	.result .btn {
		margin-left: auto;
	}
	@media (max-width: 600px) {
		fieldset {
			grid-template-columns: 1fr;
		}
	}
</style>
