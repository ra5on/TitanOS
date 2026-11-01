<script lang="ts">
	import AppIcon from '#lib/components/AppIcon.svelte';
	import {
		api,
		ApiError,
		type AppSource,
		type AppStore,
		type CatalogApp,
		type DockerStatus,
		type Share,
	} from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { localize } from '#lib/compose/template.ts';
	import { errorText, i18n, t, tOr } from '#lib/i18n/index.svelte.ts';

	// The order the categories are offered in; what a source calls differently follows.
	const CATEGORIES = [
		'media',
		'photos',
		'backup',
		'security',
		'productivity',
		'development',
		'network',
		'home',
		'ai',
		'finance',
		'social',
	];

	// Stores of others that TitanOS can read. They are offered, never added
	// by themselves: their apps are not checked by TitanOS.
	const KNOWN = [
		{
			name: 'CasaOS App Store',
			url: 'https://github.com/IceWhaleTech/CasaOS-AppStore',
			note: 'apps.known.casaos',
		},
		{
			name: 'BigBear CasaOS',
			url: 'https://github.com/bigbeartechworld/big-bear-casaos',
			note: 'apps.known.bigbear',
		},
	] as const;

	let store = $state<AppStore | null>(null);
	let docker = $state<DockerStatus | null>(null);
	let shares = $state<Share[]>([]);
	let error = $state('');
	let search = $state('');
	let category = $state('all');
	let from = $state('all');
	let sourcesOpen = $state(false);
	let draft = $state({ url: '', name: '' });
	let sourceError = $state('');
	let busy = $state(false);
	let onlyInstalled = $state(false);
	// The catalog can hold hundreds of apps; they are shown in pages of 60.
	const PAGE = 60;
	let limit = $state(PAGE);
	$effect(() => {
		void [search, category, from, onlyInstalled];
		limit = PAGE;
	});
	const code = (err: unknown) => (err instanceof ApiError ? err.code : 'internal');

	$effect(() => {
		api.get<AppStore>('/apps').then(
			(res) => (store = res),
			(err) => (error = errorText(code(err))),
		);
		api.get<DockerStatus>('/docker').then(
			(res) => (docker = res),
			() => {},
		);
		api.get<Share[]>('/shares').then(
			(res) => (shares = res),
			() => {},
		);
	});

	const apps = $derived(store?.apps ?? []);
	const sources = $derived(store?.sources ?? []);
	const suggestions = $derived(
		KNOWN.filter((known) => !sources.some((source) => source.url === known.url)),
	);
	const sourceName = (id: string) => sources.find((source) => source.id === id)?.name ?? id;
	const categoryOf = (app: CatalogApp) => app.category || 'other';
	const categoryLabel = (key: string) =>
		tOr(
			`apps.category.${key}`,
			key.replace(/^./, (c) => c.toUpperCase()),
		);
	const categories = $derived.by(() => {
		const present = new Set(apps.map(categoryOf));
		const known = CATEGORIES.filter((key) => present.has(key));
		const others = [...present]
			.filter((key) => !CATEGORIES.includes(key) && key !== 'other')
			.sort();
		return ['all', ...known, ...others, ...(present.has('other') ? ['other'] : [])];
	});
	const describe = (app: CatalogApp) => localize(app.description, i18n.locale) || app.image;
	const installed = (app: CatalogApp) => docker?.apps.includes(app.id) ?? false;
	// Everything but the category: what the category buttons count.
	const matching = $derived.by(() => {
		const query = search.trim().toLowerCase();
		return apps.filter(
			(app) =>
				(from === 'all' || app.source === from) &&
				(!onlyInstalled || installed(app)) &&
				(query === '' || `${app.name} ${app.id} ${describe(app)}`.toLowerCase().includes(query)),
		);
	});
	const categoryCounts = $derived.by(() => {
		const counts: Record<string, number> = { all: matching.length };
		for (const app of matching) counts[categoryOf(app)] = (counts[categoryOf(app)] ?? 0) + 1;
		return counts;
	});
	const visible = $derived(
		matching.filter((app) => category === 'all' || categoryOf(app) === category),
	);
	const installedCount = $derived(apps.filter(installed).length);
	const ready = $derived(!!docker?.settings.enabled && docker.running);

	function checked(source: AppSource): string {
		if (source.error) return tOr(`apps.sourceError.${source.error}`, source.error);
		const time = new Date(source.updated).getTime();
		if (!time || time < 0) return t('apps.neverChecked');
		if (source.builtin) return t('apps.builtin');
		return t('apps.checked', {
			time: new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'short', timeStyle: 'short' }).format(
				time,
			),
		});
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
		if (
			await change(() => api.post<AppStore>('/apps/sources', { url: draft.url, name: draft.name }))
		)
			draft = { url: '', name: '' };
	}

	async function removeSource(source: AppSource) {
		if (from === source.id) from = 'all';
		await change(() => api.delete<AppStore>(`/apps/sources/${source.id}`));
	}
</script>

<PageHead title={t('apps.title')}>
	{#if store}<p class="muted">
			{t(sources.length === 1 ? 'apps.summaryOne' : 'apps.summary', {
				apps: apps.length,
				sources: sources.length,
			})}
		</p>{/if}
	{#snippet actions()}
		<button
			type="button"
			class="btn"
			aria-expanded={sourcesOpen}
			onclick={() => (sourcesOpen = !sourcesOpen)}>{t('apps.sources')}</button
		>
		<a class="btn" href="/docker#import">{t('apps.import')}</a>
	{/snippet}
</PageHead>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if store && sources[0]?.builtin && sources[0].count === 0}
	<p class="notice-warn" role="status">
		{sources[0].error ? t('apps.officialOffline') : t('apps.officialPending')}
	</p>
{/if}
{#if docker && !ready}
	<p class="notice-warn" role="status">
		{t('apps.dockerOff')} <a href="/docker">{t('apps.dockerOpen')}</a>
	</p>
{/if}

{#if sourcesOpen && store}
	<section class="tile sources" aria-labelledby="sources-title">
		<div class="sources-head">
			<h2 class="tile-title" id="sources-title">{t('apps.sources')}</h2>
			<span class="muted">{t('apps.sourcesNote')}</span>
			<button
				type="button"
				class="btn small"
				onclick={() => change(() => api.post<AppStore>('/apps/refresh'))}
				disabled={busy}>{t('apps.refresh')}</button
			>
		</div>
		{#each sources as source (source.id)}
			<div class="source">
				<span class="dot" class:ok={!source.error} class:warn={!!source.error} aria-hidden="true"
				></span>
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
					<button
						type="button"
						class="btn small"
						aria-label={t('apps.sourceRemove', { name: source.name })}
						onclick={() => removeSource(source)}
						disabled={busy}>✕</button
					>
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
						<button
							type="button"
							class="btn small"
							onclick={() =>
								change(() =>
									api.post<AppStore>('/apps/sources', { url: known.url, name: known.name }),
								)}
							disabled={busy}>{t('apps.knownAdd')}</button
						>
					</div>
				{/each}
			</div>
		{/if}
		<form class="add" onsubmit={addSource}>
			<label class="grow"
				><span>{t('apps.sourceUrl')}</span>
				<input
					class="field mono"
					type="url"
					bind:value={draft.url}
					placeholder="https://github.com/…"
					spellcheck="false"
					required
				/>
			</label>
			<label
				><span>{t('apps.sourceName')}</span><input
					class="field"
					type="text"
					bind:value={draft.name}
					maxlength="80"
				/></label
			>
			<button type="submit" class="btn btn-primary" disabled={busy}
				>{t(busy ? 'apps.sourceAdding' : 'apps.sourceAdd')}</button
			>
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
				<input
					class="field search"
					type="search"
					bind:value={search}
					placeholder={t('apps.search')}
					aria-label={t('apps.search')}
				/>
				{#if sources.length > 1}
					<select class="field from" bind:value={from} aria-label={t('apps.fromSource')}>
						<option value="all">{t('apps.allSources')}</option>
						{#each sources as source (source.id)}<option value={source.id}
								>{source.name} ({source.count})</option
							>{/each}
					</select>
				{/if}
				<button
					type="button"
					class="toggle"
					aria-pressed={onlyInstalled}
					onclick={() => (onlyInstalled = !onlyInstalled)}
					>{t('apps.filterInstalled')} <span class="num">{installedCount}</span></button
				>
				<span class="muted shown">{t('apps.shown', { count: visible.length })}</span>
			</div>
			<div class="categories" role="group" aria-label={t('apps.categories')}>
				{#each categories as key (key)}
					<button type="button" aria-pressed={category === key} onclick={() => (category = key)}
						>{categoryLabel(key)} <span class="num">{categoryCounts[key] ?? 0}</span></button
					>
				{/each}
			</div>
			<div class="cards">
				{#each visible.slice(0, limit) as app (`${app.source}/${app.id}`)}
					<a
						class="tile card"
						href="/apps/install/{encodeURIComponent(app.source)}/{encodeURIComponent(app.id)}"
					>
						<span class="card-head">
							<span class="monogram" aria-hidden="true"
								><AppIcon src={app.icon} name={app.name} /></span
							>
							<span class="names"
								><span class="name">{app.name}</span><span class="muted category"
									>{categoryLabel(categoryOf(app))}</span
								></span
							>
						</span>
						<span class="description">{describe(app)}</span>
						<span class="card-foot">
							<span class="muted source-label">{sourceName(app.source)}</span>
							<span class="chip" class:ok={installed(app)}
								>{t(installed(app) ? 'apps.installed' : 'apps.install')}</span
							>
						</span>
					</a>
				{/each}
			</div>
			{#if visible.length > limit}
				<div class="more">
					<button type="button" class="btn" onclick={() => (limit += PAGE)}
						>{t('apps.showMore', { count: Math.min(PAGE, visible.length - limit) })}</button
					>
					<span class="muted">{t('apps.showing', { shown: limit, total: visible.length })}</span>
				</div>
			{/if}
			{#if visible.length === 0}<EmptyState inline title={t('apps.none')} />{/if}
		</div>
	</div>
{/if}

<style>
	.more {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 12px;
		font-size: var(--text-base);
	}
	.num {
		margin-left: 4px;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		opacity: 0.75;
	}
	.toggle {
		height: 2.25rem;
		padding: 0 14px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		font-weight: 500;
		cursor: pointer;
	}
	.toggle[aria-pressed='true'] {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
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
		font-size: var(--text-base);
	}
	.sources-head h2 {
		margin-right: auto;
	}
	.small {
		height: 2.25rem;
		padding: 0 0.75rem;
		font-size: var(--text-base);
	}
	.source {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px 16px;
		padding: 12px 18px;
		border-bottom: 1px solid var(--panel-border);
		font-size: var(--text-md);
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
		font-size: var(--text-sm);
		font-weight: 400;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.count {
		width: 5.5rem;
		font-size: var(--text-base);
		text-align: right;
	}
	.when {
		width: 10rem;
		font-size: var(--text-sm);
	}
	.when.failed {
		color: var(--warn-text);
	}
	.known > .muted {
		display: block;
		padding: 12px 18px 0;
		font-size: var(--text-base);
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
		font-size: var(--text-base);
	}
	.add label.grow {
		flex: 1 1 360px;
	}
	.add label > span {
		color: var(--muted);
	}
	.hint {
		flex: 1 1 100%;
		font-size: var(--text-base);
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
		font-size: var(--text-base);
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
		font-size: var(--text-base);
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
		text-decoration: none;
		cursor: pointer;
		transition:
			border-color 0.15s ease,
			transform 0.15s ease;
	}
	.card:hover {
		transform: translateY(-1px);
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
		color: var(--text);
		font-size: var(--text-lg);
		font-weight: 600;
	}
	.names {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.name {
		font-size: var(--text-md);
		font-weight: 600;
	}
	.category,
	.source-label {
		font-size: var(--text-sm);
	}
	.description {
		min-height: 2.4rem;
		font-size: var(--text-base);
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
</style>
