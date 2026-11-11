<script lang="ts">
	import { api } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import LinkDialog from './LinkDialog.svelte';
	import PluginDetails from './PluginDetails.svelte';
	import ProblemNotice from './ProblemNotice.svelte';
	import RemoveDialog from './RemoveDialog.svelte';
	import SourceRemoveDialog from './SourceRemoveDialog.svelte';
	import SourcesDialog from './SourcesDialog.svelte';
	import UpdateDialog from './UpdateDialog.svelte';
	import { badge, categoryKey, categoryOf, hue, monogram, type Status } from './plugins.ts';
	import { PluginsPage } from './plugins-page.svelte.ts';

	// State and actions live in plugins-page.svelte.ts.
	const pg = new PluginsPage();
	const status = $derived(pg.status);
	const current = $derived(pg.current);

	// PLAN 67-1: the menu bar of the desktop – the head's actions of the plugins.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({
		'appcenter.refresh': {
			run: () => void pg.refreshCatalog(true),
			enabled: () => !pg.busy && !pg.refreshing,
		},
		'appcenter.sources': {
			run: () => ((pg.error = null), (pg.sourcesOpen = true)),
			enabled: () => !pg.locked,
		},
		'appcenter.fromLink': { run: pg.openLink, enabled: () => !pg.locked && !!pg.volumes.length },
	});
</script>

<PageHead title={t('plugins.title')}>
	<p class="muted">{t('plugins.intro')} <a href="/apps">{t('nav.apps')}</a></p>
	<p class="muted">{t('plugins.catalogIntro')}</p>
	{#snippet actions()}
		<a class="btn" href="/plugins/cleanup">{t('cleanup.title')}</a>
		<button
			type="button"
			class="btn"
			disabled={pg.locked}
			onclick={() => ((pg.error = null), (pg.sourcesOpen = true))}>{t('plugins.sourceList')}</button
		>
		<button
			type="button"
			class="btn"
			disabled={pg.locked || !pg.volumes.length}
			onclick={pg.openLink}>{t('plugins.fromLink')}</button
		>
		<button
			type="button"
			class="btn"
			disabled={pg.busy || pg.refreshing}
			onclick={() => pg.refreshCatalog(true)}
			>{t(pg.refreshing ? 'plugins.catalogLoading' : 'plugins.catalogRefresh')}</button
		>
	{/snippet}
</PageHead>

{#if status?.catalog.state === 'development'}
	<p class="notice-warn" role="status">{t('plugins.catalogDevelopment')}</p>
{:else if status?.catalog.state === 'unavailable'}
	<p class="notice-warn" role="status">
		{t('plugins.catalogUnavailable', { version: status.catalog.release })}
	</p>
{:else if status?.catalog.state === 'error'}
	<div class="notice-warn" role="status">
		{t('plugins.catalogError')}
		{#if status.catalog.error}<details>
				<summary>{t('plugins.errorDetails')}</summary>
				<pre>{status.catalog.error}</pre>
			</details>{/if}
	</div>
{/if}

{#if pg.error && !pg.asking && !pg.removing && !pg.updating && !pg.linking && !pg.sourcesOpen}
	<ProblemNotice problem={pg.error} />
{/if}

{#if status && pg.recommendations.length > 0}
	<section class="tile panel recommended" aria-labelledby="recommended-title">
		<h2 class="tile-title" id="recommended-title">{t('plugins.recommendedTitle')}</h2>
		<p class="muted note">{t('plugins.recommendedIntro')}</p>
		<ul class="recommendations">
			{#each pg.recommendations as entry (entry.plugin)}
				{@const offered = pg.plugins.find((plugin) => plugin.name === entry.plugin)}
				<li class="recommendation">
					<div class="about">
						<span class="name-row"
							><span class="name">{offered?.title ?? entry.title}</span><span class="muted source"
								>{t(categoryKey[categoryOf(entry)])}</span
							></span
						>
						{#each entry.devices as device (device.key + (device.address ?? ''))}
							<span class="description"
								>{device.name}{#if device.address}<span class="muted mono">
										· {device.address}</span
									>{/if}</span
							>
						{/each}
						{#if !offered}<span class="muted note">{t('plugins.recommendedUnavailable')}</span>{/if}
					</div>
					<div class="row-actions">
						<button
							type="button"
							class="btn"
							disabled={pg.busy}
							onclick={() =>
								pg.run(() =>
									api.post<Status>(
										`/plugins/recommendations/${encodeURIComponent(entry.plugin)}/dismiss`,
										{},
									),
								)}>{t('plugins.recommendedDismiss')}</button
						>
						<button
							type="button"
							class="btn btn-primary"
							disabled={pg.busy || !offered}
							onclick={() => pg.showRecommended(entry.plugin)}
							>{t('plugins.recommendedShow')}</button
						>
					</div>
				</li>
			{/each}
		</ul>
	</section>
{/if}
{#if status?.dismissed}
	<p class="muted note">
		<button
			type="button"
			class="btn small"
			disabled={pg.busy}
			onclick={() => pg.run(() => api.delete<Status>('/plugins/recommendations/dismissed'))}
			>{t('plugins.recommendedRestore', { count: status.dismissed })}</button
		>
	</p>
{/if}

{#if status && pg.plugins.length === 0}
	<EmptyState
		title={t(pg.refreshing ? 'plugins.catalogLoading' : 'plugins.emptyTitle')}
		text={pg.refreshing ? undefined : [t('plugins.emptyText'), t('plugins.emptyOfficial')]}
	/>
{:else if status && current}
	<div class="layout">
		<div class="list">
			<div class="filters">
				<input
					class="field"
					type="search"
					placeholder={t('plugins.search')}
					aria-label={t('plugins.search')}
					bind:value={pg.query}
				/>
				<select class="field" aria-label={t('plugins.categoryAll')} bind:value={pg.area}>
					<option value="">{t('plugins.categoryAll')}</option>
					{#each pg.usedAreas as id (id)}<option value={id}>{t(categoryKey[id])}</option>{/each}
				</select>
			</div>
			{#each pg.groups as group (group.id)}
				<h2 class="group">{t(categoryKey[group.id])}</h2>
				{#each group.plugins as plugin (plugin.name)}
					{@const state = badge(plugin)}
					<button
						type="button"
						class="tile card"
						aria-pressed={plugin.name === current.name}
						onclick={() => pg.pick(plugin.name)}
					>
						<span class="monogram" style:background="hsl({hue(plugin)} 45% 42%)" aria-hidden="true"
							>{monogram(plugin)}</span
						>
						<span class="about">
							<span class="name-row"
								><span class="name">{plugin.title}</span><span class="muted source"
									>{t(
										plugin.local
											? 'plugins.driverLocal'
											: plugin.official
												? 'plugins.official'
												: 'plugins.community',
									)}{#if plugin.source}
										· {pg.sourceName(plugin.source)}{/if}</span
								></span
							>
							<span class="description">{pg.text(plugin.description)}</span>
						</span>
						{#if pg.fits(plugin)}<span class="chip ok">{t('plugins.fits')}</span>{/if}
						{#if plugin.update}<span class="chip">{t('plugins.updateChip')}</span>{/if}
						<span class="chip {state.kind}">{state.text}</span>
					</button>
				{/each}
			{:else}
				<p class="muted note">{t('plugins.noMatch')}</p>
			{/each}
		</div>

		<PluginDetails {pg} {status} {current} />
	</div>
	<!-- Style W4 (0.74): the figures of the list in the status bar. -->
	<StatusBar label={t('plugins.statusBar')}>
		<span>{t('plugins.statCount', { count: pg.plugins.length })}</span>
		<span>{t('plugins.statInstalled')} <b>{pg.plugins.filter((p) => p.installed).length}</b></span>
		<span class:warn={pg.plugins.some((p) => p.update)}
			>{t('plugins.statUpdates')} <b>{pg.plugins.filter((p) => p.update).length}</b></span
		>
		<span class="end"
			>{status.volume
				? t('plugins.where', { volume: status.volume })
				: t('plugins.whereNone')}</span
		>
	</StatusBar>
{/if}

{#if pg.removing}
	{@const target = pg.removing}
	<RemoveDialog
		title={target.title}
		busy={pg.busy}
		steps={pg.removingSteps}
		error={pg.error}
		onclose={() => (pg.removing = null)}
		onremove={() => pg.run(() => api.delete<Status>(`/plugins/${target.name}`), target.name)}
	/>
{/if}

{#if pg.linking}
	<LinkDialog
		bind:url={pg.linkURL}
		bind:volume={pg.volume}
		bind:activate={pg.activate}
		preview={pg.linkPreview}
		chooseVolume={!status?.volume}
		volumes={pg.volumes}
		busy={pg.busy}
		checking={pg.linkChecking}
		steps={pg.linkPreview ? pg.stepsFor(pg.linkPreview.name) : undefined}
		error={pg.error}
		onclose={() => (pg.linking = false)}
		oncheck={pg.checkLink}
		oninstall={pg.installLink}
		onedit={() => (pg.linkPreview = null)}
	/>
{/if}

{#if pg.sourcesOpen}
	<SourcesDialog
		sources={pg.sources}
		bind:url={pg.sourceURL}
		adding={pg.sourceAdding}
		error={pg.error}
		onclose={() => (pg.sourcesOpen = false)}
		onadd={pg.addSource}
		onremove={(source) => (pg.sourceRemoving = source)}
	/>
{/if}

{#if pg.sourceRemoving}
	{@const target = pg.sourceRemoving}
	<SourceRemoveDialog
		name={target.name}
		onclose={() => (pg.sourceRemoving = null)}
		onremove={() => pg.removeSource(target)}
	/>
{/if}

{#if pg.updating}
	{@const target = pg.updating}
	<UpdateDialog
		{target}
		busy={pg.busy}
		steps={pg.updatingSteps}
		error={pg.error}
		onclose={() => (pg.updating = null)}
		onupdate={() =>
			pg.run(
				() =>
					api.post<Status>(`/plugins/${target.name}/update`, {
						path: target.path,
						confirm: target.name,
					}),
				target.name,
			)}
	/>
{/if}

<style>
	.title {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.intro {
		max-width: 60rem;
		font-size: var(--text-md);
	}
	.empty {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: 32px;
		font-size: var(--text-md);
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: var(--space-5);
	}
	.list {
		flex: 1 1 520px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.card {
		display: flex;
		align-items: center;
		gap: 14px;
		padding: calc(var(--row-pad) + 2px) var(--tile-pad);
		border-width: 2px;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.recommended {
		margin-bottom: var(--space-4);
	}
	.recommendations {
		list-style: none;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
	}
	.recommendation {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-3) 0;
		border-top: 1px solid var(--panel-border);
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.filters input {
		flex: 1 1 14rem;
	}
	.group {
		margin-top: var(--space-2);
		font-size: var(--text-sm);
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.card[aria-pressed='true'] {
		border-color: var(--accent);
	}
	.monogram {
		width: 44px;
		height: 44px;
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: var(--radius-ctl);
		color: var(--on-monogram);
		font-weight: 600;
	}
	.about {
		flex: 1 1 auto;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.name-row {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-1) var(--space-2-5);
	}
	.name {
		font-size: var(--text-md);
		font-weight: 600;
	}
	.source {
		font-size: var(--text-sm);
	}
	.description {
		font-size: var(--text-base);
		overflow-wrap: anywhere;
	}
	.note {
		font-size: var(--text-base);
	}
	h2 {
		font-size: var(--text-lg);
		font-weight: 600;
	}
	ul {
		margin: 0;
		padding-left: 1.25rem;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.text {
		font-size: var(--text-md);
	}
	pre {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
