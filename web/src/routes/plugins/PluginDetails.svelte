<script lang="ts">
	// The plugin shown beside the list: what it contains and changes, the
	// hardware found, its facts, and installing, switching, updating and
	// removing it.
	import { api } from '#lib/api.ts';
	import DangerZone from '#lib/components/DangerZone.svelte';
	import DriverChoice from '#lib/components/DriverChoice.svelte';
	import DriverUpdates from '#lib/components/DriverUpdates.svelte';
	import NetbirdSettings from '#lib/components/NetbirdSettings.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import PluginSteps from '#lib/components/PluginSteps.svelte';
	import TailscaleSettings from '#lib/components/TailscaleSettings.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { hue, monogram, type Plugin, type Status } from './plugins.ts';
	import type { PluginsPage } from './plugins-page.svelte.ts';
	import ProblemNotice from './ProblemNotice.svelte';

	let { pg, status, current }: { pg: PluginsPage; status: Status; current: Plugin } = $props();
</script>

<!-- PLAN 78-5: the details are no tile: the plugin's name, what it is as
     text under plain headings, its facts and settings as flat rows. -->
<aside aria-labelledby="plugin-title" bind:this={pg.details}>
	<div class="head">
		<span class="monogram large" style:background="hsl({hue(current)} 45% 42%)" aria-hidden="true"
			>{monogram(current)}</span
		>
		<div class="about">
			<h2 id="plugin-title">{current.title}</h2>
			<span class="description">{pg.text(current.description)}</span>
			<span class="muted mono meta"
				>{current.name} · {current.version || '–'} · {formatBytes(current.size)} · {t(
					current.local
						? 'plugins.driverLocal'
						: current.official
							? 'plugins.official'
							: 'plugins.community',
				)}{#if current.source}
					· {pg.sourceName(current.source)}{/if}</span
			>
		</div>
	</div>

	<div class="body">
		{#if pg.contains(current).length > 0}
			<SettingGroup title={t('plugins.contains')} id="plugin-contains">
				<div class="chips">
					{#each pg.contains(current) as entry (entry)}<span class="chip">{entry}</span>{/each}
				</div>
			</SettingGroup>
		{/if}
		{#if pg.about(current)?.when}
			<SettingGroup title={t('plugins.when')} id="plugin-when">
				<p>{pg.about(current)?.when}</p>
			</SettingGroup>
		{/if}
		{#if pg.about(current)?.hardware?.length}
			<SettingGroup title={t('plugins.hardware')} id="plugin-hardware">
				<ul>
					{#each pg.about(current)?.hardware ?? [] as entry (entry)}<li>{entry}</li>{/each}
				</ul>
			</SettingGroup>
		{/if}
		{#if current.devices?.length || pg.about(current)?.hardware?.length}
			<SettingGroup title={t('plugins.detected')} id="plugin-detected">
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
			</SettingGroup>
		{/if}
		<SettingGroup title={t('plugins.does')} id="plugin-does">
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
			{#if pg.about(current)?.changes}<p>{pg.about(current)?.changes}</p>{/if}
		</SettingGroup>
		{#if pg.about(current)?.limits?.length}
			<SettingGroup title={t('plugins.limits')} id="plugin-limits">
				<ul>
					{#each pg.about(current)?.limits ?? [] as entry (entry)}<li>{entry}</li>{/each}
				</ul>
			</SettingGroup>
		{/if}
		{#if current.license || current.upstream || pg.about(current)?.footprint || current.sources?.length}
			<div class="sfacts">
				<SettingGroup title={t('plugins.facts')} id="plugin-facts">
					{#if current.license}
						<SettingRow title={t('plugins.license')}>
							{#snippet control()}<span>{current.license}</span>{/snippet}
						</SettingRow>
					{/if}
					{#if current.upstream}
						<SettingRow title={t('plugins.upstream')}>
							{#snippet control()}<span
									>{#if current.upstream?.url && pg.link(current.upstream.url)}<a
											href={current.upstream.url}
											rel="noreferrer">{current.upstream.name || current.upstream.url}</a
										>{:else}{current.upstream?.name}{/if}{#if current.upstream?.version}<span
											class="mono"
										>
											· {current.upstream.version}</span
										>{/if}</span
								>{/snippet}
						</SettingRow>
					{/if}
					<SettingRow title={t('plugins.footprint')}>
						{#snippet control()}<span
								>{t('plugins.diskSize', {
									size: formatBytes(current.size),
								})}{#if pg.about(current)?.footprint}. {pg.about(current)?.footprint}{/if}</span
							>{/snippet}
					</SettingRow>
					{#if current.sources?.length}
						<SettingRow title={t('plugins.sources')}>
							{#snippet control()}
								<ul class="plain">
									{#each current.sources as source (source)}<li class="mono">
											{#if pg.link(source)}<a href={source} rel="noreferrer">{source}</a
												>{:else}{source}{/if}
										</li>{/each}
								</ul>
							{/snippet}
						</SettingRow>
					{/if}
				</SettingGroup>
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

		{#if pg.shown && !pg.asking && pg.removing?.name !== current.name && pg.updating?.name !== current.name}
			<PluginSteps progress={pg.shown} ondismiss={() => pg.dismiss(pg.shown)} />
		{/if}

		{#if !current.installed && !pg.asking}
			<SettingGroup>
				<SettingRow
					icon="download"
					title={current.download
						? t('plugins.githubSource', { version: status.catalog.release })
						: current.path}
				>
					{#snippet control()}<button
							type="button"
							class="btn btn-primary"
							disabled={pg.locked ||
								pg.refreshing ||
								!pg.volumes.length ||
								current.problem === 'kernel'}
							onclick={pg.ask}
							>{t(current.download ? 'plugins.downloadInstall' : 'plugins.install')}</button
						>{/snippet}
				</SettingRow>
			</SettingGroup>
			{#if !pg.volumes.length}<p class="muted">
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
				<SettingGroup>
					{#if !status.volume}
						<SettingRow icon="disk" title={t('plugins.volume')} for="plugin-volume">
							{#snippet control()}<select id="plugin-volume" class="field" bind:value={pg.volume}
									>{#each pg.volumes as entry (entry)}<option value={entry}>{entry}</option
										>{/each}</select
								>{/snippet}
						</SettingRow>
					{/if}
					<SettingRow
						icon="power"
						title={t('plugins.activateNow')}
						for="plugin-activate"
						desc={t('plugins.activateLaterHint')}
					>
						{#snippet control()}<input
								id="plugin-activate"
								class="switch"
								type="checkbox"
								role="switch"
								bind:checked={pg.activate}
								disabled={pg.busy}
							/>{/snippet}
					</SettingRow>
				</SettingGroup>
				{#if pg.error}<ProblemNotice problem={pg.error} />{/if}
				{#if pg.shown}
					<PluginSteps
						progress={pg.shown}
						ondismiss={pg.busy ? undefined : () => pg.dismiss(pg.shown)}
					/>
				{:else if pg.busy}
					<!-- Until the first step arrives. -->
					<div class="progress" role="status">
						<progress
							aria-label={t(
								current.download ? 'plugins.downloadingChecking' : 'plugins.installing',
							)}
						></progress>
						<span class="muted"
							>{t(current.download ? 'plugins.downloadingChecking' : 'plugins.installing')}</span
						>
					</div>
				{/if}
				<div class="row-actions">
					<button type="button" class="btn" disabled={pg.busy} onclick={() => (pg.asking = false)}
						>{t('common.cancel')}</button
					>
					<button
						type="button"
						class="btn btn-primary"
						disabled={pg.busy || !pg.volume || current.problem === 'kernel'}
						onclick={() =>
							pg.run(
								() =>
									current.download && current.source
										? api.post<Status>('/plugins/external', {
												source: current.source,
												name: current.name,
												volume: pg.volume,
												confirm: current.name,
												activate: pg.activate,
											})
										: api.post<Status>(
												'/plugins',
												current.download
													? { name: current.name, volume: pg.volume, activate: pg.activate }
													: {
															path: current.path,
															volume: pg.volume,
															confirm: current.name,
															activate: pg.activate,
														},
											),
								current.name,
							)}
						>{t(
							pg.busy
								? current.download
									? 'plugins.downloading'
									: 'plugins.installing'
								: 'plugins.allow',
						)}</button
					>
				</div>
			</form>
		{:else}
			<SettingGroup>
				<SettingRow
					icon="power"
					title={t('plugins.enabled')}
					for="plugin-enabled"
					desc={t('plugins.enabledNote')}
				>
					{#snippet control()}<input
							id="plugin-enabled"
							class="switch"
							type="checkbox"
							role="switch"
							checked={current.enabled}
							disabled={pg.locked}
							onchange={(event) => pg.toggle(current.name, event.currentTarget)}
						/>{/snippet}
				</SettingRow>
				{#if current.update}
					<SettingRow
						icon="download"
						title={current.update.download
							? t('plugins.updateAvailableDownload', { version: current.update.version })
							: t('plugins.updateAvailable', { version: current.update.version })}
						desc={current.update.download ? undefined : current.update.path}
					>
						{#snippet control()}<button
								type="button"
								class="btn btn-primary"
								disabled={pg.locked}
								onclick={() => pg.openUpdate(current)}>{t('plugins.update')}</button
							>{/snippet}
					</SettingRow>
				{/if}
			</SettingGroup>
			{#if current.name === 'tailscale'}<TailscaleSettings
					onpending={(value) => (pg.tailnetPending = value)}
				/>{/if}
			{#if current.name === 'netbird'}<NetbirdSettings
					onpending={(value) => (pg.netbirdPending = value)}
				/>{/if}
			{#if current.name === 'nvidia' && (current.official || current.local) && status.driverUpdate?.drivers?.length}<DriverChoice
					status={status.driverUpdate}
					busy={pg.busy}
					action={(data) => pg.run(() => api.post<Status>('/plugins/nvidia/updates', data))}
				/>{:else if current.name === 'nvidia' && (current.official || current.local) && status.driverUpdate}<DriverUpdates
					status={status.driverUpdate}
					busy={pg.busy}
					action={(data) => pg.run(() => api.post<Status>('/plugins/nvidia/updates', data))}
				/>{/if}
			{#if current.enabled && current.problem === 'activation'}<button
					type="button"
					class="btn"
					disabled={pg.busy}
					onclick={() => pg.run(() => api.post<Status>(`/plugins/${current.name}/enable`))}
					>{t('plugins.retry')}</button
				>{/if}
			<DangerZone hint={t('plugins.removeHint')}>
				<button
					type="button"
					class="btn btn-danger"
					onclick={() => pg.openRemove(current)}
					disabled={pg.locked}>{t('plugins.remove')}</button
				>
			</DangerZone>
		{/if}
	</div>
</aside>

<style>
	.plain {
		list-style: none;
		padding: 0;
		text-align: right;
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
	.description {
		font-size: var(--text-base);
		overflow-wrap: anywhere;
	}
	aside {
		flex: 0 1 520px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	.head {
		display: flex;
		align-items: flex-start;
		gap: 14px;
	}
	h2 {
		font-size: var(--text-lg);
		font-weight: 600;
	}
	.meta {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		font-size: var(--text-md);
	}
	.body :global(.sgroup > p) {
		margin: 0;
	}
	.row-actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2-5);
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	ul {
		margin: 0;
		padding-left: 1.25rem;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.confirm {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.progress {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-base);
	}
	.progress progress {
		width: 100%;
		accent-color: var(--accent);
	}
</style>
