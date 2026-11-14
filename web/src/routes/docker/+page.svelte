<script lang="ts">
	// Docker › Container (PLAN 67, owner's decision of 7 Nov 2026): one table
	// of all apps – a Compose stack is one row – with state as text, CPU, RAM,
	// ports and updates; Start/Stop and a ⋯ menu per row, the app's own page
	// on a click. Above it the filters and four figures. The earlier card and
	// tile views are gone; the stored docker preference is still read (order
	// and main services of stacks).
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api } from '#lib/api.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import AppTable from './AppTable.svelte';
	import DockerDialogs from './DockerDialogs.svelte';
	import { DockerPage } from './docker-page.svelte.ts';
	import PageMenu from './PageMenu.svelte';

	// State and actions live in docker-page.svelte.ts.
	const p = new DockerPage();

	// PLAN 67-1: the menu bar of the desktop runs the same actions as the toolbar.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	const ready = () => !!p.status?.running && !p.globalBusy;
	registerMenuCommands({
		// PLAN 87: pulling the list down on the phone (PhoneGestures).
		'docker.refresh': () => void p.load(),
		'docker.import': { run: () => p.open('import'), enabled: () => !!p.status?.running },
		'docker.startAll': {
			run: p.startAll,
			enabled: () => ready() && p.runCount < p.containers.length,
		},
		'docker.stopAll': {
			run: () => (p.confirmStopAll = true),
			enabled: () => ready() && p.runCount > 0,
		},
		'docker.checkUpdates': {
			run: p.checkUpdates,
			enabled: () => !!p.status?.running && !p.globalBusy,
		},
		'docker.updateAll': { run: p.askUpdates, enabled: () => !!p.updatable && !p.globalBusy },
		...Object.fromEntries(
			(['all', 'running', 'stopped', 'updates'] as const).map((f) => [
				'docker.filter.' + f,
				{ run: () => (p.filter = f), checked: () => p.filter === f },
			]),
		),
	});
</script>

{#snippet searchBox()}
	<span class="search-box"
		><Icon name="search" size={15} /><input
			class="field search"
			type="search"
			bind:value={p.search}
			placeholder={t('docker.search')}
			aria-label={t('docker.search')}
		/></span
	>
{/snippet}

{#snippet filters()}
	<div class="segmented" role="group" aria-label={t('docker.filter')}>
		{#each p.FILTERS as [id, label] (id)}
			<button type="button" aria-pressed={p.filter === id} onclick={() => (p.filter = id)}
				>{t(label)} <b class="mono">{p.counts[id]}</b></button
			>
		{/each}
	</div>
{/snippet}

<PageHead title={t('docker.title')}>
	{#snippet actions()}
		{#if p.status?.running && p.containers.length > 0 && !p.phone}
			{@render searchBox()}
			{#if p.roomy}{@render filters()}{/if}
		{/if}
		{#if p.status?.settings.enabled}
			<span class="split">
				<a class="btn btn-primary" href="/apps"
					><Icon name="plus" size={15} />{t('docker.installApp')}</a
				><button
					type="button"
					class="btn btn-primary"
					aria-label={t('docker.installMenu')}
					title={t('docker.installMenu')}
					aria-haspopup="menu"
					aria-expanded={p.pageMenu === 'install'}
					onpointerdown={() => (p.pageMenuWasOpen = p.pageMenu === 'install')}
					onclick={(event) => p.openPageMenu('install', event.currentTarget)}
					><Icon name="chevronDown" size={15} /></button
				>
			</span>
		{/if}
		<button
			type="button"
			class="btn icon-btn"
			aria-label={t('docker.table.more')}
			title={t('docker.table.more')}
			aria-haspopup="menu"
			aria-expanded={p.pageMenu === 'more'}
			onpointerdown={() => (p.pageMenuWasOpen = p.pageMenu === 'more')}
			onclick={(event) => p.openPageMenu('more', event.currentTarget)}
			><Icon name="more" size={17} /></button
		>
	{/snippet}
</PageHead>

{#if p.phone && p.status?.running && p.containers.length > 0}
	<!-- PLAN 64: on the phone search and filters get rows of their own under the tool bar. -->
	<div class="phone-tools">{@render searchBox()}{@render filters()}</div>
{:else if !p.roomy && p.status?.running && p.containers.length > 0}
	<div class="phone-tools">{@render filters()}</div>
{/if}

<PageMenu {p} />

{#if p.error}
	<div class="notice-error" role="alert">
		<div class="error-line">
			<span>{p.error.text}</span>{#if p.failedAction}{@const retry = p.failedAction}<button
					type="button"
					class="btn"
					disabled={p.busyKeys.has(retry.key)}
					onclick={() => p.run(retry.key, retry.action, retry.label)}>{t('common.retry')}</button
				>{/if}
		</div>
		{#if p.error.detail}<pre>{p.error.detail}</pre>{/if}
	</div>
{/if}

{#if p.refreshError}<div class="notice-warn" role="alert">
		<span
			>{p.refreshError}
			{p.lastRefresh
				? t('docker.stale', { time: new Date(p.lastRefresh).toLocaleTimeString() })
				: t('docker.noCurrentData')}</span
		><button class="btn" disabled={p.refreshing} onclick={p.load}>{t('common.retry')}</button>
	</div>{/if}
{#if !p.status && !p.refreshError}<p class="muted loading" role="status">
		{t('common.loading')}
	</p>{/if}
{#if p.status && !p.status.settings.enabled}
	<!-- PLAN 78-5: setting Docker up is a plain heading with its one setting
	     as a row; the button below it switches Docker on. -->
	<SettingGroup title={t('docker.setupTitle')} id="docker-setup" desc={t('docker.setupText')}>
		{#if p.volumes.length === 0}
			<p class="notice-warn">{t('docker.setupNeedVolume')}</p>
		{:else}
			<SettingRow icon="disk" title={t('docker.volume')} for="docker-volume">
				{#snippet control()}<select id="docker-volume" class="field" bind:value={p.draft.volume}
						>{#each p.volumes as volume (volume)}<option value={volume}>{volume}</option
							>{/each}</select
					>{/snippet}
			</SettingRow>
			<div class="row-actions setup-actions">
				<button type="button" class="btn btn-primary" onclick={p.enable} disabled={p.globalBusy}
					>{t('docker.enable')}</button
				>
			</div>
		{/if}
	</SettingGroup>
{:else if p.status && !p.status.running}
	<p class="notice-warn" role="status">
		{p.volumes.includes(p.status.settings.volume) &&
		storage.status?.volumes.find((volume) => volume.name === p.status?.settings.volume)?.mounted
			? t('docker.starting')
			: t('docker.waitingVolume', { volume: p.status.settings.volume })}
	</p>
{:else if p.status}
	{#each p.jobs as [app, job] (app)}
		<div class={job.state === 'failed' ? 'notice-error' : 'notice-warn'} role="status">
			{t(job.state === 'failed' ? 'docker.jobFailed' : 'docker.jobRunning', { app })}
			{#if job.state === 'failed' && job.output}<pre>{job.output}</pre>{/if}
			{#if job.state === 'failed'}
				<div class="buttons job-actions">
					<button
						type="button"
						class="btn"
						disabled={p.busyKeys.has(p.appKey(app))}
						onclick={() => p.run(p.appKey(app), () => api.post(`/docker/apps/${app}/update`), app)}
						>{t('common.retry')}</button
					>
					<a class="btn" href="/docker/apps/{app}">{t('docker.edit')}</a>
					<button
						type="button"
						class="btn btn-danger-outline"
						disabled={p.busyKeys.has(p.appKey(app))}
						onclick={() => p.askRemoveApp(app)}>{t('docker.remove')}</button
					>
					<button
						type="button"
						class="btn"
						disabled={p.busyKeys.has(p.appKey(app))}
						onclick={() => p.run(p.appKey(app), () => api.delete(`/docker/apps/${app}/job`), app)}
						>{t('docker.jobDismiss')}</button
					>
				</div>
			{/if}
		</div>
	{/each}

	{#each p.idleApps as app (app)}
		<div class="notice-warn" role="status">
			{t('docker.appIdle', { app, path: `/config/apps/${app}/compose.yaml` })}
			<div class="buttons job-actions">
				<button
					type="button"
					class="btn"
					disabled={p.busyKeys.has(p.appKey(app))}
					onclick={() => p.run(p.appKey(app), () => api.post(`/docker/apps/${app}/update`), app)}
					>{t('docker.appStart')}</button
				>
				<a class="btn" href="/docker/apps/{app}">{t('docker.edit')}</a>
				<button
					type="button"
					class="btn btn-danger-outline"
					disabled={p.busyKeys.has(p.appKey(app))}
					onclick={() => p.askRemoveApp(app)}>{t('docker.remove')}</button
				>
			</div>
		</div>
	{/each}

	{#if p.containers.length > 0}
		{#if p.shownGroups.length > 0}
			<AppTable {p} />
		{:else}
			<EmptyState title={t('docker.noMatch')} />
		{/if}
		<StatusBar label={t('docker.table.statusBar')}>
			<span>{t('docker.table.statApps', { count: p.counts.all })}</span>
			<span
				>{t('docker.table.statRunning')}
				<b
					>{t('docker.table.statRunningValue', {
						running: p.counts.running,
						total: p.counts.all,
					})}</b
				></span
			>
			<span>{t('docker.cpu')} <b>{formatPercent(p.totalCpu)}</b></span>
			<span>{t('docker.table.statRam')} <b>{formatBytes(p.totalRam)}</b></span>
			<span class:warn={p.counts.updates > 0}
				>{t('docker.table.statUpdates')}
				<b
					>{p.counts.updates
						? t('docker.table.statUpdatesValue', { count: p.counts.updates })
						: t('docker.table.statUpdatesNone')}</b
				></span
			>
			<span class="end">{t('docker.onVolume', { volume: p.status.settings.volume })}</span>
			<span
				>{p.checked}<button
					type="button"
					aria-label={t('docker.checkUpdates')}
					title={t('docker.checkUpdates')}
					onclick={p.checkUpdates}
					disabled={p.globalBusy}><Icon name="refresh" size={13} /></button
				></span
			>
			<span>{t('docker.version', { version: p.status.version })}</span>
		</StatusBar>
	{:else if p.jobs.length === 0 && p.idleApps.length === 0}
		<EmptyState title={t('docker.empty')} />
	{/if}
{/if}

<DockerDialogs {p} />

<style>
	.notice-warn {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2-5);
	}
	.job-actions {
		margin-top: var(--space-2-5);
	}
	.setup-actions {
		margin-top: var(--space-2-5);
	}
	.search-box {
		position: relative;
		display: inline-flex;
		align-items: center;
		width: 15rem;
		max-width: 100%;
		color: var(--muted);
	}
	.search-box :global(svg) {
		position: absolute;
		left: 10px;
		pointer-events: none;
	}
	.search {
		width: 100%;
		padding-left: 32px;
	}
	.icon-btn {
		width: var(--control-height);
		padding: 0;
	}
	.split {
		display: inline-flex;
	}
	.split > :first-child {
		border-top-right-radius: 0;
		border-bottom-right-radius: 0;
	}
	.split > :last-child {
		width: 2.25rem;
		padding: 0;
		border-top-left-radius: 0;
		border-bottom-left-radius: 0;
		border-left: 1px solid color-mix(in srgb, var(--on-accent) 25%, transparent);
	}
	/* The filters: one segmented control with the number of apps in each. */
	.segmented {
		display: flex;
		flex: none;
		min-width: 0;
		max-width: 100%;
		overflow-x: auto;
		scrollbar-width: thin;
		gap: 2px;
		padding: 3px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segmented button {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		height: 1.875rem;
		padding: 0 var(--space-2-5);
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--muted);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.segmented button b {
		font-weight: 500;
	}
	.segmented button:hover {
		color: var(--text);
	}
	.segmented button[aria-pressed='true'] {
		background: var(--chip);
		color: var(--text);
		font-weight: 600;
	}
	pre {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
	.error-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}
	/* PLAN 64: in the window's tool bar the search sits left and gives way first. */
	:global(.win-toolbar) .search-box {
		flex: 0 1 15rem;
		/* Wide enough for its hint; the tool bar wraps before the hint is cut. */
		min-width: 12rem;
		width: auto;
		margin-right: auto;
	}
	.phone-tools {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	/* In this column the search box keeps its own height: the row layout's
	   flex-basis (10rem) would otherwise become its height and leave a large
	   empty gap around the field on the phone. */
	.phone-tools .search-box {
		flex: none;
		width: 100%;
	}
	@media (max-width: 639px) {
		.search-box {
			flex: 1 1 10rem;
			width: auto;
		}
		.segmented button {
			height: 2.5rem;
		}
	}
</style>
