<script lang="ts">
	// PLAN 79-7: why the detection does or does not run, without a terminal –
	// the state in plain words, the cause the NAS read from Frigate's log with
	// what it means and what to do, the detection device, every camera's
	// frames, the container, and the log to read and copy. The owner tests
	// the NPU with this page.
	import { api } from '#lib/api.ts';
	import BackendDetail from '#lib/components/BackendDetail.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { formatDecimal } from '#lib/format.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { nvr, refreshNvr } from '#lib/nvr/nvr.svelte.ts';
	import {
		deviceName,
		reasonAdvice,
		reasonMeaning,
		reasonTitle,
		sinceText,
	} from '#lib/nvr/texts.ts';
	import type { Diagnosis } from '#lib/nvr/types.ts';
	import { poll } from '#lib/poll.ts';

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	let d = $state<Diagnosis | null>(null);
	let error = $state('');
	async function load() {
		try {
			d = await api.get<Diagnosis>('/nvr/diagnosis');
			error = '';
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	$effect(() => poll(load, { interval: 10000 }));

	let busy = $state(false);
	async function retry() {
		busy = true;
		try {
			await api.post('/nvr/detector/retry', {});
			await Promise.all([load(), refreshNvr()]);
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	const devices = $derived(d?.hardware.devices ?? []);
	const cause = $derived(d?.detector.failover?.reason ?? d?.lastError?.code ?? '');
	const causeDetail = $derived(d?.detector.failover?.detail || d?.lastError?.detail || '');
	const when = (iso: string | null | undefined) => (iso ? sinceText(iso, locale) : '–');
	const fps = (value: number) => formatDecimal(value, 1);

	let copied = $state('');
	async function copy(what: 'log' | 'details') {
		if (!d) return;
		const text =
			what === 'log' ? d.log.join('\n') : JSON.stringify({ ...d, log: d.log.slice(-60) }, null, 2);
		try {
			await navigator.clipboard.writeText(text);
			copied = what;
			setTimeout(() => (copied = ''), 2500);
		} catch {
			error = t('nvr.diag.copyFailed');
		}
	}
</script>

<PageHead title={t('nvr.menu.diagnosis')}>
	<p class="muted">{t('nvr.diag.intro')}</p>
</PageHead>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if !d}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else}
	<SettingGroup title={t('nvr.diag.state')} id="nvr-diag-state">
		{#snippet tools()}
			<button type="button" class="btn" onclick={() => copy('details')}
				><Icon name="copy" size={15} />{copied === 'details'
					? t('nvr.diag.copied')
					: t('nvr.diag.copyDetails')}</button
			>
		{/snippet}
		<SettingRow
			icon="activity"
			title={t(`nvr.diag.stateName.${d.state}` as MessageKey)}
			desc={t(`nvr.diag.stateText.${d.state}` as MessageKey)}
			id="nvr-diag-now"
		/>
		{#if cause}
			<SettingRow icon="info" title={reasonTitle(cause)} id="nvr-diag-cause">
				{#snippet description()}
					<p><b>{t('nvr.diag.means')}</b> {reasonMeaning(cause)}</p>
					<p><b>{t('nvr.diag.todo')}</b> {reasonAdvice(cause)}</p>
					<p class="mono code">{cause}</p>
					<BackendDetail text={causeDetail} />
				{/snippet}
			</SettingRow>
		{/if}
		{#if d.detector.failover}
			{@const f = d.detector.failover}
			<SettingRow
				icon="restart"
				title={t('nvr.failover.title', {
					to: deviceName(f.to, devices),
					from: deviceName(f.from, devices),
				})}
				desc="{t('nvr.failover.since', { time: when(f.at) })} {t('nvr.failover.retryHint')}"
				id="nvr-diag-failover"
			>
				{#snippet control()}
					<button type="button" class="btn" disabled={busy} onclick={retry}>{t('nvr.retry')}</button
					>
				{/snippet}
			</SettingRow>
		{:else if d.state === 'stopped'}
			<SettingRow icon="restart" title={t('nvr.diag.restart')} desc={t('nvr.diag.restartHint')}>
				{#snippet control()}
					<button type="button" class="btn" disabled={busy} onclick={retry}>{t('nvr.retry')}</button
					>
				{/snippet}
			</SettingRow>
		{/if}
	</SettingGroup>

	<SettingGroup title={t('nvr.diag.detection')} id="nvr-diag-detector">
		<SettingRow
			icon="zap"
			title={t('nvr.detector.title')}
			desc={t('nvr.diag.chosen', {
				device:
					d.detector.kind === 'auto'
						? t('nvr.detector.auto', { device: deviceName(d.hardware.auto) })
						: deviceName(d.detector.kind, devices),
			})}
			summary={deviceName(d.detector.active, devices)}
		/>
		<SettingRow
			icon="clock"
			title={t('nvr.diag.inference')}
			desc={t('nvr.diag.inferenceHint')}
			summary={d.detector.inferenceMs === null
				? t('nvr.diag.unknown')
				: t('nvr.diag.ms', { ms: formatDecimal(d.detector.inferenceMs, 1) })}
		/>
		<SettingRow
			icon="chip"
			title={t('nvr.diag.process')}
			summary={d.detectorPid === null
				? t('nvr.diag.processNone')
				: t('nvr.diag.processPid', { pid: d.detectorPid })}
		/>
		<SettingRow
			icon="npu"
			title="/dev/accel"
			desc={t('nvr.diag.accelHint')}
			summary={d.accelDevice ? t('nvr.diag.present') : t('nvr.diag.absent')}
		/>
		<SettingRow icon="memory" title={t('nvr.diag.firmware')} id="nvr-diag-firmware">
			{#snippet description()}
				{#if d?.npuFirmware.length}
					{#each d.npuFirmware as line (line)}<p class="mono wrap">{line}</p>{/each}
				{:else}{t('nvr.diag.firmwareNone')}{/if}
			{/snippet}
		</SettingRow>
		<SettingRow icon="list" title={t('nvr.diag.devices')}>
			{#snippet description()}
				{#each devices as device (device.kind)}
					<p>
						{deviceName(device.kind, devices)}:
						{device.available
							? t('nvr.diag.usable')
							: t(
									`nvr.device.reason.${['driver', 'vm', 'plugin'].includes(device.reason ?? '') ? device.reason : 'other'}` as MessageKey,
								)}
					</p>
				{/each}
				<p>
					{d?.hardware.videoDecode ? t('nvr.diag.decodeYes') : t('nvr.diag.decodeNo')}
				</p>
				<p>
					{d?.hardware.enrichmentsPossible ? t('nvr.diag.enrichYes') : t('nvr.setup.enrichNo')}
				</p>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('nvr.menu.cameras')} id="nvr-diag-cameras">
		{#each d.cameras as camera (camera.id)}
			<SettingRow
				icon="camera"
				title={camera.name}
				desc={t('nvr.diag.cameraFps', {
					fps: fps(camera.fps),
					detect: fps(camera.detectionFps),
					process: fps(camera.processFps),
					skipped: fps(camera.skippedFps),
				})}
				summary={!camera.known
					? t('nvr.camera.state.unknown')
					: camera.online
						? t('nvr.camera.state.online')
						: camera.offlineSince
							? t('nvr.camera.offlineSince', { time: when(camera.offlineSince) })
							: t('nvr.camera.state.offline')}
			/>
		{/each}
		{#if d.cameras.length === 0}<p class="muted">{t('nvr.live.noneTitle')}</p>{/if}
	</SettingGroup>

	<SettingGroup title={t('nvr.diag.container')} id="nvr-diag-container">
		<SettingRow
			icon="box"
			title={t('nvr.diag.containerState')}
			desc={t('nvr.diag.containerTimes', {
				started: when(d.container.startedAt),
				finished: when(d.container.finishedAt),
				ends: d.container.recentEnds,
			})}
			summary="{t(
				`nvr.diag.containerName.${['running', 'exited', 'missing'].includes(d.container.state) ? d.container.state : 'other'}` as MessageKey,
			)}{d.container.state === 'exited' ? ` (${d.container.exitCode})` : ''}"
		/>
		<SettingRow icon="archive" title={t('nvr.diag.image')} id="nvr-diag-image">
			{#snippet description()}
				<p>
					{t('nvr.diag.imageVersion', { version: d?.imageVersion ?? '' })} ·
					{d?.imagePresent ? t('nvr.diag.imagePresent') : t('nvr.diag.imageAbsent')}
					{#if d?.runningVersion}
						· {t('nvr.diag.runningVersion', { version: d.runningVersion })}{/if}
				</p>
				<p class="mono wrap">{d?.imageReference}</p>
			{/snippet}
		</SettingRow>
		<SettingRow
			icon="memory"
			title={t('nvr.diag.shm')}
			desc={t('nvr.diag.shmHint')}
			summary={d.shm.totalMB === null || d.shm.usedMB === null
				? t('nvr.diag.unknown')
				: t('nvr.diag.shmUsed', {
						used: Math.round(d.shm.usedMB),
						total: Math.round(d.shm.totalMB),
					})}
		/>
	</SettingGroup>

	<SettingGroup
		title={t('nvr.diag.log')}
		desc={t(d.logSource === 'frigate' ? 'nvr.diag.logFrigate' : 'nvr.diag.logContainer')}
		id="nvr-diag-log"
	>
		{#snippet tools()}
			<button type="button" class="btn" disabled={d?.log.length === 0} onclick={() => copy('log')}
				><Icon name="copy" size={15} />{copied === 'log'
					? t('nvr.diag.copied')
					: t('nvr.diag.copyLog')}</button
			>
		{/snippet}
		{#if d.log.length}
			<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
			<pre class="log mono" tabindex="0" aria-label={t('nvr.diag.log')}>{d.log.join('\n')}</pre>
		{:else}
			<p class="muted">{t('nvr.diag.logEmpty')}</p>
		{/if}
	</SettingGroup>
{/if}

<style>
	p {
		margin: 0;
	}
	.wrap,
	.code {
		overflow-wrap: anywhere;
		word-break: break-all;
	}
	.code {
		color: var(--muted);
	}
	.log {
		margin: 0;
		max-height: 26rem;
		overflow: auto;
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		font-size: var(--text-sm);
		line-height: 1.5;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		word-break: break-word;
	}
	:global(.sgroup-tools) .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
</style>
