<script lang="ts">
	// PLAN 66: choose a file of the NAS for a web app, or a folder and a name to
	// save one. Lists the places of the file manager with the rights of the
	// account (GET /files/roots, /files/list); nothing is read or written here.
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api, type FileEntry, type FileRoot } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { extOf, validFileName } from './protocol.ts';

	type Props = {
		/** folder (PLAN 70, sharing to the NAS): only a writable folder is chosen. */
		mode: 'open' | 'save' | 'folder';
		title: string;
		/** Where the dialog starts (the folder of the open document). */
		start?: { root: string; dir: string };
		/** save: the name filled in. */
		name?: string;
		/** open: the file types the app takes ([] = all). */
		types?: string[];
		/** A chosen file (open) or the file to write (save: dir + name). */
		onpick: (chosen: { root: string; path: string; entry?: FileEntry }) => void;
		onclose: () => void;
		/** open: offers "Von diesem Gerät …" (a file of this PC instead of the NAS). */
		onlocal?: () => void;
	};
	let { mode, title, start, name = '', types = [], onpick, onclose, onlocal }: Props = $props();

	const first = (() => start)();
	let roots = $state<FileRoot[] | null>(null);
	let root = $state(first?.root ?? '');
	let dir = $state(first?.dir ?? '');
	let entries = $state<FileEntry[] | null>(null);
	let error = $state('');
	let fileName = $state((() => name)());
	let chosen = $state('');
	const id = $props.id();

	const usable = $derived(
		(roots ?? []).filter((r) => r.kind !== 'stick' && (mode === 'open' || r.writable)),
	);
	const here = $derived(usable.find((r) => r.id === root));
	const parts = $derived(dir ? dir.split('/') : []);
	// Hidden like in the file manager (PLAN 69): "._name" companion files a
	// Mac leaves on shares looked like the document and failed to open.
	const visible = $derived((entries ?? []).filter((e) => !e.name.startsWith('.')));
	const folders = $derived(visible.filter((e) => e.dir));
	const files = $derived(
		visible.filter(
			(e) =>
				!e.dir &&
				mode !== 'folder' &&
				(mode === 'save' || !types.length || types.includes(extOf(e.name))),
		),
	);
	const taken = $derived(
		mode === 'save' && (entries ?? []).some((e) => !e.dir && e.name === fileName.trim()),
	);

	$effect(() => {
		api
			.get<FileRoot[]>('/files/roots')
			.then((r) => {
				roots = r;
				if (!usable.some((x) => x.id === root)) {
					root = usable[0]?.id ?? '';
					dir = '';
				}
			})
			.catch((e) => {
				roots = [];
				error = apiErrorText(e);
			});
	});

	async function load() {
		entries = null;
		error = '';
		chosen = '';
		if (!root) return;
		try {
			const list = await api.get<FileEntry[]>(
				'/files/list?' + new URLSearchParams({ root, path: dir }),
			);
			entries = [...list].sort(
				(a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name),
			);
		} catch (e) {
			entries = [];
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		void root;
		void dir;
		if (roots) void load();
	});

	const join = (a: string, b: string) => (a ? `${a}/${b}` : b);
	const valid = $derived(
		mode === 'open' ? !!chosen : mode === 'folder' ? !!root : !!root && validFileName(fileName),
	);

	function submit(event: SubmitEvent) {
		event.preventDefault();
		if (!valid) return;
		if (mode === 'open') {
			const entry = files.find((f) => f.name === chosen);
			onpick({ root, path: join(dir, chosen), entry });
		} else if (mode === 'folder') onpick({ root, path: dir });
		else onpick({ root, path: join(dir, fileName.trim()) });
	}
</script>

<Dialog {title} {onclose} wide>
	<form class="fd" onsubmit={submit}>
		<div class="where">
			<label class="place"
				><span class="sr-only">{t('webapps.dialog.place')}</span>
				<select
					class="field"
					bind:value={root}
					onchange={() => (dir = '')}
					aria-label={t('webapps.dialog.place')}
				>
					{#each usable as r (r.id)}<option value={r.id}>{r.name}</option>{/each}
				</select>
			</label>
			<nav class="crumbs" aria-label={t('webapps.dialog.folder')}>
				<button type="button" class="crumb" onclick={() => (dir = '')}>{here?.name ?? '…'}</button>
				{#each parts as part, i (i)}
					<span aria-hidden="true">›</span>
					<button
						type="button"
						class="crumb"
						onclick={() => (dir = parts.slice(0, i + 1).join('/'))}>{part}</button
					>
				{/each}
			</nav>
		</div>
		<div class="list" role="listbox" aria-labelledby="{id}-label" tabindex="-1">
			<span id="{id}-label" class="sr-only">{t('webapps.dialog.folder')}</span>
			{#if entries === null}
				<p class="muted pad" role="status">{t('common.loading')}</p>
			{:else}
				{#if dir}
					<button type="button" class="row" onclick={() => (dir = parts.slice(0, -1).join('/'))}
						><Icon name="arrowUp" size={16} /><span>{t('webapps.dialog.up')}</span></button
					>
				{/if}
				{#each folders as f (f.name)}
					<button type="button" class="row" onclick={() => (dir = join(dir, f.name))}
						><Icon name="folder" size={16} /><span class="name">{f.name}</span></button
					>
				{/each}
				{#each files as f (f.name)}
					<button
						type="button"
						class="row file"
						class:on={mode === 'open' ? chosen === f.name : fileName === f.name}
						role="option"
						aria-selected={mode === 'open' ? chosen === f.name : fileName === f.name}
						onclick={() => (mode === 'open' ? (chosen = f.name) : (fileName = f.name))}
						ondblclick={() => {
							if (mode === 'open') onpick({ root, path: join(dir, f.name), entry: f });
						}}><Icon name="file" size={16} /><span class="name">{f.name}</span></button
					>
				{/each}
				{#if !folders.length && !files.length}
					<p class="muted pad">{t('webapps.dialog.empty')}</p>
				{/if}
			{/if}
		</div>
		{#if mode === 'save'}
			<label class="stack"
				><span>{t('webapps.dialog.name')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input class="field" bind:value={fileName} maxlength="255" required autofocus /></label
			>
			{#if taken}<p class="muted small" role="status">
					{t('webapps.dialog.replaceHint', { name: fileName.trim() })}
				</p>{/if}
		{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			{#if onlocal && mode === 'open'}
				<button type="button" class="btn local" onclick={onlocal}>{t('webapps.fromDevice')}</button>
			{/if}
			<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
			<button class="btn btn-primary" disabled={!valid}
				>{mode === 'open'
					? t('webapps.dialog.open')
					: mode === 'folder'
						? t('share.here')
						: t('webapps.dialog.save')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	.fd {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
	}
	.where {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
	}
	.place select {
		max-width: 100%;
	}
	.crumbs {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 2px var(--space-1);
		min-width: 0;
		color: var(--muted);
	}
	.crumb {
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		padding: var(--space-1) var(--space-1-5);
		border-radius: 6px;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.crumb:hover {
		background: color-mix(in srgb, var(--text) 8%, transparent);
	}
	.list {
		display: flex;
		flex-direction: column;
		height: min(46vh, 360px);
		overflow-y: auto;
		border: 1px solid var(--border);
		border-radius: calc(10px * var(--radius-scale, 1));
		padding: var(--space-1);
		background: color-mix(in srgb, var(--surface) 60%, transparent);
	}
	.row {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		flex: none;
	}
	.row :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.row .name {
		overflow-wrap: anywhere;
		min-width: 0;
	}
	.row:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.row.on {
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		font-weight: 600;
	}
	.row:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.pad {
		padding: var(--space-2-5);
		margin: 0;
	}
	.stack {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.local {
		margin-inline-end: auto;
	}
</style>
