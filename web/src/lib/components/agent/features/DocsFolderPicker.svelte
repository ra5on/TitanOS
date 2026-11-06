<script lang="ts">
	// Choose a folder of a share the account can read (or write): the
	// shares of the file manager with the account's own rights (PLAN 54-D).
	import Dialog from '#lib/components/Dialog.svelte';
	import { api, type FileEntry, type FileRoot } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { FolderRef } from '#lib/features/docs.ts';
	import { place } from '#lib/features/docs.ts';

	type Props = {
		title: string;
		writable?: boolean;
		onpick: (f: FolderRef) => void;
		onclose: () => void;
	};
	let { title, writable = false, onpick, onclose }: Props = $props();

	let roots = $state<FileRoot[] | null>(null);
	let root = $state<FileRoot | null>(null);
	let dir = $state('');
	let dirs = $state<string[]>([]);
	let error = $state('');

	$effect(() => {
		api
			.get<FileRoot[]>('/files/roots')
			.then((list) => (roots = list.filter((r) => r.kind === 'share' && (!writable || r.writable))))
			.catch((e) => {
				error = apiErrorText(e);
				roots = [];
			});
	});

	async function go(next: FileRoot | null, path = '') {
		error = '';
		root = next;
		dir = path;
		dirs = [];
		if (!next) return;
		try {
			const list = await api.get<FileEntry[]>(
				`/files/list?root=${encodeURIComponent(next.id)}&path=${encodeURIComponent(path)}`,
			);
			dirs = list
				.filter((e) => e.dir && !e.name.startsWith('.'))
				.map((e) => e.name)
				.sort((a, b) => a.localeCompare(b));
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const up = () => {
		if (!dir) void go(null);
		else void go(root, dir.split('/').slice(0, -1).join('/'));
	};
</script>

<Dialog {title} {onclose}>
	{#if root}
		<p class="where">
			<button type="button" class="btn" onclick={up}>‹</button>
			<strong>{place({ root: root.id, path: dir })}</strong>
		</p>
	{/if}
	<ul class="list">
		{#if !root}
			{#each roots ?? [] as r (r.id)}
				<li>
					<button type="button" onclick={() => go(r)}>📁 {r.name}</button>
				</li>
			{:else}
				<li class="muted">{roots ? t('agent.f.docs.pickNone') : '…'}</li>
			{/each}
		{:else}
			{#each dirs as d (d)}
				<li>
					<button type="button" onclick={() => go(root, dir ? `${dir}/${d}` : d)}>📁 {d}</button>
				</li>
			{:else}
				<li class="muted">{t('agent.f.docs.pickEmpty')}</li>
			{/each}
		{/if}
	</ul>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<p class="muted">{writable ? t('agent.f.docs.pickWrite') : t('agent.f.docs.pickRead')}</p>
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button
			type="button"
			class="btn btn-primary"
			disabled={!root}
			onclick={() => root && onpick({ root: root.id, path: dir })}
			>{t('agent.f.docs.pickThis')}</button
		>
	</div>
</Dialog>

<style>
	.where {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 0 0 8px;
		overflow-wrap: anywhere;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		max-height: 50vh;
		overflow-y: auto;
		border: 1px solid var(--border);
		border-radius: 10px;
	}
	.list li {
		border-top: 1px solid var(--border);
	}
	.list li:first-child {
		border-top: 0;
	}
	.list button {
		width: 100%;
		text-align: left;
		padding: 10px 12px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.list .muted {
		padding: 10px 12px;
	}
</style>
