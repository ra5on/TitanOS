<script lang="ts">
	// PLAN 66: programs that run directly in the browser, above the Docker catalog.
	import { tick } from 'svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import BrowserApps from '#lib/webapps/BrowserApps.svelte';
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
	import Icon from '#lib/components/Icon.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
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
		'games',
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
	const phone = $derived(phoneView());
	let filtersOpen = $state(false);
	function showTab(tab: 'catalog' | 'installed' | 'sources') {
		sourcesOpen = tab === 'sources';
		onlyInstalled = tab === 'installed';
	}
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

	// PLAN 67-1: the menu bar of the desktop – catalogue, sources and filters.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({
		'appcenter.refresh': {
			run: () => void change(() => api.post<AppStore>('/apps/refresh')),
			enabled: () => !busy && !!store,
		},
		'appcenter.sources': { run: () => (sourcesOpen = !sourcesOpen), checked: () => sourcesOpen },
		'appcenter.filter.all': { run: () => showTab('catalog'), checked: () => !onlyInstalled },
		'appcenter.filter.installed': {
			run: () => showTab('installed'),
			checked: () => onlyInstalled,
		},
		'appcenter.webapps': async () => {
			if (phone) {
				showTab('catalog');
				search = '';
				category = 'all';
				from = 'all';
			}
			await tick();
			document.querySelector('.browser-apps')?.scrollIntoView({ block: 'start' });
		},
		'appcenter.categories': {
			items: () =>
				categories.map((key) => ({
					id: key,
					text: key === 'all' ? t('menubar.appcenter.all') : categoryLabel(key),
					checked: category === key,
				})),
			run: (key) => {
				if (key && categories.includes(key)) category = key;
			},
		},
		'appcenter.source': {
			items: () =>
				sources.length > 1
					? [
							{ id: 'all', text: t('apps.allSources'), checked: from === 'all' },
							...sources.map((s) => ({ id: s.id, text: s.name, checked: from === s.id })),
						]
					: [],
			run: (id) => {
				if (id === 'all' || sources.some((s) => s.id === id)) from = id ?? 'all';
			},
		},
	});
</script>

<PageHead title={t(phone ? 'desktop.app.appcenter' : 'apps.title')}>
	{#if store && !phone}<p class="muted">
			{t(sources.length === 1 ? 'apps.summaryOne' : 'apps.summary', {
				apps: apps.length,
				sources: sources.length,
			})}
		</p>{/if}
	{#snippet actions()}
		{#if !phone}<button
				type="button"
				class="btn"
				aria-expanded={sourcesOpen}
				onclick={() => (sourcesOpen = !sourcesOpen)}>{t('apps.sources')}</button
			>
			<a class="btn" href="/docker#import">{t('apps.import')}</a>{/if}
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

{#if phone}
	<div class="nord-segments" role="group" aria-label={t('apps.title')}>
		<button aria-pressed={!sourcesOpen && !onlyInstalled} onclick={() => showTab('catalog')}
			>{t('nord.apps.catalog')}</button
		>
		<button aria-pressed={!sourcesOpen && onlyInstalled} onclick={() => showTab('installed')}
			>{t('apps.installed')}</button
		>
		<button aria-pressed={sourcesOpen} onclick={() => showTab('sources')}
			>{t('plugins.sources')}</button
		>
	</div>
{:else}<BrowserApps />{/if}

{#if sourcesOpen && store}
	<!-- PLAN 78-5: the sources are settings: a plain heading, each source a
	     row, the known stores and the form for a new one as groups of rows. -->
	<SettingGroup title={t('apps.sources')} id="sources" desc={t('apps.sourcesNote')}>
		{#snippet tools()}
			<button
				type="button"
				class="btn"
				onclick={() => change(() => api.post<AppStore>('/apps/refresh'))}
				disabled={busy}>{t('apps.refresh')}</button
			>
		{/snippet}
		{#each sources as source (source.id)}
			<SettingRow icon="box" title={source.name}>
				{#snippet description()}
					{#if source.url}<span class="mono url">{source.url}</span>{/if}
					<span class="when" class:failed={!!source.error}
						><span
							class="dot"
							class:ok={!source.error}
							class:warn={!!source.error}
							aria-hidden="true"
						></span>{checked(source)}</span
					>
				{/snippet}
				{#snippet control()}
					{#if source.format}<span class="chip">{t(`apps.format.${source.format}`)}</span>{/if}
					<span class="mono count">{t('apps.count', { count: source.count })}</span>
					{#if !source.builtin}
						<button
							type="button"
							class="btn"
							aria-label={t('apps.sourceRemove', { name: source.name })}
							title={t('apps.sourceRemove', { name: source.name })}
							onclick={() => removeSource(source)}
							disabled={busy}><Icon name="close" size={15} /></button
						>
					{/if}
				{/snippet}
			</SettingRow>
		{/each}
	</SettingGroup>
	{#if suggestions.length > 0}
		<SettingGroup title={t('apps.knownTitle')} id="sources-known">
			{#each suggestions as known (known.url)}
				<SettingRow icon="globe" title={known.name}>
					{#snippet description()}
						<span class="mono url">{known.url}</span>
						<span class="when">{t(known.note)}</span>
					{/snippet}
					{#snippet control()}
						<button
							type="button"
							class="btn"
							aria-label="{t('apps.knownAdd')}: {known.name}"
							onclick={() =>
								change(() =>
									api.post<AppStore>('/apps/sources', { url: known.url, name: known.name }),
								)}
							disabled={busy}>{t('apps.knownAdd')}</button
						>
					{/snippet}
				</SettingRow>
			{/each}
		</SettingGroup>
	{/if}
	<form class="add" onsubmit={addSource}>
		<SettingGroup title={t('apps.sourceAdd')} id="sources-add">
			<SettingRow
				icon="plus"
				title={t('apps.sourceUrl')}
				for="source-url"
				desc={t('apps.sourceHint')}
			>
				{#snippet control()}<input
						id="source-url"
						class="field mono"
						type="url"
						bind:value={draft.url}
						placeholder="https://github.com/…"
						spellcheck="false"
						required
					/>{/snippet}
			</SettingRow>
			<SettingRow icon="edit" title={t('apps.sourceName')} for="source-name">
				{#snippet control()}<input
						id="source-name"
						class="field"
						type="text"
						bind:value={draft.name}
						maxlength="80"
					/>{/snippet}
			</SettingRow>
			<p class="notice-warn">{t('apps.sourceWarning')}</p>
			{#if sourceError}<p class="notice-error" role="alert">{sourceError}</p>{/if}
			<div class="row-actions">
				<button type="submit" class="btn btn-primary" disabled={busy}
					>{t(busy ? 'apps.sourceAdding' : 'apps.sourceAdd')}</button
				>
			</div>
		</SettingGroup>
	</form>
{/if}

{#if store && (!phone || !sourcesOpen)}
	<div class="layout" class:mobile-catalog={phone}>
		<div class="catalog">
			<div class="find" role="search" aria-label={t('apps.title')}>
				<input
					class="field search"
					type="search"
					bind:value={search}
					placeholder={t('apps.search')}
					aria-label={t('apps.search')}
				/>
				{#if phone}<button
						class="btn filter-btn"
						aria-label={t('apps.categories')}
						aria-expanded={filtersOpen}
						onclick={() => (filtersOpen = !filtersOpen)}><Icon name="filter" size={19} /></button
					>{/if}
				{#if sources.length > 1 && (!phone || filtersOpen)}
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
					hidden={phone}
					aria-pressed={onlyInstalled}
					onclick={() => (onlyInstalled = !onlyInstalled)}
					>{t('apps.filterInstalled')} <span class="num">{installedCount}</span></button
				>
				<span class="muted shown" hidden={phone}>{t('apps.shown', { count: visible.length })}</span>
			</div>
			<div
				class="categories"
				hidden={phone && !filtersOpen}
				role="group"
				aria-label={t('apps.categories')}
			>
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
						class:feature={phone &&
							!onlyInstalled &&
							!search &&
							category === 'all' &&
							from === 'all' &&
							app === visible[0]}
						href={(phone || onlyInstalled) && installed(app)
							? '/docker/apps/' + encodeURIComponent(app.id)
							: '/apps/install/' +
								encodeURIComponent(app.source) +
								'/' +
								encodeURIComponent(app.id)}
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
								>{t(
									installed(app)
										? !phone && onlyInstalled
											? 'docker.edit'
											: 'apps.installed'
										: 'apps.install',
								)}</span
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
			{#if visible.length === 0 && !phone}<EmptyState inline title={t('apps.none')} />{/if}
		</div>
	</div>
	<!-- Style W4 (0.74): the figures of the catalog in the status bar. -->
	<StatusBar label={t('apps.statusBar')}>
		<span>{t('apps.statApps', { count: apps.length })}</span>
		<span>{t('apps.installed')}: <b>{installedCount}</b></span>
		<span>{t('apps.statShown')} <b>{visible.length}</b></span>
		<span class="end">{t('apps.statSources', { count: sources.length })}</span>
	</StatusBar>
{/if}

{#if phone && !sourcesOpen}<BrowserApps
		{onlyInstalled}
		query={search}
		include={category === 'all' && from === 'all'}
	/>{/if}
{#if phone && sourcesOpen}<a class="btn" href="/docker#import">{t('apps.import')}</a>{/if}
{#if phone && !sourcesOpen && store && visible.length === 0}<p class="muted">
		{t('nord.apps.noContainers')}
	</p>{/if}

<style>
	.more {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
		font-size: var(--text-base);
	}
	.num {
		margin-left: var(--space-1);
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
	.url,
	.when {
		display: block;
		overflow-wrap: anywhere;
	}
	.when .dot {
		margin-right: var(--space-1-5);
	}
	.when.failed {
		color: var(--warn-text);
	}
	.count {
		font-size: var(--text-base);
	}
	.add {
		display: contents;
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: var(--space-5);
	}
	.catalog {
		flex: 1 1 560px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	.find {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
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
		gap: var(--space-1-5);
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
		gap: var(--space-3);
		padding: var(--space-4);
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
		gap: var(--space-3);
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
		/* Some stores write whole paragraphs: they are shown whole, never cut off. */
		overflow-wrap: anywhere;
	}
	.card-foot {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin-top: auto;
	}
	.source-label {
		overflow-wrap: anywhere;
	}
	[hidden] {
		display: none !important;
	}
	.mobile-catalog .find {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.mobile-catalog .search {
		flex: 1 1 65%;
		min-width: 0;
		min-height: 44px;
	}
	.filter-btn {
		height: 44px;
		width: 44px;
		padding: 0;
	}
	.mobile-catalog .from {
		flex: 1 1 100%;
		min-height: 44px;
	}
	.mobile-catalog .categories {
		flex-wrap: wrap;
		gap: 5px;
	}
	.mobile-catalog .categories button {
		min-height: 44px;
	}
	.mobile-catalog .cards {
		display: flex;
		flex-direction: column;
		gap: 0;
		border-radius: var(--nord-radius);
		border: 1px solid var(--nord-line);
		overflow: hidden;
		background: var(--nord-surface);
	}
	.mobile-catalog .cards:empty {
		display: none;
	}
	.mobile-catalog .card {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		align-items: center;
		gap: 6px;
		min-height: 64px;
		padding: 10px 12px;
		border: 0;
		border-radius: 0;
		box-shadow: none;
		background: transparent;
		backdrop-filter: none;
	}
	.mobile-catalog .card + .card {
		border-top: 1px solid var(--nord-line);
	}
	.mobile-catalog .card-foot {
		padding: 0;
		min-width: 0;
	}
	.mobile-catalog .source-label,
	.mobile-catalog .description {
		display: none;
	}
	.mobile-catalog .feature {
		min-height: 106px;
		align-content: end;
		background:
			linear-gradient(140deg, color-mix(in srgb, var(--accent) 18%, transparent), transparent 70%),
			linear-gradient(0deg, var(--surface), transparent);
		padding: 14px;
	}
	.mobile-catalog .feature .description {
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
		grid-column: 1/-1;
		margin: 0;
	}
	.mobile-catalog .feature .name {
		font-size: var(--text-lg);
	}
</style>
