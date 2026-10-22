<script lang="ts">
	// Picks files from the shares (and, for administrators, volumes and USB
	// drives) of this account, for the assistant to read. Only names travel:
	// the NAS reads the files itself with the rights of the account.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import type { ChatAttachment } from '#lib/assistant.svelte.ts';
	import Dialog from './Dialog.svelte';
	import Icon from './Icon.svelte';

	type Props = { max: number; onpick: (files: ChatAttachment[]) => void; onclose: () => void };
	let { max, onpick, onclose }: Props = $props();

	type Root = { id: string; name: string; kind: string };
	type Entry = { name: string; dir: boolean; size: number };
	let roots = $state<Root[]>([]);
	let root = $state<Root | null>(null);
	let dir = $state('');
	let entries = $state<Entry[] | null>(null);
	let chosen = $state<ChatAttachment[]>([]);
	let error = $state('');

	const readable = /\.(txt|md|markdown|csv|tsv|json|ya?ml|conf|cfg|ini|toml|xml|log|sh|py|js|ts|html|css|sql|docx|xlsx|png|jpe?g|webp|gif)$/i;

	onMount(async () => {
		try {
			roots = (await api.get<Root[]>('/files/roots')).filter((r) => r.kind !== 'stick');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	});

	async function open(next: Root, path: string) {
		error = '';
		try {
			const list = await api.get<Entry[]>(`/files/list?${new URLSearchParams({ root: next.id, path })}`);
			root = next;
			dir = path;
			entries = list.filter((e) => !e.name.startsWith('.')).sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name));
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	function up() {
		if (!root) return;
		if (dir === '') {
			root = null;
			entries = null;
			return;
		}
		open(root, dir.includes('/') ? dir.slice(0, dir.lastIndexOf('/')) : '');
	}

	const key = (name: string) => `${root?.id}/${dir ? dir + '/' : ''}${name}`;
	function toggle(entry: Entry) {
		if (!root) return;
		const path = dir ? `${dir}/${entry.name}` : entry.name;
		const i = chosen.findIndex((c) => `${c.root}/${c.path}` === key(entry.name));
		if (i >= 0) chosen.splice(i, 1);
		else if (chosen.length < max) chosen.push({ name: entry.name, kind: 'nas', root: root.id, path });
	}
	const isChosen = (entry: Entry) => chosen.some((c) => `${c.root}/${c.path}` === key(entry.name));
</script>

<Dialog title={t('assistant.nasTitle')} wide {onclose}>
	<p class="muted small">{t('assistant.nasText', { count: max })}</p>
	<div class="where">
		{#if root}<button type="button" class="btn small-btn" onclick={up}><Icon name="arrowUp" size={14} />{t('assistant.nasUp')}</button>{/if}
		<span class="mono">{root ? `${root.name}/${dir}` : t('assistant.nasRoots')}</span>
	</div>
	<ul class="list">
		{#if !root}
			{#each roots as r (r.id)}
				<li><button type="button" class="row" onclick={() => open(r, '')}><Icon name="folder" size={16} /><span>{r.name}</span><span class="muted small">{r.kind}</span></button></li>
			{:else}
				<li class="muted small">{t('assistant.nasNone')}</li>
			{/each}
		{:else if entries}
			{#each entries as entry (entry.name)}
				<li>
					{#if entry.dir}
						<button type="button" class="row" onclick={() => open(root!, dir ? `${dir}/${entry.name}` : entry.name)}><Icon name="folder" size={16} /><span>{entry.name}</span></button>
					{:else}
						<label class="row" class:off={!readable.test(entry.name)}>
							<input type="checkbox" checked={isChosen(entry)} disabled={!readable.test(entry.name) || (!isChosen(entry) && chosen.length >= max)} onchange={() => toggle(entry)} />
							<Icon name="file" size={16} /><span>{entry.name}</span><span class="muted small">{formatBytes(entry.size)}</span>
						</label>
					{/if}
				</li>
			{:else}
				<li class="muted small">{t('assistant.nasEmpty')}</li>
			{/each}
		{/if}
	</ul>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button type="button" class="btn btn-primary" onclick={() => onpick(chosen)} disabled={chosen.length === 0}>{t('assistant.nasAttach', { count: chosen.length })}</button>
	</div>
</Dialog>

<style>
	.small {
		margin: 0;
		font-size: 0.75rem;
	}
	.where {
		display: flex;
		align-items: center;
		gap: 10px;
		font-size: 0.8125rem;
	}
	.small-btn {
		gap: 4px;
		height: 28px;
		padding: 0 10px;
		font-size: 0.75rem;
	}
	.list {
		display: grid;
		max-height: 50vh;
		overflow-y: auto;
		margin: 0;
		padding: 0;
		list-style: none;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.list li {
		border-bottom: 1px solid var(--border);
	}
	.list li:last-child {
		border-bottom: 0;
	}
	.list > .muted {
		padding: 10px 12px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		padding: 8px 12px;
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		text-align: left;
		cursor: pointer;
	}
	.row span:nth-of-type(1) {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.row:hover {
		background: var(--chip);
	}
	.row.off {
		opacity: 0.5;
		cursor: default;
	}
</style>
