<script lang="ts">
	// Photo folders and descriptions (PLAN 52): the folders of the account's
	// shares that hold its photos, and when the picture model of the
	// KI-Agent describes them – the first run by the button, then by choice
	// only by button, at night, or at night and whenever no chat runs. The
	// map loads tiles from OpenStreetMap only when switched on.
	import Icon from '#lib/components/Icon.svelte';
	import { api, type FileEntry, type FileRoot } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { PhotoPrefs } from './photos.ts';

	let {
		prefs,
		ai,
		described,
		total,
		running,
		onsave,
		ondescribe,
		onclose,
	}: {
		prefs: PhotoPrefs;
		ai: boolean;
		described: number;
		total: number;
		running: boolean;
		onsave: (p: PhotoPrefs) => Promise<void>;
		ondescribe: (run: boolean) => void;
		onclose: () => void;
	} = $props();

	const start = (() => $state.snapshot(prefs))();
	let draft = $state<PhotoPrefs>(structuredClone(start));
	let roots = $state<FileRoot[]>([]);
	let browsing = $state<{ root: FileRoot | null; path: string; folders: string[] } | null>(null);
	let error = $state('');
	let busy = $state(false);
	$effect(() => {
		api
			.get<FileRoot[]>('/files/roots')
			.then((r) => (roots = r.filter((x) => x.kind !== 'stick')))
			.catch(() => (roots = []));
	});
	const rootName = (id: string) =>
		roots.find((r) => r.id === id)?.name ?? id.replace(/^share\//, '');
	async function browse(root: FileRoot | null, path = '') {
		error = '';
		if (!root) {
			browsing = { root: null, path: '', folders: [] };
			return;
		}
		try {
			const list = await api.get<FileEntry[]>(
				`/files/list?root=${encodeURIComponent(root.id)}&path=${encodeURIComponent(path)}`,
			);
			browsing = {
				root,
				path,
				folders: list
					.filter((e) => e.dir && !e.name.startsWith('.'))
					.map((e) => e.name)
					.sort((a, b) => a.localeCompare(b)),
			};
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	function choose() {
		if (!browsing?.root) return;
		const f = { root: browsing.root.id, path: browsing.path };
		if (!draft.folders.some((x) => x.root === f.root && x.path === f.path))
			draft.folders = [...draft.folders, f];
		browsing = null;
	}
	async function save() {
		busy = true;
		error = '';
		try {
			await onsave(draft);
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	const hours = Array.from({ length: 24 }, (_, i) => i);
	const modes = $derived([
		{ id: 'manual', title: t('photos.modeManual'), hint: t('photos.modeManualHint') },
		{ id: 'night', title: t('photos.modeNight'), hint: t('photos.modeNightHint') },
		{ id: 'idle', title: t('photos.modeIdle'), hint: t('photos.modeIdleHint') },
	] as const);
</script>

<div class="scrim" role="presentation" onclick={onclose}></div>
<aside class="panel" aria-label={t('photos.settings')}>
	<header>
		<h2>{t('photos.folders')}</h2>
		<button type="button" class="x" aria-label={t('common.close')} onclick={onclose}
			><Icon name="close" size={18} /></button
		>
	</header>
	<p class="muted">{t('photos.foldersHint')}</p>
	<ul class="folders">
		{#each draft.folders as f, i (f.root + f.path)}
			<li>
				<Icon name="folder" size={16} />
				<span>{rootName(f.root)}{f.path ? ' › ' + f.path.replaceAll('/', ' › ') : ''}</span>
				<button
					type="button"
					class="x small"
					aria-label={t('photos.removeFolder')}
					onclick={() => (draft.folders = draft.folders.filter((_, j) => j !== i))}
					><Icon name="close" size={14} /></button
				>
			</li>
		{:else}
			<li class="muted">{t('photos.noFolders')}</li>
		{/each}
	</ul>
	{#if browsing}
		<div class="browse">
			<div class="crumbs">
				<button type="button" class="link" onclick={() => browse(null)}>{t('photos.shares')}</button
				>
				{#if browsing.root}
					› <button type="button" class="link" onclick={() => browse(browsing!.root, '')}
						>{browsing.root.name}</button
					>
					{#each browsing.path ? browsing.path.split('/') : [] as part, i (i)}
						› <button
							type="button"
							class="link"
							onclick={() =>
								browse(
									browsing!.root,
									browsing!.path
										.split('/')
										.slice(0, i + 1)
										.join('/'),
								)}>{part}</button
						>
					{/each}
				{/if}
			</div>
			<ul class="list">
				{#if !browsing.root}
					{#each roots as r (r.id)}
						<li>
							<button type="button" onclick={() => browse(r)}
								><Icon name="folder" size={16} />{r.name}</button
							>
						</li>
					{/each}
				{:else}
					{#each browsing.folders as name (name)}
						<li>
							<button
								type="button"
								onclick={() =>
									browse(browsing!.root, browsing!.path ? `${browsing!.path}/${name}` : name)}
								><Icon name="folder" size={16} />{name}</button
							>
						</li>
					{:else}
						<li class="muted">{t('photos.noSubfolders')}</li>
					{/each}
				{/if}
			</ul>
			<div class="row">
				<button type="button" class="btn" onclick={() => (browsing = null)}
					>{t('common.cancel')}</button
				>
				<button type="button" class="btn btn-primary" disabled={!browsing.root} onclick={choose}
					>{t('photos.chooseFolder')}</button
				>
			</div>
		</div>
	{:else}
		<button type="button" class="btn add" onclick={() => browse(null)}
			><Icon name="plus" size={15} /> {t('photos.addFolder')}</button
		>
	{/if}

	<h2 class="sub">{t('photos.aiTitle')}</h2>
	{#if ai}
		<p class="muted">{t('photos.aiHint')}</p>
		<div class="modes" role="radiogroup" aria-label={t('photos.aiWhen')}>
			{#each modes as m (m.id)}
				<label class="mode" class:on={draft.describe === m.id}>
					<input type="radio" name="describe" value={m.id} bind:group={draft.describe} />
					<span><strong>{m.title}</strong><small>{m.hint}</small></span>
				</label>
			{/each}
		</div>
		{#if draft.describe !== 'manual'}
			<div class="row hours">
				<span>{t('photos.window')}</span>
				<select class="field" bind:value={draft.from} aria-label={t('photos.from')}>
					{#each hours as h (h)}<option value={h}>{String(h).padStart(2, '0')}:00</option>{/each}
				</select>
				<span>–</span>
				<select class="field" bind:value={draft.to} aria-label={t('photos.to')}>
					{#each hours as h (h)}<option value={h}>{String(h).padStart(2, '0')}:00</option>{/each}
				</select>
			</div>
			{#if !prefs.started}<p class="muted small">{t('photos.firstByButton')}</p>{/if}
		{/if}
		<p class="muted small">{t('photos.aiCount', { done: described, total })}</p>
		<button
			type="button"
			class="btn"
			class:btn-primary={!running}
			onclick={() => ondescribe(!running)}
			>{running ? t('photos.stop') : t('photos.describeNow')}</button
		>
	{:else}
		<p class="muted">{t('photos.aiOff')}</p>
	{/if}

	<h2 class="sub">{t('photos.mapTitle')}</h2>
	<label class="check"
		><input type="checkbox" bind:checked={draft.map} /><span>{t('photos.mapOn')}</span></label
	>
	<p class="muted small">{t('photos.mapHint')}</p>

	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="row end">
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button type="button" class="btn btn-primary" disabled={busy} onclick={save}
			>{t('common.save')}</button
		>
	</div>
</aside>

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 70;
		background: rgb(0 0 0 / 0.35);
	}
	.panel {
		position: fixed;
		z-index: 71;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(440px, 100%);
		overflow-y: auto;
		padding: 18px 18px calc(18px + env(safe-area-inset-bottom, 0px));
		display: flex;
		flex-direction: column;
		gap: 10px;
		background: color-mix(in srgb, var(--surface) 94%, transparent);
		backdrop-filter: blur(24px);
		border-left: 1px solid var(--border);
		box-shadow: -20px 0 50px -20px rgb(0 0 0 / 0.6);
	}
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}
	h2 {
		margin: 0;
		font-size: var(--text-lg);
	}
	h2.sub {
		font-size: var(--text-md);
		margin-top: 10px;
	}
	.muted {
		margin: 0;
	}
	.small {
		font-size: var(--text-sm);
	}
	.x {
		width: 36px;
		height: 36px;
		border-radius: 50%;
		border: 0;
		display: grid;
		place-items: center;
		background: color-mix(in srgb, var(--text) 8%, transparent);
		color: var(--text);
		cursor: pointer;
	}
	.x.small {
		width: 28px;
		height: 28px;
		margin-left: auto;
	}
	.folders,
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.folders li {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px 0;
		border-bottom: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
	}
	.add {
		align-self: flex-start;
	}
	.browse {
		border: 1px solid var(--border);
		border-radius: 12px;
		padding: 10px;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.crumbs {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.link {
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		cursor: pointer;
		padding: 0;
	}
	.list {
		max-height: 240px;
		overflow-y: auto;
	}
	.list button {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		min-height: 38px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		border-radius: 8px;
		padding: 0 6px;
	}
	.list button:hover {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.row {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.row.end {
		justify-content: flex-end;
		margin-top: 8px;
	}
	.modes {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.mode {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		padding: 8px 10px;
		border-radius: 10px;
		border: 1px solid var(--border);
		cursor: pointer;
	}
	.mode.on {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 10%, transparent);
	}
	.mode span {
		display: flex;
		flex-direction: column;
	}
	.mode small {
		color: var(--muted);
	}
	.hours select {
		width: 6.5rem;
	}
</style>
