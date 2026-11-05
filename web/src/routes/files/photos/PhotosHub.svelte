<script lang="ts">
	// Photos in the file manager (PLAN 52, decisions of the owner of
	// 5.11.2026): the timeline over the account's photo folders, the search
	// in normal words over the descriptions of the KI-Agent, albums (shared
	// with other accounts, only references) and the map. The view sits in
	// the file manager in place of the folder list.
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api } from '#lib/api.ts';
	import { poll } from '#lib/poll.ts';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { thumbnailURL } from '#lib/file-actions.ts';
	import PhotoGrid from './PhotoGrid.svelte';
	import PhotoMap from './PhotoMap.svelte';
	import PhotoPrefs from './PhotoPrefs.svelte';
	import PhotoViewer from './PhotoViewer.svelte';
	import {
		byMonth,
		byPlace,
		photoKey,
		placeText,
		type PhotoAlbum,
		type PhotoItem,
		type PhotoPrefs as Prefs,
		type PhotosAnswer,
	} from './photos.ts';

	let {
		view,
		onview,
		onclose,
		onfolder,
	}: {
		view: 'timeline' | 'albums' | 'map';
		onview: (v: 'timeline' | 'albums' | 'map') => void;
		/** Back to the folders. */
		onclose: () => void;
		onfolder: (root: string, dir: string) => void;
	} = $props();

	let data = $state<PhotosAnswer | null>(null);
	let albums = $state<PhotoAlbum[]>([]);
	let error = $state('');
	let query = $state('');
	let hits = $state<PhotoItem[] | null>(null);
	let viewer = $state<{ items: PhotoItem[]; index: number } | null>(null);
	let settings = $state(false);
	let album = $state<PhotoAlbum | null>(null);
	let place = $state<PhotoItem[] | null>(null);
	let dialog = $state<
		| { kind: 'name'; album: PhotoAlbum | null; name: string }
		| { kind: 'share'; album: PhotoAlbum; accounts: string[] | null; members: string[] }
		| { kind: 'delete'; album: PhotoAlbum }
		| null
	>(null);

	async function load() {
		try {
			const d = await api.get<PhotosAnswer>('/photos');
			d.items ??= [];
			d.prefs.folders ??= [];
			data = d;
			error = '';
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function loadAlbums() {
		try {
			albums = (await api.get<{ albums: PhotoAlbum[] }>('/photos/albums')).albums;
		} catch {
			albums = [];
		}
	}
	$effect(() => {
		void loadAlbums();
		void load();
	});
	const busy = $derived(!!data && (data.scanning || data.describe.running));
	// While folders are read or photos described, only the state is asked
	// for; the list comes again once that has ended.
	$effect(() => {
		if (!busy) return;
		return poll(
			async () => {
				const s = await api.get<Omit<PhotosAnswer, 'items'>>('/photos?status=1');
				if (!data) return;
				const ended =
					(data.scanning && !s.scanning) || (data.describe.running && !s.describe.running);
				data = { ...data, ...s, items: data.items };
				if (ended) await load();
			},
			{ interval: 6000 },
		);
	});
	const items = $derived(data?.items ?? []);
	const months = $derived(byMonth(items));
	const images = $derived(items.filter((p) => p.kind === 'image').length);
	const byKey = $derived(new Map(items.map((p) => [photoKey(p), p])));
	const monthName = (d: Date) =>
		d.toLocaleDateString(i18n.locale === 'de' ? 'de-DE' : 'en-GB', {
			month: 'long',
			year: 'numeric',
		});

	let searchTimer: ReturnType<typeof setTimeout> | undefined;
	function search() {
		clearTimeout(searchTimer);
		const q = query.trim();
		if (!q) {
			hits = null;
			return;
		}
		searchTimer = setTimeout(async () => {
			try {
				hits = (
					await api.get<{ items: PhotoItem[] }>('/photos/search?' + new URLSearchParams({ q }))
				).items;
			} catch (e) {
				error = apiErrorText(e);
			}
		}, 250);
	}
	async function describe(run: boolean) {
		error = '';
		try {
			await api.post('/photos/describe', { run });
			await load();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function savePrefs(p: Prefs) {
		await api.put('/photos/prefs', p);
		settings = false;
		await load();
	}
	async function albumSave(id: string, body: Record<string, unknown>) {
		try {
			const a = await api.put<PhotoAlbum>(`/photos/albums/${id}`, body);
			await loadAlbums();
			if (album?.id === a.id) album = albums.find((x) => x.id === a.id) ?? a;
			return a;
		} catch (e) {
			error = apiErrorText(e);
			return null;
		}
	}
	function albumItems(a: PhotoAlbum): PhotoItem[] {
		return a.items.map((k) => {
			const known = byKey.get(k);
			if (known) return known;
			const i = k.indexOf(':');
			return { root: k.slice(0, i), path: k.slice(i + 1), kind: 'image', size: 0, modified: '' };
		});
	}
	const open = (list: PhotoItem[], p: PhotoItem) =>
		(viewer = { items: list, index: Math.max(0, list.indexOf(p)) });
	const places = $derived(byPlace(items));
	const located = $derived(places.reduce((n, g) => n + g.items.length, 0));
</script>

<div class="photos">
	<div class="top">
		<div class="seg" role="group" aria-label={t('photos.mode')}>
			<button type="button" onclick={onclose}
				><Icon name="folder" size={15} /> {t('photos.folderView')}</button
			>
			<button type="button" class="on" aria-pressed="true"
				><Icon name="photo" size={15} /> {t('photos.title')}</button
			>
		</div>
		<label class="search">
			<Icon name="search" size={16} />
			<input
				type="search"
				bind:value={query}
				oninput={search}
				placeholder={t('photos.searchHint')}
				aria-label={t('photos.search')}
			/>
		</label>
		<button type="button" class="btn" onclick={() => (settings = true)}
			><Icon name="gear" size={15} /> {t('photos.folders')}</button
		>
	</div>
	<div class="tabs" role="tablist" aria-label={t('photos.title')}>
		{#each [['timeline', 'photos.timeline', 'photo'], ['albums', 'photos.albums', 'list'], ['map', 'photos.map', 'grid']] as const as [id, label, icon] (id)}
			<button
				type="button"
				role="tab"
				aria-selected={view === id}
				class:on={view === id}
				onclick={() => {
					onview(id);
					album = null;
					place = null;
				}}><Icon name={icon} size={15} /> {t(label)}</button
			>
		{/each}
	</div>

	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if data?.broken}<p class="notice-error" role="alert">
			{t('photos.broken', { error: data.broken })}
		</p>{/if}

	{#if data && data.ai && images > 0}
		<div class="ai" class:running={data.describe.running}>
			<Icon name="sparkle" size={18} />
			<span class="txt">
				<strong>{t('photos.aiTitle')}</strong>
				<small
					>{t('photos.aiCount', { done: data.described, total: images })}{data.describe.running
						? ' · ' + t('photos.aiRunning', { model: data.describe.model ?? '' })
						: ''}{data.describe.error ? ' · ' + data.describe.error : ''}</small
				>
			</span>
			<span class="bar" aria-hidden="true"
				><i style:width="{images ? Math.round((data.described / images) * 100) : 0}%"></i></span
			>
			<button
				type="button"
				class="btn"
				class:btn-primary={!data.describe.running && data.described < images}
				onclick={() => describe(!data!.describe.running)}
				>{data.describe.running ? t('photos.stop') : t('photos.describeNow')}</button
			>
			<button type="button" class="btn" onclick={() => (settings = true)}
				><Icon name="gear" size={15} /> {t('photos.when')}</button
			>
		</div>
	{/if}

	{#if !data}
		<p class="muted pad">{t('common.loading')}</p>
	{:else if !data.prefs.folders?.length}
		<div class="empty">
			<Icon name="photo" size={36} />
			<h3>{t('photos.startTitle')}</h3>
			<p class="muted">{t('photos.startText')}</p>
			<button type="button" class="btn btn-primary" onclick={() => (settings = true)}
				>{t('photos.addFolder')}</button
			>
		</div>
	{:else if hits}
		<p class="muted">{t('photos.hits', { count: hits.length })}</p>
		{#if hits.length}<PhotoGrid items={hits} captions onopen={(p) => open(hits!, p)} />{/if}
	{:else if view === 'timeline'}
		{#if busy && !items.length}<p class="muted pad">{t('photos.scanning')}</p>{/if}
		{#each months as m (m.key)}
			<h3 class="month">
				{monthName(m.date)} <small>{t('photos.count', { count: m.items.length })}</small>
			</h3>
			<PhotoGrid items={m.items} onopen={(p) => open(items, p)} />
		{:else}
			{#if !busy}<p class="muted pad">{t('photos.none')}</p>{/if}
		{/each}
	{:else if view === 'albums'}
		{#if album}
			{@const a = album}
			<div class="album-head">
				<button type="button" class="btn" onclick={() => (album = null)}
					><Icon name="chevronLeft" size={15} /> {t('photos.albums')}</button
				>
				<h3>{a.name}</h3>
				<small class="muted"
					>{t('photos.count', { count: a.items.length })}{a.owner
						? ' · ' + t('photos.sharedBy', { name: a.owner })
						: a.members?.length
							? ' · ' + t('photos.sharedWith', { names: a.members.join(', ') })
							: ''}</small
				>
				{#if !a.owner}
					<span class="grow"></span>
					<button
						type="button"
						class="btn"
						onclick={() => (dialog = { kind: 'name', album: a, name: a.name })}
						><Icon name="edit" size={15} /> {t('photos.rename')}</button
					>
					<button
						type="button"
						class="btn"
						onclick={async () => {
							dialog = { kind: 'share', album: a, accounts: null, members: [...(a.members ?? [])] };
							const r = await api
								.get<{ accounts: string[] }>('/photos/accounts')
								.catch(() => ({ accounts: [] }));
							if (dialog?.kind === 'share') dialog.accounts = r.accounts;
						}}><Icon name="external" size={15} /> {t('photos.share')}</button
					>
					<button
						type="button"
						class="btn btn-danger-outline"
						onclick={() => (dialog = { kind: 'delete', album: a })}
						><Icon name="trash" size={15} /></button
					>
				{/if}
			</div>
			{#if a.items.length}
				<PhotoGrid items={albumItems(a)} onopen={(p) => open(albumItems(a), p)} />
			{:else}
				<p class="muted pad">{t('photos.albumEmpty')}</p>
			{/if}
		{:else}
			<div class="albums">
				{#each albums as a (a.id + (a.owner ?? ''))}
					{@const cover = albumItems(a).slice(0, 3)}
					<button type="button" class="album" onclick={() => (album = a)}>
						<span class="cover">
							{#each cover as p (photoKey(p))}<img
									src={thumbnailURL(p.root, p.path, 320, p.modified)}
									alt=""
									loading="lazy"
								/>{:else}<span class="blank"><Icon name="photo" size={28} /></span>{/each}
						</span>
						<span class="meta"
							><strong>{a.name}</strong><small
								>{t('photos.count', { count: a.items.length })}{a.owner
									? ' · ' + t('photos.sharedBy', { name: a.owner })
									: a.members?.length
										? ' · ' + t('photos.sharedWithCount', { count: a.members.length })
										: ''}</small
							></span
						>
					</button>
				{/each}
				<button
					type="button"
					class="album new"
					onclick={() => (dialog = { kind: 'name', album: null, name: '' })}
					><Icon name="plus" size={24} /><span>{t('photos.newAlbum')}</span></button
				>
			</div>
		{/if}
	{:else if view === 'map'}
		<p class="muted">{t('photos.located', { count: located, none: items.length - located })}</p>
		{#if data.prefs.map}
			{#if located}<PhotoMap {items} onopen={(g) => (place = g)} />{/if}
		{:else}
			<div class="maphint">
				<p class="muted">{t('photos.mapOffText')}</p>
				<button type="button" class="btn" onclick={() => savePrefs({ ...data!.prefs, map: true })}
					>{t('photos.mapSwitchOn')}</button
				>
			</div>
			<ul class="places">
				{#each places as g, i (i)}
					<li>
						<button type="button" onclick={() => (place = g.items)}>
							<img
								src={thumbnailURL(g.items[0].root, g.items[0].path, 160, g.items[0].modified)}
								alt=""
							/>
							<span
								><strong>{placeText(g.lat, g.lon, i18n.locale)}</strong><small
									>{t('photos.count', { count: g.items.length })}</small
								></span
							>
						</button>
					</li>
				{/each}
			</ul>
		{/if}
		{#if place}
			<h3 class="month">
				{t('photos.atPlace')} <small>{t('photos.count', { count: place.length })}</small>
			</h3>
			<PhotoGrid items={place} onopen={(p) => open(place!, p)} />
		{/if}
	{/if}
</div>

{#if viewer}
	<PhotoViewer
		items={viewer.items}
		bind:index={viewer.index}
		{albums}
		onalbum={async (p, id, name) => {
			await albumSave(id, { name: name ?? '', add: [photoKey(p)] });
		}}
		onfolder={(root, dir) => {
			viewer = null;
			onfolder(root, dir);
		}}
		onclose={() => (viewer = null)}
	/>
{/if}
{#if settings && data}
	<PhotoPrefs
		prefs={data.prefs}
		ai={data.ai}
		described={data.described}
		total={images}
		running={data.describe.running}
		onsave={savePrefs}
		ondescribe={(run) => describe(run)}
		onclose={() => (settings = false)}
	/>
{/if}
{#if dialog?.kind === 'name'}
	{@const d = dialog}
	<Dialog
		title={d.album ? t('photos.rename') : t('photos.newAlbum')}
		onclose={() => (dialog = null)}
	>
		<form
			class="form"
			onsubmit={async (e) => {
				e.preventDefault();
				const a = await albumSave(d.album?.id ?? 'new', { name: d.name });
				dialog = null;
				if (a && !d.album) album = albums.find((x) => x.id === a.id) ?? a;
			}}
		>
			<input
				class="field"
				bind:value={d.name}
				maxlength="80"
				required
				aria-label={t('photos.albumName')}
			/>
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary">{t('common.save')}</button>
			</div>
		</form>
	</Dialog>
{:else if dialog?.kind === 'share'}
	{@const d = dialog}
	<Dialog title={t('photos.share')} onclose={() => (dialog = null)}>
		<p class="muted">{t('photos.shareHint')}</p>
		{#if d.accounts === null}
			<p class="muted">…</p>
		{:else if d.accounts.length === 0}
			<p class="muted">{t('photos.noAccounts')}</p>
		{:else}
			<div class="names">
				{#each d.accounts as name (name)}
					<label class="check"
						><input
							type="checkbox"
							checked={d.members.includes(name)}
							onchange={(e) =>
								(d.members = e.currentTarget.checked
									? [...d.members, name]
									: d.members.filter((m) => m !== name))}
						/><span>{name}</span></label
					>
				{/each}
			</div>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				onclick={async () => {
					await albumSave(d.album.id, { members: d.members });
					dialog = null;
				}}>{t('common.save')}</button
			>
		</div>
	</Dialog>
{:else if dialog?.kind === 'delete'}
	{@const d = dialog}
	<Dialog title={t('photos.deleteAlbum')} onclose={() => (dialog = null)}>
		<p>{t('photos.deleteAlbumText', { name: d.album.name })}</p>
		<div class="dialog-actions">
			<button
				type="button"
				class="btn btn-danger"
				onclick={async () => {
					try {
						await api.delete(`/photos/albums/${d.album.id}`);
					} catch (e) {
						error = apiErrorText(e);
					}
					dialog = null;
					album = null;
					await loadAlbums();
				}}>{t('common.delete')}</button
			>
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.photos {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 14px;
		min-width: 0;
	}
	.top {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.seg,
	.tabs {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 999px;
		padding: 2px;
		gap: 2px;
	}
	.seg button,
	.tabs button {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		padding: 6px 14px;
		border-radius: 999px;
		cursor: pointer;
	}
	.seg button.on,
	.tabs button.on {
		background: var(--accent);
		color: var(--on-accent);
	}
	.tabs {
		align-self: flex-start;
	}
	.search {
		flex: 1;
		min-width: 220px;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 0 12px;
		min-height: 38px;
		border: 1px solid color-mix(in srgb, var(--accent) 35%, var(--border));
		border-radius: 12px;
		background: color-mix(in srgb, var(--surface) 70%, transparent);
		color: var(--muted);
	}
	.search:focus-within {
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 16%, transparent);
	}
	.search input {
		flex: 1;
		min-width: 0;
		border: 0;
		outline: none;
		background: transparent;
		color: var(--text);
		font: inherit;
	}
	.ai {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
		padding: 8px 12px;
		border-radius: 12px;
		border: 1px solid color-mix(in srgb, var(--accent) 35%, var(--border));
		background: linear-gradient(
			110deg,
			color-mix(in srgb, var(--accent) 14%, var(--surface)),
			color-mix(in srgb, var(--surface) 70%, transparent)
		);
	}
	.ai :global(svg) {
		color: var(--accent);
	}
	.ai.running :global(svg) {
		animation: glow 1.6s ease-in-out infinite;
	}
	@keyframes glow {
		50% {
			opacity: 0.4;
		}
	}
	.txt {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.txt small {
		color: var(--muted);
	}
	.bar {
		flex: 1;
		min-width: 80px;
		max-width: 200px;
		height: 6px;
		border-radius: 6px;
		background: color-mix(in srgb, var(--text) 12%, transparent);
		overflow: hidden;
	}
	.bar i {
		display: block;
		height: 100%;
		background: var(--accent);
		transition: width 0.4s ease;
	}
	.month {
		margin: 8px 0 0;
		font-size: var(--text-sm);
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
		display: flex;
		gap: 8px;
		align-items: baseline;
	}
	.month small {
		text-transform: none;
		letter-spacing: 0;
		font-weight: 400;
	}
	.pad {
		padding: var(--space-3) 0;
	}
	.empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
		gap: 8px;
		padding: 40px 16px;
	}
	.empty :global(svg) {
		color: var(--accent);
	}
	.empty h3 {
		margin: 0;
	}
	.albums {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
		gap: 12px;
	}
	.album {
		display: flex;
		flex-direction: column;
		padding: 0;
		border-radius: 14px;
		overflow: hidden;
		border: 1px solid var(--border);
		background: color-mix(in srgb, var(--surface) 70%, transparent);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.cover {
		display: grid;
		grid-template-columns: 2fr 1fr;
		grid-template-rows: 1fr 1fr;
		gap: 2px;
		aspect-ratio: 4 / 3;
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.cover img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.cover img:first-child {
		grid-row: 1 / 3;
	}
	.blank {
		grid-column: 1 / 3;
		grid-row: 1 / 3;
		display: grid;
		place-items: center;
		color: var(--muted);
	}
	.meta {
		padding: 8px 10px;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.meta small {
		color: var(--muted);
	}
	.album.new {
		align-items: center;
		justify-content: center;
		gap: 8px;
		min-height: 200px;
		border-style: dashed;
		color: var(--muted);
	}
	.album-head {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.album-head h3 {
		margin: 0;
	}
	.grow {
		flex: 1;
	}
	.maphint {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
		padding: 10px 12px;
		border: 1px dashed var(--border);
		border-radius: 12px;
	}
	.maphint p {
		margin: 0;
		flex: 1;
	}
	.places {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
		gap: 8px;
	}
	.places button {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 6px;
		border-radius: 12px;
		border: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.places img {
		width: 56px;
		height: 56px;
		object-fit: cover;
		border-radius: 8px;
	}
	.places span {
		display: flex;
		flex-direction: column;
	}
	.places small {
		color: var(--muted);
	}
	.form {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.names {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 16px;
	}
	.dialog-actions {
		display: flex;
		gap: var(--space-2);
		justify-content: flex-end;
		flex-wrap: wrap;
	}
	@media (max-width: 760px) {
		.photos {
			padding: 8px;
		}
		.search {
			min-width: 0;
			order: 3;
			flex-basis: 100%;
		}
	}
</style>
