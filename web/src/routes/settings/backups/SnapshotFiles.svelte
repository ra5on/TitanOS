<script lang="ts">
	// Browses one saved state folder by folder and picks files and folders
	// to restore. Paths are those inside the backup (/mnt/<volume>/<share>/…).
	import { api, ApiError } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Entry = { name: string; path: string; type: string; size: number };
	let { job, snapshot, start, selected = $bindable([]) }: { job: string; snapshot: string; start: string; selected: string[] } = $props();

	let path = $state('');
	let entries = $state<Entry[]>([]);
	let total = $state(0);
	let next = $state<number | null>(null);
	let cut = $state(false);
	let loading = $state(false);
	let error = $state('');

	async function open(folder: string, offset = 0) {
		loading = true;
		error = '';
		try {
			const res = await api.get<{ entries: Entry[]; total: number; cut: boolean; nextOffset?: number }>(`/backups/${encodeURIComponent(job)}/snapshots/${encodeURIComponent(snapshot)}/files?path=${encodeURIComponent(folder)}&offset=${offset}`);
			path = folder;
			entries = offset ? [...entries, ...res.entries] : res.entries;
			total = res.total;
			cut = res.cut;
			next = res.nextOffset ?? null;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		loading = false;
	}

	$effect(() => {
		snapshot;
		selected = [];
		open(start || '/');
	});

	const parent = $derived(path === '/' ? '' : path.slice(0, path.lastIndexOf('/')) || '/');
	function toggle(p: string) {
		selected = selected.includes(p) ? selected.filter((v) => v !== p) : [...selected, p];
	}
</script>

<div class="picker">
	<div class="where">
		<button type="button" class="btn" disabled={!parent || loading} onclick={() => open(parent)} aria-label={t('backup.filesUp')}>↑</button>
		<span class="mono">{path}</span>
	</div>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<ul>
		{#each entries as entry (entry.path)}
			<li>
				<label class="check"><input type="checkbox" checked={selected.includes(entry.path)} onchange={() => toggle(entry.path)} /></label>
				{#if entry.type === 'dir'}
					<button type="button" class="link" onclick={() => open(entry.path)}>{entry.name}/</button>
				{:else}
					<span>{entry.name}</span>
				{/if}
				<span class="muted mono size">{entry.type === 'dir' ? '' : formatBytes(entry.size)}</span>
			</li>
		{:else}
			{#if !loading}<li class="muted">{t('backup.filesEmpty')}</li>{/if}
		{/each}
	</ul>
	{#if next !== null}<button type="button" class="btn" disabled={loading} onclick={() => open(path, next ?? 0)}>{t('backup.filesMore', { shown: entries.length, total })}</button>{/if}
	{#if cut}<p class="notice-warn">{t('backup.filesCut')}</p>{/if}
	<p class="muted small">{t('backup.filesSelected', { count: selected.length })}</p>
</div>

<style>
	.picker {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.where {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
	}
	.where .mono {
		overflow-wrap: anywhere;
		font-size: 0.8125rem;
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		max-height: 18rem;
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	li {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 6px 10px;
		border-bottom: 1px solid var(--border);
		font-size: 0.875rem;
	}
	li:last-child {
		border-bottom: 0;
	}
	.size {
		margin-left: auto;
		font-size: 0.75rem;
	}
	.check input {
		width: 16px;
		height: 16px;
		margin: 0;
	}
	.link {
		padding: 0;
		background: none;
		border: 0;
		color: var(--accent);
		font: inherit;
		cursor: pointer;
		text-align: left;
		overflow-wrap: anywhere;
	}
	.small {
		margin: 0;
		font-size: 0.8125rem;
	}
</style>
