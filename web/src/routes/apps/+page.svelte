<script lang="ts">
	import { api, ApiError, type AppSource, type AppStore, type CatalogApp, type DockerStatus, type Share } from '#lib/api.ts';
	import { localize } from '#lib/compose/template.ts';
	import { errorText, i18n, t, tOr } from '#lib/i18n/index.svelte.ts';
	import InstallPanel from './InstallPanel.svelte';

	// The order the categories are offered in; what a source calls differently follows.
	const CATEGORIES = ['media', 'photos', 'backup', 'security', 'productivity', 'development', 'network', 'home', 'ai', 'finance', 'social'];

	// Stores of others that TitanOS can read. They are offered, never added
	// by themselves: their apps are not checked by TitanOS.
	const KNOWN = [
		{ name: 'CasaOS App Store', url: 'https://github.com/IceWhaleTech/CasaOS-AppStore', note: 'apps.known.casaos' },
		{ name: 'BigBear CasaOS', url: 'https://github.com/bigbeartechworld/big-bear-casaos', note: 'apps.known.bigbear' }
	] as const;

	let store = $state<AppStore | null>(null);
	let docker = $state<DockerStatus | null>(null);
	let shares = $state<Share[]>([]);
	let error = $state('');
	let search = $state('');
	let category = $state('all');
	let from = $state('all');
	let sourcesOpen = $state(false);
	let selected = $state<CatalogApp | null>(null);
	let draft = $state({ url: '', name: '' });
	let sourceError = $state('');
	let busy = $state(false);

	const code = (err: unknown) => (err instanceof ApiError ? err.code : 'internal');

	$effect(() => {
		api.get<AppStore>('/apps').then((res) => (store = res), (err) => (error = errorText(code(err))));
		api.get<DockerStatus>('/docker').then((res) => (docker = res), () => {});
		api.get<Share[]>('/shares').then((res) => (shares = res), () => {});
	});

	const apps = $derived(store?.apps ?? []);
	const sources = $derived(store?.sources ?? []);
	const suggestions = $derived(KNOWN.filter((known) => !sources.some((source) => source.url === known.url)));
	const sourceName = (id: string) => sources.find((source) => source.id === id)?.name ?? id;
	const categoryOf = (app: CatalogApp) => app.category || 'other';
	const categoryLabel = (key: string) => tOr(`apps.category.${key}`, key.replace(/^./, (c) => c.toUpperCase()));
	const categories = $derived.by(() => {
		const present = new Set(apps.map(categoryOf));
		const known = CATEGORIES.filter((key) => present.has(key));
		const others = [...present].filter((key) => !CATEGORIES.includes(key) && key !== 'other').sort();
		return ['all', ...known, ...others, ...(present.has('other') ? ['other'] : [])];
	});
	const describe = (app: CatalogApp) => localize(app.description, i18n.locale) || app.image;
	const visible = $derived.by(() => {
		const query = search.trim().toLowerCase();
		return apps.filter((app) => (from === 'all' || app.source === from) && (category === 'all' || categoryOf(app) === category) && (query === '' || `${app.name} ${app.id} ${describe(app)}`.toLowerCase().includes(query)));
	});
	const installed = (app: CatalogApp) => docker?.apps.includes(app.id) ?? false;
	const ready = $derived(!!docker?.settings.enabled && docker.running);

	const monogram = (app: CatalogApp) => app.name.replace(/[^a-zA-Z0-9]/g, '').slice(0, 2).replace(/^./, (c) => c.toUpperCase()) || '?';
	const hue = (app: CatalogApp) => [...app.id].reduce((sum, char) => (sum * 31 + char.charCodeAt(0)) % 360, 7);

	function checked(source: AppSource): string {
		if (source.error) return tOr(`apps.sourceError.${source.error}`, source.error);
		const time = new Date(source.updated).getTime();
		if (!time || time < 0) return t('apps.neverChecked');
		if (source.builtin) return t('apps.builtin');
		return t('apps.checked', { time: new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'short', timeStyle: 'short' }).format(time) });
	}

	async function change(action: () => Promise<AppStore>) {
		busy = true;
		sourceError = '';
		try {
			store = await action();
			return true;
		} catch (err) {
			sourceError = errorText(code(err));
			return false;
		} finally {
			busy = false;
		}
	}

	async function addSource(event: SubmitEvent) {
		event.preventDefault();
		if (await change(() => api.post<AppStore>('/apps/sources', { url: draft.url, name: draft.name }))) draft = { url: '', name: '' };
	}

	async function removeSource(source: AppSource) {
		if (selected?.source === source.id) selected = null;
		if (from === source.id) from = 'all';
		await change(() => api.delete<AppStore>(`/apps/sources/${source.id}`));
	}
</script>

<div class="head">
	<div class="title">
		<h1>{t('apps.title')}</h1>
		{#if store}<p class="muted">{t(sources.length === 1 ? 'apps.summaryOne' : 'apps.summary', { apps: apps.length, sources: sources.length })}</p>{/if}
	</div>
	<div class="buttons">
		<button type="button" class="btn" aria-expanded={sourcesOpen} onclick={() => (sourcesOpen = !sourcesOpen)}>{t('apps.sources')}</button>
		<a class="btn" href="/docker#import">{t('apps.import')}</a>
	</div>
</div>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if store && sources[0]?.builtin && sources[0].count === 0}
	<p class="notice-warn" role="status">{sources[0].error ? t('apps.officialOffline') : t('apps.officialPending')}</p>
{/if}
{#if docker && !ready}
	<p class="notice-warn" role="status">{t('apps.dockerOff')} <a href="/docker">{t('apps.dockerOpen')}</a></p>
{/if}

{#if sourcesOpen && store}
	<section class="tile sources" aria-labelledby="sources-title">
		<div class="sources-head">
			<h2 class="tile-title" id="sources-title">{t('apps.sources')}</h2>
			<span class="muted">{t('apps.sourcesNote')}</span>
			<button type="button" class="btn small" onclick={() => change(() => api.post<AppStore>('/apps/refresh'))} disabled={busy}>{t('apps.refresh')}</button>
		</div>
		{#each sources as source (source.id)}
			<div class="source">
				<span class="dot" class:ok={!source.error} class:warn={!!source.error} aria-hidden="true"></span>
				<div class="source-name">
					<span>{source.name}</span>
					{#if source.url}<span class="muted mono url">{source.url}</span>{/if}
				</div>
				{#if source.format}<span class="chip">{t(`apps.format.${source.format}`)}</span>{/if}
				<span class="mono count">{t('apps.count', { count: source.count })}</span>
				<span class="muted when" class:failed={!!source.error}>{checked(source)}</span>
				{#if source.builtin}
					<span class="remove-space"></span>
				{:else}
					<button type="button" class="btn small" aria-label={t('apps.sourceRemove', { name: source.name })} onclick={() => removeSource(source)} disabled={busy}>✕</button>
				{/if}
			</div>
		{/each}
		{#if suggestions.length > 0}
			<div class="known">
				<span class="muted">{t('apps.knownTitle')}</span>
				{#each suggestions as known (known.url)}
					<div class="source">
						<div class="source-name">
							<span>{known.name}</span>
							<span class="muted mono url">{known.url}</span>
						</div>
						<span class="muted when wide">{t(known.note)}</span>
						<button type="button" class="btn small" onclick={() => change(() => api.post<AppStore>('/apps/sources', { url: known.url, name: known.name }))} disabled={busy}>{t('apps.knownAdd')}</button>
					</div>
				{/each}
			</div>
		{/if}
		<form class="add" onsubmit={addSource}>
			<label class="grow"><span>{t('apps.sourceUrl')}</span>
				<input class="field mono" type="url" bind:value={draft.url} placeholder="https://github.com/…" spellcheck="false" required />
			</label>
			<label><span>{t('apps.sourceName')}</span><input class="field" type="text" bind:value={draft.name} maxlength="80" /></label>
			<button type="submit" class="btn btn-primary" disabled={busy}>{t(busy ? 'apps.sourceAdding' : 'apps.sourceAdd')}</button>
			<p class="muted hint">{t('apps.sourceHint')}</p>
			<p class="notice-warn hint">{t('apps.sourceWarning')}</p>
			{#if sourceError}<p class="notice-error hint" role="alert">{sourceError}</p>{/if}
		</form>
	</section>
{/if}

{#if store}
	<div class="layout">
		<div class="catalog">
			<div class="find">
				<input class="field search" type="search" bind:value={search} placeholder={t('apps.search')} aria-label={t('apps.search')} />
				{#if sources.length > 1}
					<select class="field from" bind:value={from} aria-label={t('apps.fromSource')}>
						<option value="all">{t('apps.allSources')}</option>
						{#each sources as source (source.id)}<option value={source.id}>{source.name} ({source.count})</option>{/each}
					</select>
				{/if}
				<span class="muted shown">{t('apps.shown', { count: visible.length })}</span>
			</div>
			<div class="categories" role="group" aria-label={t('apps.categories')}>
				{#each categories as key (key)}
					<button type="button" aria-pressed={category === key} onclick={() => (category = key)}>{categoryLabel(key)}</button>
				{/each}
			</div>
			<div class="cards">
				{#each visible as app (`${app.source}/${app.id}`)}
					<button type="button" class="tile card" aria-pressed={selected?.id === app.id && selected.source === app.source} onclick={() => (selected = app)}>
						<span class="card-head">
							<span class="monogram" style:background="hsl({hue(app)} 45% 42%)" aria-hidden="true">{monogram(app)}</span>
							<span class="names"><span class="name">{app.name}</span><span class="muted category">{categoryLabel(categoryOf(app))}</span></span>
						</span>
						<span class="description">{describe(app)}</span>
						<span class="card-foot">
							<span class="muted source-label">{sourceName(app.source)}</span>
							<span class="chip" class:ok={installed(app)}>{t(installed(app) ? 'apps.installed' : 'apps.install')}</span>
						</span>
					</button>
				{/each}
			</div>
			{#if visible.length === 0}<p class="none muted">{t('apps.none')}</p>{/if}
		</div>

		{#if selected}
			{#key `${selected.source}/${selected.id}`}
				<InstallPanel app={selected} source={sourceName(selected.source)} description={describe(selected)} monogram={monogram(selected)} hue={hue(selected)} {docker} {shares} onclose={() => (selected = null)} />
			{/key}
		{/if}
	</div>
{/if}

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.title p {
		font-size: 0.875rem;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
	}
	.sources {
		overflow: hidden;
	}
	.sources-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 16px;
		padding: 12px 18px;
		border-bottom: 1px solid var(--panel-border);
		font-size: 0.8125rem;
	}
	.sources-head h2 {
		margin-right: auto;
	}
	.small {
		height: 2.25rem;
		padding: 0 0.75rem;
		font-size: 0.8125rem;
	}
	.source {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px 16px;
		padding: 12px 18px;
		border-bottom: 1px solid var(--panel-border);
		font-size: 0.875rem;
	}
	.source-name {
		flex: 1 1 280px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		font-weight: 500;
	}
	.url {
		font-size: 0.75rem;
		font-weight: 400;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.count {
		width: 5.5rem;
		font-size: 0.8125rem;
		text-align: right;
	}
	.when {
		width: 10rem;
		font-size: 0.75rem;
	}
	.when.failed {
		color: var(--warn-text);
	}
	.known > .muted {
		display: block;
		padding: 12px 18px 0;
		font-size: 0.8125rem;
	}
	.when.wide {
		width: auto;
		flex: 0 1 22rem;
	}
	.remove-space {
		width: 2.25rem;
	}
	.add {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 12px;
		padding: 16px 18px;
	}
	.add label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		flex: 0 1 240px;
		font-size: 0.8125rem;
	}
	.add label.grow {
		flex: 1 1 360px;
	}
	.add label > span {
		color: var(--muted);
	}
	.hint {
		flex: 1 1 100%;
		font-size: 0.8125rem;
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: 24px;
	}
	.catalog {
		flex: 1 1 560px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.find {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 12px;
	}
	.search {
		flex: 1 1 260px;
		max-width: 380px;
	}
	.from {
		flex: 0 1 260px;
	}
	.shown {
		font-size: 0.8125rem;
	}
	.categories {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.categories button {
		height: 2.25rem;
		padding: 0 14px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: 999px;
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		font-weight: 500;
		cursor: pointer;
	}
	.categories button[aria-pressed='true'] {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
	}
	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
		gap: 14px;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
		border-width: 2px;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		transition:
			border-color 0.15s ease,
			transform 0.15s ease;
	}
	.card:hover {
		transform: translateY(-1px);
	}
	.card[aria-pressed='true'] {
		border-color: var(--accent);
	}
	@media (prefers-reduced-motion: reduce) {
		.card {
			transition: none;
		}
		.card:hover {
			transform: none;
		}
	}
	.card-head {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.monogram {
		width: 48px;
		height: 48px;
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: var(--radius-ctl);
		color: var(--on-monogram);
		font-size: 1.0625rem;
		font-weight: 600;
	}
	.names {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.name {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.category,
	.source-label {
		font-size: 0.75rem;
	}
	.description {
		min-height: 2.4rem;
		font-size: 0.8125rem;
		line-height: 1.45;
		overflow-wrap: anywhere;
		/* Some stores write whole paragraphs. */
		display: -webkit-box;
		-webkit-box-orient: vertical;
		-webkit-line-clamp: 4;
		line-clamp: 4;
		overflow: hidden;
	}
	.card-foot {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		margin-top: auto;
	}
	.source-label {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.none {
		padding: 32px;
		border: 1px dashed var(--border);
		border-radius: var(--radius-tile);
		font-size: 0.875rem;
		text-align: center;
	}
</style>
