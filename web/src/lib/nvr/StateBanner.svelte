<script lang="ts">
	// PLAN 79-7: what the NVR is doing when it is not simply running – the
	// image is loading, Docker is off, the volume is gone, the detection moved
	// to another device or nothing works. Administrators get the cause and
	// the way out; other accounts one sentence.
	import { api, ApiError } from '../api.ts';
	import BackendDetail from '../components/BackendDetail.svelte';
	import { formatBytes } from '../format.ts';
	import { apiErrorText, i18n, t } from '../i18n/index.svelte.ts';
	import Confirm from './Confirm.svelte';
	import { nvr, nvrAdmin, refreshNvr } from './nvr.svelte.ts';
	import { deviceName, reasonAdvice, reasonTitle, sinceText } from './texts.ts';

	let {
		/** The live view shows only what stops the pictures. */
		compact = false,
	}: { compact?: boolean } = $props();

	const status = $derived(nvr.status);
	const admin = $derived(nvrAdmin());
	const failover = $derived(status?.detector?.failover ?? null);
	const devices = $derived(status?.hardware?.devices ?? []);
	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');

	let busy = $state(false);
	let error = $state('');
	let askDocker = $state(false);

	async function retry() {
		busy = true;
		error = '';
		try {
			await api.post('/nvr/detector/retry', {});
			await refreshNvr();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	async function enable(enableDocker: boolean) {
		busy = true;
		error = '';
		try {
			await api.post('/nvr/enable', { volume: status?.volume ?? '', enableDocker });
			askDocker = false;
			await refreshNvr();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'nvr_docker_off' && !enableDocker)
				askDocker = true;
			else error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

{#if status?.enabled}
	{#if !admin}
		{#if status.state !== 'running'}
			<p class="nvr-banner" role="status">{t('nvr.state.userDown')}</p>
		{/if}
	{:else if status.state === 'pulling'}
		<div class="nvr-banner" role="status">
			<div class="text">
				<strong>{t('nvr.state.pulling')}</strong>
				<span
					>{status.image && status.image.size > 0
						? t('nvr.state.pullingProgress', {
								percent: Math.round((status.image.progress || 0) * 100),
								done: formatBytes(status.image.bytes),
								size: formatBytes(status.image.size),
							})
						: t('nvr.state.pullingStart')}</span
				>
			</div>
			{#if status.image && status.image.size > 0}
				<progress max="1" value={status.image.progress || 0} aria-label={t('nvr.state.pulling')}
				></progress>
			{/if}
		</div>
	{:else if status.state === 'starting' || status.state === 'docker_starting'}
		<p class="nvr-banner" role="status">{t('nvr.state.starting')}</p>
	{:else if status.state === 'docker_off'}
		<div class="nvr-banner warn" role="status">
			<div class="text">
				<strong>{t('nvr.state.dockerOff')}</strong><span>{t('nvr.state.dockerOffHint')}</span>
			</div>
			<button type="button" class="btn" disabled={busy} onclick={() => (askDocker = true)}
				>{t('nvr.setup.dockerOn')}</button
			>
		</div>
	{:else if status.state === 'no_volume'}
		<div class="nvr-banner warn" role="status">
			<div class="text">
				<strong>{t('nvr.state.noVolume', { volume: status.volume ?? '' })}</strong>
				<span>{t('nvr.state.noVolumeHint')}</span>
			</div>
			<a class="btn" href="/nvr/settings">{t('nvr.menu.settings')}</a>
		</div>
	{:else if status.state === 'stopped'}
		<div class="nvr-banner error" role="alert">
			<div class="text">
				<strong>{t('nvr.state.stopped')}</strong>
				<span
					>{reasonTitle(status.error?.code ?? '')}
					{reasonAdvice(status.error?.code ?? '')}</span
				>
			</div>
			<a class="btn" href="/nvr/diagnosis">{t('nvr.menu.diagnosis')}</a>
			<button type="button" class="btn" disabled={busy} onclick={retry}>{t('nvr.retry')}</button>
		</div>
	{:else if status.state === 'error'}
		<div class="nvr-banner error" role="alert">
			<div class="text">
				<strong>{t('nvr.state.error')}</strong>
				<span>{t('nvr.state.errorHint')}<BackendDetail text={status.error?.detail} /></span>
			</div>
			<a class="btn" href="/nvr/diagnosis">{t('nvr.menu.diagnosis')}</a>
		</div>
	{:else if failover && !compact}
		<div class="nvr-banner warn" role="status" id="nvr-failover">
			<div class="text">
				<strong
					>{t('nvr.failover.title', {
						to: deviceName(failover.to, devices),
						from: deviceName(failover.from, devices),
					})}</strong
				>
				<span
					>{reasonTitle(failover.reason)}
					{t('nvr.failover.since', { time: sinceText(failover.at, locale) })}
					{t('nvr.failover.recording')}</span
				>
			</div>
			<a class="btn" href="/nvr/diagnosis">{t('nvr.menu.diagnosis')}</a>
			<button type="button" class="btn" disabled={busy} onclick={retry}>{t('nvr.retry')}</button>
		</div>
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{/if}

{#if askDocker}
	<Confirm
		title={t('nvr.setup.dockerTitle')}
		text={t('nvr.setup.dockerText')}
		confirm={t('nvr.setup.dockerOn')}
		{busy}
		{error}
		onconfirm={() => enable(true)}
		onclose={() => (askDocker = false)}
	/>
{/if}

<style>
	.nvr-banner {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		margin: 0;
		padding: var(--space-2-5) var(--space-4);
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		font-size: var(--text-base);
		min-width: 0;
	}
	.nvr-banner.warn {
		border-color: var(--warn-border);
		background: var(--warn-bg);
	}
	.nvr-banner.error {
		border-color: var(--error-border);
		background: var(--error-bg);
	}
	.text {
		display: flex;
		flex-direction: column;
		gap: 2px;
		flex: 1 1 16rem;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.text span {
		color: var(--muted);
	}
	progress {
		flex: 1 1 100%;
		width: 100%;
	}
</style>
