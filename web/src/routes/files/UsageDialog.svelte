<script lang="ts">
	import { api, type FileRoot } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
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
		onopen: (p: string) => void;
	} = $props();
	type Item = { path: string; bytes: number; files: number; dir: boolean };
	let data = $state<{
			bytes: number;
			files: number;
			folders: number;
			largest: Item[];
			children: Item[];
		} | null>(null),
		busy = $state(false),
		error = $state('');
	let controller: AbortController | undefined;
	$effect(() => () => controller?.abort());
	async function scan() {
		controller?.abort();
		const c = new AbortController();
		controller = c;
		busy = true;
		error = '';
		data = null;
		try {
			const result = await api.post<typeof data>(
				'/files/usage',
				{ root: root.id, path: dir, showHidden },
				c.signal,
			);
			if (!c.signal.aborted) data = result;
		} catch (e) {
			if (!c.signal.aborted) error = apiErrorText(e);
		} finally {
			if (controller === c) busy = false;
		}
	}
</script>

<Dialog title={t('files.usageTitle')} {onclose} wide>
	<p class="mono">{root.name}/{dir}</p>
	<p class="muted">{t('files.usageHint')}</p>
	<button class="btn btn-primary" disabled={busy} onclick={scan}>{t('files.usageStart')}</button>
	{#if busy}<p role="status">{t('files.searchRunning')}</p>
		<button
			class="btn"
			onclick={() => {
				controller?.abort();
				busy = false;
			}}>{t('common.cancel')}</button
		>{/if}
	{#if error}<p role="alert" class="notice-error">{error}</p>{/if}
	{#if data}<p role="status">
			{formatBytes(data.bytes)} · {t('files.downloadCount', { count: data.files })} · {t(
				'files.usageFolders',
				{ count: data.folders },
			)}
		</p>
		<div class="results">
			{#each [{ title: t('files.usageChildren'), items: data.children }, { title: t('files.usageLargest'), items: data.largest }] as group}
				<h3>{group.title}</h3>
				{#each group.items as item (item.path)}<div class="row">
						<span class="mono">{item.path}{item.dir ? '/' : ''}</span><strong
							>{formatBytes(item.bytes)}</strong
						><button
							class="btn"
							onclick={() =>
								onopen(item.dir ? item.path : item.path.split('/').slice(0, -1).join('/'))}
							>{t('files.openFolder')}</button
						>
					</div>{/each}{/each}
		</div>{/if}
	<div class="dialog-actions">
		<button class="btn" onclick={onclose}>{t('files.editClose')}</button>
	</div>
</Dialog>

<style>
	.results {
		max-height: 50vh;
		overflow: auto;
	}
	.row {
		display: flex;
		gap: 12px;
		align-items: center;
		flex-wrap: wrap;
		padding: 10px 0;
		border-top: 1px solid var(--border);
	}
	.mono {
		overflow-wrap: anywhere;
	}
	.row .mono {
		flex: 1;
		min-width: 140px;
	}
</style>
