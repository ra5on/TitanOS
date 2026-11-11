<script lang="ts">
	// The plugin shown beside the list: what it contains and changes, the
	// hardware found, its facts, and installing, switching, updating and
	// removing it.
	import { api } from '#lib/api.ts';
	import DangerZone from '#lib/components/DangerZone.svelte';
	import DriverChoice from '#lib/components/DriverChoice.svelte';
	import DriverUpdates from '#lib/components/DriverUpdates.svelte';
	import NetbirdSettings from '#lib/components/NetbirdSettings.svelte';
	import PluginSteps from '#lib/components/PluginSteps.svelte';
	import TailscaleSettings from '#lib/components/TailscaleSettings.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { hue, monogram, type Plugin, type Status } from './plugins.ts';
	import type { PluginsPage } from './plugins-page.svelte.ts';
	import ProblemNotice from './ProblemNotice.svelte';

	let { pg, status, current }: { pg: PluginsPage; status: Status; current: Plugin } = $props();
</script>

<aside class="tile" aria-labelledby="plugin-title" bind:this={pg.details}>
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
			<div class="block">
				<h3>{t('plugins.contains')}</h3>
				<div class="chips">
					{#each pg.contains(current) as entry (entry)}<span class="chip">{entry}</span>{/each}
				</div>
			</div>
		{/if}
		{#if pg.about(current)?.when}
			<div class="block">
				<h3>{t('plugins.when')}</h3>
				<p>{pg.about(current)?.when}</p>
			</div>
		{/if}
		{#if pg.about(current)?.hardware?.length}
			<div class="block">
				<h3>{t('plugins.hardware')}</h3>
				<ul>
					{#each pg.about(current)?.hardware ?? [] as entry (entry)}<li>{entry}</li>{/each}
				</ul>
			</div>
		{/if}
		{#if current.devices?.length || pg.about(current)?.hardware?.length}
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
			{#if pg.about(current)?.changes}<p>{pg.about(current)?.changes}</p>{/if}
		</div>
		{#if pg.about(current)?.limits?.length}
			<div class="block">
				<h3>{t('plugins.limits')}</h3>
				<ul>
					{#each pg.about(current)?.limits ?? [] as entry (entry)}<li>{entry}</li>{/each}
				</ul>
			</div>
		{/if}
		{#if current.license || current.upstream || pg.about(current)?.footprint || current.sources?.length}
			<div class="block">
				<h3>{t('plugins.facts')}</h3>
				<dl class="facts">
					{#if current.license}<dt>{t('plugins.license')}</dt>
						<dd>{current.license}</dd>{/if}
					{#if current.upstream}
						<dt>{t('plugins.upstream')}</dt>
						<dd>
							{#if current.upstream.url && pg.link(current.upstream.url)}<a
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
						})}{#if pg.about(current)?.footprint}. {pg.about(current)?.footprint}{/if}
					</dd>
					{#if current.sources?.length}
						<dt>{t('plugins.sources')}</dt>
						<dd>
							<ul class="plain">
								{#each current.sources as source (source)}<li class="mono">
										{#if pg.link(source)}<a href={source} rel="noreferrer">{source}</a
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

		{#if pg.shown && !pg.asking && pg.removing?.name !== current.name && pg.updating?.name !== current.name}
			<PluginSteps progress={pg.shown} ondismiss={() => pg.dismiss(pg.shown)} />
		{/if}

		{#if !current.installed && !pg.asking}
			<div class="row">
				<span class="muted path"
					>{current.download
						? t('plugins.githubSource', { version: status.catalog.release })
						: current.path}</span
				>
				<button
					type="button"
					class="btn btn-primary"
					disabled={pg.locked ||
						pg.refreshing ||
						!pg.volumes.length ||
						current.problem === 'kernel'}
					onclick={pg.ask}
					>{t(current.download ? 'plugins.downloadInstall' : 'plugins.install')}</button
				>
			</div>
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
				{#if !status.volume}
					<label
						><span>{t('plugins.volume')}</span>
						<select class="field" bind:value={pg.volume}
							>{#each pg.volumes as entry (entry)}<option value={entry}>{entry}</option
								>{/each}</select
						>
					</label>
				{/if}
				<label class="check"
					><input type="checkbox" bind:checked={pg.activate} disabled={pg.busy} /><span
						>{t('plugins.activateNow')}</span
					></label
				>
				<p class="muted note">{t('plugins.activateLaterHint')}</p>
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
				<div class="row">
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
			<div class="row">
				<label class="check"
					><input
						type="checkbox"
						checked={current.enabled}
						disabled={pg.locked}
						onchange={(event) => pg.toggle(current.name, event.currentTarget)}
					/><span>{t('plugins.enabled')}</span></label
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
						disabled={pg.locked}
						onclick={() => pg.openUpdate(current)}>{t('plugins.update')}</button
					>
				</div>
			{/if}
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
	.facts {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: var(--space-1-5) var(--space-3);
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
		gap: var(--space-4);
		padding: var(--tile-pad);
		font-size: var(--text-md);
	}
	.block {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
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
		gap: var(--space-1-5);
	}
	ul {
		margin: 0;
		padding-left: 1.25rem;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
	}
	.update {
		padding: var(--space-2-5) var(--space-3);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.confirm {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-base);
	}
	label > span {
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2-5);
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
