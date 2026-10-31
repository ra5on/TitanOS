<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { tick, untrack } from 'svelte';
	import { api } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import TailscaleSettings from '#lib/components/TailscaleSettings.svelte';
	import NetbirdSettings from '#lib/components/NetbirdSettings.svelte';
	import DriverUpdates from '#lib/components/DriverUpdates.svelte';
	import PluginSteps from '#lib/components/PluginSteps.svelte';
	import { latestFor, type PluginProgress } from '#lib/plugin-progress.ts';
	import { formatBytes } from '#lib/format.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import LinkDialog from './LinkDialog.svelte';
	import ProblemNotice from './ProblemNotice.svelte';
	import RemoveDialog from './RemoveDialog.svelte';
	import SourceRemoveDialog from './SourceRemoveDialog.svelte';
	import SourcesDialog from './SourcesDialog.svelte';
	import UpdateDialog from './UpdateDialog.svelte';
	import {
		aboutOf,
		badge,
		CATEGORIES,
		categoryKey,
		categoryOf,
		containsOf,
		hue,
		isLink,
		localized,
		matches,
		monogram,
		pluginProblem,
		type Category,
		type Plugin,
		type Source,
		type Status,
		type Problem,
	} from './plugins.ts';

	let status = $state<Status | null>(null);
	let error = $state<Problem | null>(null);
	let picked = $state('');
	let asking = $state(false);
	let activate = $state(true);
	let volume = $state('');
	let busy = $state(false);
	let refreshing = $state(false);
	// The plugin the remove dialog is about, fixed when it opens; the polled
	// list may change meanwhile.
	let removing = $state<{ name: string; title: string } | null>(null);
	// Likewise the update dialog: plugin and the version it was opened for.
	let updating = $state<{
		name: string;
		title: string;
		path: string;
		version: string;
		official: boolean;
		download?: boolean;
	} | null>(null);
	// Community plugins: a single link, and the list of sources.
	let linking = $state(false);
	let linkURL = $state('');
	let linkPreview = $state<Plugin | null>(null);
	let linkChecking = $state(false);
	let sourcesOpen = $state(false);
	let sourceURL = $state('');
	let sourceAdding = $state(false);
	let sourceRemoving = $state<Source | null>(null);
	const sources = $derived(status?.sources ?? []);
	const sourceName = (id?: string) => sources.find((entry) => entry.id === id)?.name ?? '';
	let tailnetPending = $state(false);
	let netbirdPending = $state(false);
	// Steps of installations, updates and removals as the NAS reports them.
	// They keep running when the page is left; a finished one is shown for the
	// plugins changed here and for failures until it is dismissed.
	let operations = $state<PluginProgress[]>([]);
	let actedOn = $state<string[]>([]);
	const working = $derived(operations.some((entry) => entry.state === 'running'));

	const locked = $derived(
		busy || working || tailnetPending || netbirdPending || !!status?.driverUpdate?.pending,
	);
	const plugins = $derived(status?.plugins ?? []);
	const current = $derived(plugins.find((plugin) => plugin.name === picked) ?? plugins[0]);
	const volumes = $derived(
		(storage.status?.volumes ?? []).filter((entry) => entry.mounted).map((entry) => entry.name),
	);
	const text = (texts: Record<string, string> | null) => localized(texts, i18n.locale);
	const contains = (plugin: Plugin) => containsOf(plugin, i18n.locale);
	const about = (plugin: Plugin) => aboutOf(plugin, i18n.locale);
	let query = $state('');
	let area = $state<Category | ''>('');
	const shownPlugins = $derived(
		plugins.filter(
			(plugin) =>
				(!area || categoryOf(plugin) === area) &&
				matches(plugin, query.trim().toLowerCase(), i18n.locale),
		),
	);
	const groups = $derived(
		CATEGORIES.map((id) => ({
			id,
			plugins: shownPlugins.filter((plugin) => categoryOf(plugin) === id),
		})).filter((group) => group.plugins.length > 0),
	);
	const usedAreas = $derived(
		CATEGORIES.filter((id) => plugins.some((plugin) => categoryOf(plugin) === id)),
	);
	const recommendations = $derived(status?.recommendations ?? []);
	const fits = (plugin: Plugin) =>
		!plugin.installed && recommendations.some((entry) => entry.plugin === plugin.name);
	const link = isLink;

	// Opens the plugin and, if it can be installed right away, its install form.
	async function showRecommended(name: string) {
		const plugin = plugins.find((entry) => entry.name === name);
		if (!plugin || busy) return;
		query = '';
		area = '';
		await pick(name);
		if (!plugin.installed) ask();
	}

	function stepsFor(name: string): PluginProgress | undefined {
		const entry = latestFor(operations, name);
		if (!entry) return undefined;
		return entry.state === 'running' || entry.state === 'failed' || actedOn.includes(name)
			? entry
			: undefined;
	}
	async function loadProgress() {
		try {
			operations =
				(await api.get<{ operations: PluginProgress[] }>('/plugins/progress')).operations ?? [];
		} catch {
			// The status poll shows connection problems; the steps keep their last state.
		}
	}
	async function dismiss(entry: PluginProgress | undefined) {
		if (!entry) return;
		operations = operations.filter((other) => other.id !== entry.id);
		actedOn = actedOn.filter((name) => name !== entry.plugin);
		try {
			await api.delete(`/plugins/progress/${encodeURIComponent(entry.id)}`);
		} catch {
			// Already gone, or still kept by the NAS: the next poll shows it again.
		}
	}

	function problem(err: unknown) {
		error = pluginProblem(err);
	}

	// Polling goes on during a change, so the page keeps showing what the
	// NAS does; an answer that started before the change ended is dropped.
	let changes = 0;
	async function load() {
		void loadProgress();
		const started = changes;
		try {
			const next = await api.get<Status>('/plugins');
			if (started === changes) status = next;
		} catch (err) {
			if (!status) problem(err);
		}
	}
	async function refreshCatalog(force = false) {
		if (busy || refreshing) return;
		refreshing = true;
		try {
			const updated = await api.post<Status>('/plugins/catalog', { force });
			if (!busy) status = updated;
		} catch (err) {
			problem(err);
		} finally {
			refreshing = false;
		}
	}
	$effect(() => {
		void untrack(load).then(() => refreshCatalog());
		return poll(load, { interval: 5000, immediate: false });
	});
	// While something runs, follow its steps every second.
	$effect(() => {
		if (!busy && !working) return;
		return poll(loadProgress, { interval: 1000, immediate: false });
	});

	async function run(action: () => Promise<Status>, plugin = '') {
		if (plugin && !actedOn.includes(plugin)) actedOn = [...actedOn, plugin];
		busy = true;
		error = null;
		changes++;
		try {
			status = await action();
			asking = false;
			removing = null;
			updating = null;
		} catch (err) {
			problem(err);
		}
		busy = false;
		changes++;
		await load();
	}
	// Steps of the shown plugin outside its dialogs: after coming back to the
	// page, or a failure to look at.
	const shown = $derived(current ? stepsFor(current.name) : undefined);
	const removingSteps = $derived(removing ? stepsFor(removing.name) : undefined);
	const updatingSteps = $derived(updating ? stepsFor(updating.name) : undefined);

	// The box shows what the NAS has; a refused change is turned back.
	async function toggle(name: string, box: HTMLInputElement) {
		await run(() => api.post<Status>(`/plugins/${name}/${box.checked ? 'enable' : 'disable'}`));
		const plugin = status?.plugins.find((entry) => entry.name === name);
		if (plugin) box.checked = plugin.enabled;
	}

	// A new attempt starts without the marks of the previous one.
	function clearFinished(name: string) {
		const entry = stepsFor(name);
		if (entry && entry.state !== 'running') void dismiss(entry);
	}

	function openRemove(plugin: Plugin) {
		clearFinished(plugin.name);
		error = null;
		removing = { name: plugin.name, title: plugin.title };
	}

	function openUpdate(plugin: Plugin) {
		if (!plugin.update) return;
		clearFinished(plugin.name);
		error = null;
		updating = { name: plugin.name, title: plugin.title, ...plugin.update };
	}

	function openLink() {
		error = null;
		linkURL = '';
		linkPreview = null;
		activate = true;
		volume = status?.volume || volumes[0] || '';
		linking = true;
	}
	// Reads only the manifest; the image is downloaded when installing.
	async function checkLink() {
		if (!linkURL.trim() || linkChecking) return;
		linkChecking = true;
		error = null;
		linkPreview = null;
		try {
			linkPreview = await api.post<Plugin>('/plugins/external/inspect', { url: linkURL.trim() });
		} catch (err) {
			problem(err);
		} finally {
			linkChecking = false;
		}
	}
	async function installLink() {
		const preview = linkPreview;
		if (!preview) return;
		await run(
			() =>
				api.post<Status>('/plugins/external', {
					url: linkURL.trim(),
					volume,
					confirm: preview.name,
					activate,
				}),
			preview.name,
		);
		if (!error) linking = false;
	}
	async function addSource() {
		if (!sourceURL.trim() || sourceAdding) return;
		sourceAdding = true;
		error = null;
		try {
			await api.post('/plugins/sources', { url: sourceURL.trim() });
			sourceURL = '';
			await refreshCatalog(true);
		} catch (err) {
			problem(err);
		} finally {
			sourceAdding = false;
		}
	}
	async function removeSource(source: Source) {
		error = null;
		try {
			await api.delete(`/plugins/sources/${encodeURIComponent(source.id)}`);
			sourceRemoving = null;
			await refreshCatalog(true);
		} catch (err) {
			problem(err);
		}
	}

	function ask() {
		if (current) clearFinished(current.name);
		activate = true;
		volume = status?.volume || volumes[0] || '';
		error = null;
		asking = true;
	}

	let details: HTMLElement | undefined = $state();
	async function pick(name: string) {
		if (busy) return;
		picked = name;
		asking = false;
		error = null;
		// On a phone the details lie below the list: bring them into view.
		if (window.matchMedia('(max-width: 760px)').matches) {
			await tick();
			details?.scrollIntoView({ block: 'start', behavior: 'smooth' });
		}
	}
</script>

<PageHead title={t('plugins.title')}>
	<p class="muted">{t('plugins.intro')} <a href="/apps">{t('nav.apps')}</a></p>
	<p class="muted">{t('plugins.catalogIntro')}</p>
	{#snippet actions()}
		<a class="btn" href="/plugins/cleanup">{t('cleanup.title')}</a>
		<button
			type="button"
			class="btn"
			disabled={locked}
			onclick={() => ((error = null), (sourcesOpen = true))}>{t('plugins.sourceList')}</button
		>
		<button type="button" class="btn" disabled={locked || !volumes.length} onclick={openLink}
			>{t('plugins.fromLink')}</button
		>
		<button
			type="button"
			class="btn"
			disabled={busy || refreshing}
			onclick={() => refreshCatalog(true)}
			>{t(refreshing ? 'plugins.catalogLoading' : 'plugins.catalogRefresh')}</button
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

{#if error && !asking && !removing && !updating && !linking && !sourcesOpen}
	<ProblemNotice problem={error} />
{/if}

{#if status && recommendations.length > 0}
	<section class="tile panel recommended" aria-labelledby="recommended-title">
		<h2 class="tile-title" id="recommended-title">{t('plugins.recommendedTitle')}</h2>
		<p class="muted note">{t('plugins.recommendedIntro')}</p>
		<ul class="recommendations">
			{#each recommendations as entry (entry.plugin)}
				{@const offered = plugins.find((plugin) => plugin.name === entry.plugin)}
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
							disabled={busy}
							onclick={() =>
								run(() =>
									api.post<Status>(
										`/plugins/recommendations/${encodeURIComponent(entry.plugin)}/dismiss`,
										{},
									),
								)}>{t('plugins.recommendedDismiss')}</button
						>
						<button
							type="button"
							class="btn btn-primary"
							disabled={busy || !offered}
							onclick={() => showRecommended(entry.plugin)}>{t('plugins.recommendedShow')}</button
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
			disabled={busy}
			onclick={() => run(() => api.delete<Status>('/plugins/recommendations/dismissed'))}
			>{t('plugins.recommendedRestore', { count: status.dismissed })}</button
		>
	</p>
{/if}

{#if status && plugins.length === 0}
	<EmptyState
		title={t(refreshing ? 'plugins.catalogLoading' : 'plugins.emptyTitle')}
		text={refreshing ? undefined : [t('plugins.emptyText'), t('plugins.emptyOfficial')]}
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
					bind:value={query}
				/>
				<select class="field" aria-label={t('plugins.categoryAll')} bind:value={area}>
					<option value="">{t('plugins.categoryAll')}</option>
					{#each usedAreas as id (id)}<option value={id}>{t(categoryKey[id])}</option>{/each}
				</select>
			</div>
			{#each groups as group (group.id)}
				<h2 class="group">{t(categoryKey[group.id])}</h2>
				{#each group.plugins as plugin (plugin.name)}
					{@const state = badge(plugin)}
					<button
						type="button"
						class="tile card"
						aria-pressed={plugin.name === current.name}
						onclick={() => pick(plugin.name)}
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
										· {sourceName(plugin.source)}{/if}</span
								></span
							>
							<span class="description">{text(plugin.description)}</span>
						</span>
						{#if fits(plugin)}<span class="chip ok">{t('plugins.fits')}</span>{/if}
						{#if plugin.update}<span class="chip">{t('plugins.updateChip')}</span>{/if}
						<span class="chip {state.kind}">{state.text}</span>
					</button>
				{/each}
			{:else}
				<p class="muted note">{t('plugins.noMatch')}</p>
			{/each}
			<p class="muted note">
				{status.volume ? t('plugins.where', { volume: status.volume }) : t('plugins.whereNone')}
			</p>
		</div>

		<aside class="tile" aria-labelledby="plugin-title" bind:this={details}>
			<div class="head">
				<span
					class="monogram large"
					style:background="hsl({hue(current)} 45% 42%)"
					aria-hidden="true">{monogram(current)}</span
				>
				<div class="about">
					<h2 id="plugin-title">{current.title}</h2>
					<span class="description">{text(current.description)}</span>
					<span class="muted mono meta"
						>{current.name} · {current.version || '–'} · {formatBytes(current.size)} · {t(
							current.local
								? 'plugins.driverLocal'
								: current.official
									? 'plugins.official'
									: 'plugins.community',
						)}{#if current.source}
							· {sourceName(current.source)}{/if}</span
					>
				</div>
			</div>

			<div class="body">
				{#if contains(current).length > 0}
					<div class="block">
						<h3>{t('plugins.contains')}</h3>
						<div class="chips">
							{#each contains(current) as entry (entry)}<span class="chip">{entry}</span>{/each}
						</div>
					</div>
				{/if}
				{#if about(current)?.when}
					<div class="block">
						<h3>{t('plugins.when')}</h3>
						<p>{about(current)?.when}</p>
					</div>
				{/if}
				{#if about(current)?.hardware?.length}
					<div class="block">
						<h3>{t('plugins.hardware')}</h3>
						<ul>
							{#each about(current)?.hardware ?? [] as entry (entry)}<li>{entry}</li>{/each}
						</ul>
					</div>
				{/if}
				{#if current.devices?.length || about(current)?.hardware?.length}
					<div class="block">
						<h3>{t('plugins.detected')}</h3>
						{#if current.devices?.length}
							<ul>
								{#each current.devices as device (device.key + (device.address ?? ''))}
									<li>
										{device.name}{#if device.address}<span class="muted mono">
												· {device.address}</span
											>{/if}
										{#if device.bus !== 'dmi'}<span class="muted">
												· {device.driver
													? t('plugins.detectedDriver', { driver: device.driver })
													: t('plugins.detectedNoDriver')}</span
											>{/if}
									</li>
								{/each}
							</ul>
						{:else}
							<p class="muted">{t('plugins.detectedNone')}</p>
						{/if}
					</div>
				{/if}
				<div class="block">
					<h3>{t('plugins.does')}</h3>
					<ul>
						<li>{t('plugins.doesFiles')}</li>
						{#if current.modules?.length}<li>
								{t('plugins.doesModules')} <span class="mono">{current.modules.join(', ')}</span>
							</li>{/if}
						{#if current.services?.length}<li>
								{t('plugins.doesServices')} <span class="mono">{current.services.join(', ')}</span>
							</li>{/if}
						{#if current.kernel}<li>{t('plugins.forKernel', { kernel: current.kernel })}</li>{/if}
					</ul>
					{#if about(current)?.changes}<p>{about(current)?.changes}</p>{/if}
				</div>
				{#if about(current)?.limits?.length}
					<div class="block">
						<h3>{t('plugins.limits')}</h3>
						<ul>
							{#each about(current)?.limits ?? [] as entry (entry)}<li>{entry}</li>{/each}
						</ul>
					</div>
				{/if}
				{#if current.license || current.upstream || about(current)?.footprint || current.sources?.length}
					<div class="block">
						<h3>{t('plugins.facts')}</h3>
						<dl class="facts">
							{#if current.license}<dt>{t('plugins.license')}</dt>
								<dd>{current.license}</dd>{/if}
							{#if current.upstream}
								<dt>{t('plugins.upstream')}</dt>
								<dd>
									{#if current.upstream.url && link(current.upstream.url)}<a
											href={current.upstream.url}
											rel="noreferrer">{current.upstream.name || current.upstream.url}</a
										>{:else}{current.upstream.name}{/if}{#if current.upstream.version}<span
											class="mono"
										>
											· {current.upstream.version}</span
										>{/if}
								</dd>
							{/if}
							<dt>{t('plugins.footprint')}</dt>
							<dd>
								{t('plugins.diskSize', {
									size: formatBytes(current.size),
								})}{#if about(current)?.footprint}. {about(current)?.footprint}{/if}
							</dd>
							{#if current.sources?.length}
								<dt>{t('plugins.sources')}</dt>
								<dd>
									<ul class="plain">
										{#each current.sources as source (source)}<li class="mono">
												{#if link(source)}<a href={source} rel="noreferrer">{source}</a
													>{:else}{source}{/if}
											</li>{/each}
									</ul>
								</dd>
							{/if}
						</dl>
					</div>
				{/if}

				{#if current.problem === 'kernel'}
					<p class="notice-warn" role="status">
						{t('plugins.kernelMismatch', { built: current.kernel, running: status.kernel })}
					</p>
				{:else if current.problem === 'activation'}
					<p class="notice-warn" role="status">{t('plugins.activationFailed')}</p>
				{:else if current.problem === 'integrity'}
					<p class="notice-warn" role="status">{t('plugins.integrityFailed')}</p>
				{:else if current.problem === 'busy'}
					<p class="notice-warn" role="status">{t('plugins.busy')}</p>
				{/if}

				{#if shown && !asking && removing?.name !== current.name && updating?.name !== current.name}
					<PluginSteps progress={shown} ondismiss={() => dismiss(shown)} />
				{/if}

				{#if !current.installed && !asking}
					<div class="row">
						<span class="muted path"
							>{current.download
								? t('plugins.githubSource', { version: status.catalog.release })
								: current.path}</span
						>
						<button
							type="button"
							class="btn btn-primary"
							disabled={locked || refreshing || !volumes.length || current.problem === 'kernel'}
							onclick={ask}
							>{t(current.download ? 'plugins.downloadInstall' : 'plugins.install')}</button
						>
					</div>
					{#if !volumes.length}<p class="muted">
							{t('plugins.needVolume')} <a href="/storage">{t('nav.storage')}</a>
						</p>{/if}
				{:else if !current.installed}
					<!-- Installs only on a click; the API still wants the plugin name as confirmation for a local (community) plugin. -->
					<form class="confirm" onsubmit={(event) => event.preventDefault()}>
						<p class="notice-warn">
							{t(current.official ? 'plugins.trustOfficial' : 'plugins.trustCommunity')}
						</p>
						{#if current.download}<p class="muted">
								{t('plugins.downloadNote', { size: formatBytes(current.size) })}
							</p>{/if}
						{#if current.licenseUrl}<a href={current.licenseUrl}>{t('plugins.licenses')}</a>{/if}
						{#if !status.volume}
							<label
								><span>{t('plugins.volume')}</span>
								<select class="field" bind:value={volume}
									>{#each volumes as entry (entry)}<option value={entry}>{entry}</option
										>{/each}</select
								>
							</label>
						{/if}
						<label class="check"
							><input type="checkbox" bind:checked={activate} disabled={busy} /><span
								>{t('plugins.activateNow')}</span
							></label
						>
						<p class="muted note">{t('plugins.activateLaterHint')}</p>
						{#if error}<ProblemNotice problem={error} />{/if}
						{#if shown}
							<PluginSteps progress={shown} ondismiss={busy ? undefined : () => dismiss(shown)} />
						{:else if busy}
							<!-- Until the first step arrives. -->
							<div class="progress" role="status">
								<progress
									aria-label={t(
										current.download ? 'plugins.downloadingChecking' : 'plugins.installing',
									)}
								></progress>
								<span class="muted"
									>{t(
										current.download ? 'plugins.downloadingChecking' : 'plugins.installing',
									)}</span
								>
							</div>
						{/if}
						<div class="row">
							<button type="button" class="btn" disabled={busy} onclick={() => (asking = false)}
								>{t('common.cancel')}</button
							>
							<button
								type="button"
								class="btn btn-primary"
								disabled={busy || !volume || current.problem === 'kernel'}
								onclick={() =>
									run(
										() =>
											current.download && current.source
												? api.post<Status>('/plugins/external', {
														source: current.source,
														name: current.name,
														volume,
														confirm: current.name,
														activate,
													})
												: api.post<Status>(
														'/plugins',
														current.download
															? { name: current.name, volume, activate }
															: { path: current.path, volume, confirm: current.name, activate },
													),
										current.name,
									)}
								>{t(
									busy
										? current.download
											? 'plugins.downloading'
											: 'plugins.installing'
										: 'plugins.allow',
								)}</button
							>
						</div>
					</form>
				{:else}
					<div class="row">
						<label class="check"
							><input
								type="checkbox"
								checked={current.enabled}
								disabled={locked}
								onchange={(event) => toggle(current.name, event.currentTarget)}
							/><span>{t('plugins.enabled')}</span></label
						>
						<button type="button" class="btn" onclick={() => openRemove(current)} disabled={locked}
							>{t('plugins.remove')}</button
						>
					</div>
					<p class="muted note">{t('plugins.enabledNote')}</p>
					{#if current.update}
						<div class="row update">
							{#if current.update.download}<span
									>{t('plugins.updateAvailableDownload', { version: current.update.version })}</span
								>{:else}<span
									>{t('plugins.updateAvailable', { version: current.update.version })}
									<span class="muted path">{current.update.path}</span></span
								>{/if}
							<button
								type="button"
								class="btn btn-primary"
								disabled={locked}
								onclick={() => openUpdate(current)}>{t('plugins.update')}</button
							>
						</div>
					{/if}
					{#if current.name === 'tailscale'}<TailscaleSettings
							onpending={(value) => (tailnetPending = value)}
						/>{/if}
					{#if current.name === 'netbird'}<NetbirdSettings
							onpending={(value) => (netbirdPending = value)}
						/>{/if}
					{#if current.name === 'nvidia' && (current.official || current.local) && status.driverUpdate}<DriverUpdates
							status={status.driverUpdate}
							{busy}
							action={(data) => run(() => api.post<Status>('/plugins/nvidia/updates', data))}
						/>{/if}
					{#if current.enabled && current.problem === 'activation'}<button
							type="button"
							class="btn"
							disabled={busy}
							onclick={() => run(() => api.post<Status>(`/plugins/${current.name}/enable`))}
							>{t('plugins.retry')}</button
						>{/if}
				{/if}
			</div>
		</aside>
	</div>
{/if}

{#if removing}
	{@const target = removing}
	<RemoveDialog
		title={target.title}
		{busy}
		steps={removingSteps}
		{error}
		onclose={() => (removing = null)}
		onremove={() => run(() => api.delete<Status>(`/plugins/${target.name}`), target.name)}
	/>
{/if}

{#if linking}
	<LinkDialog
		bind:url={linkURL}
		bind:volume
		bind:activate
		preview={linkPreview}
		chooseVolume={!status?.volume}
		{volumes}
		{busy}
		checking={linkChecking}
		steps={linkPreview ? stepsFor(linkPreview.name) : undefined}
		{error}
		onclose={() => (linking = false)}
		oncheck={checkLink}
		oninstall={installLink}
		onedit={() => (linkPreview = null)}
	/>
{/if}

{#if sourcesOpen}
	<SourcesDialog
		{sources}
		bind:url={sourceURL}
		adding={sourceAdding}
		{error}
		onclose={() => (sourcesOpen = false)}
		onadd={addSource}
		onremove={(source) => (sourceRemoving = source)}
	/>
{/if}

{#if sourceRemoving}
	{@const target = sourceRemoving}
	<SourceRemoveDialog
		name={target.name}
		onclose={() => (sourceRemoving = null)}
		onremove={() => removeSource(target)}
	/>
{/if}

{#if updating}
	{@const target = updating}
	<UpdateDialog
		{target}
		{busy}
		steps={updatingSteps}
		{error}
		onclose={() => (updating = null)}
		onupdate={() =>
			run(
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
		gap: 4px;
	}
	.intro {
		max-width: 60rem;
		font-size: var(--text-md);
	}
	.empty {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 32px;
		font-size: var(--text-md);
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: 24px;
	}
	.list {
		flex: 1 1 520px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 12px;
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
		margin-bottom: 16px;
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
		gap: 12px;
		padding: 12px 0;
		border-top: 1px solid var(--panel-border);
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.filters input {
		flex: 1 1 14rem;
	}
	.group {
		margin-top: 8px;
		font-size: var(--text-sm);
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.facts {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 6px 12px;
		margin: 0;
	}
	.facts dt {
		color: var(--muted);
	}
	.facts dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.plain {
		list-style: none;
		padding: 0;
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
	.monogram.large {
		width: 56px;
		height: 56px;
		font-size: var(--text-xl);
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
		gap: 4px 10px;
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
	aside {
		flex: 0 1 520px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}
	.head {
		display: flex;
		align-items: flex-start;
		gap: 14px;
		padding: 20px;
		border-bottom: 1px solid var(--panel-border);
	}
	h2 {
		font-size: var(--text-lg);
		font-weight: 600;
	}
	.meta,
	.path {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: var(--tile-pad);
		font-size: var(--text-md);
	}
	.block {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	h3 {
		font-size: var(--text-sm);
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	ul {
		margin: 0;
		padding-left: 1.25rem;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.update {
		padding: 10px 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.confirm {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-base);
	}
	label > span {
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: var(--text-md);
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: var(--text-md);
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
	.progress {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-base);
	}
	.progress progress {
		width: 100%;
		accent-color: var(--accent);
	}
</style>
