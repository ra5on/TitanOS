<script lang="ts">
	// PLAN 79-7: the first start of the NVR, as rows – the privacy notice,
	// where recordings go, which device detects, how much room recordings may
	// take. "NVR einschalten" then loads Frigate's image; the live page shows
	// the progress and offers "Kamera hinzufügen". No configuration file.
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Confirm from '#lib/nvr/Confirm.svelte';
	import { nvr, refreshNvr } from '#lib/nvr/nvr.svelte.ts';
	import { deviceName } from '#lib/nvr/texts.ts';
	import type { DetectorKind, NvrSettings } from '#lib/nvr/types.ts';
	import { storage } from '#lib/storage.svelte.ts';

	const status = $derived(nvr.status);
	const hardware = $derived(status?.hardware);
	const devices = $derived(hardware?.devices ?? []);
	const coralNoPlugin = $derived(
		devices.some((d) => d.kind === 'coral' && !d.available && d.reason === 'plugin'),
	);
	const volumes = $derived((storage.status?.volumes ?? []).filter((v) => v.mounted));

	let volume = $state('');
	let detector = $state<DetectorKind>('auto');
	let limitGB = $state(0);
	let privacySeen = $state(false);
	let loaded = $state(false);
	$effect(() => {
		if (loaded) return;
		api.get<NvrSettings>('/nvr/settings').then(
			(s) => {
				volume = s.volume;
				detector = s.detector.kind;
				limitGB = s.storage.limitGB;
				privacySeen = s.privacyNoticeDismissed;
				loaded = true;
			},
			() => (loaded = true),
		);
	});
	// Without a choice yet: the first volume.
	$effect(() => {
		if (loaded && !volume && volumes.length) volume = volumes[0].name;
	});

	let busy = $state(false);
	let error = $state('');
	let ask = $state<'' | 'docker' | 'sleep'>('');
	let enableDocker = false;
	let confirmSleepingVolume = false;

	async function dismissPrivacy() {
		privacySeen = true;
		try {
			await api.put('/nvr/settings', { privacyNoticeDismissed: true });
		} catch {
			// Asked again with "NVR einschalten".
		}
	}

	async function enable() {
		busy = true;
		error = '';
		try {
			await api.put('/nvr/settings', {
				detector: { kind: detector },
				storage: { limitGB: Math.max(0, Math.round(Number(limitGB) || 0)) },
				privacyNoticeDismissed: privacySeen,
			});
			await api.post('/nvr/enable', { volume, enableDocker, confirmSleepingVolume });
			ask = '';
			await refreshNvr();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'nvr_docker_off' && !enableDocker) ask = 'docker';
			else if (
				err instanceof ApiError &&
				err.code === 'nvr_volume_sleeps' &&
				!confirmSleepingVolume
			)
				ask = 'sleep';
			else {
				ask = '';
				error = apiErrorText(err);
			}
		} finally {
			busy = false;
		}
	}
	function start() {
		enableDocker = false;
		confirmSleepingVolume = false;
		void enable();
	}
	function confirmed() {
		if (ask === 'docker') enableDocker = true;
		if (ask === 'sleep') confirmSleepingVolume = true;
		void enable();
	}

	const reasonText = (reason?: string) =>
		t(
			`nvr.device.reason.${['driver', 'vm', 'plugin'].includes(reason ?? '') ? reason : 'other'}` as MessageKey,
		);
</script>

<PageHead title={t('nvr.setup.title')}>
	<p class="muted">{t('nvr.setup.intro')}</p>
</PageHead>

<SettingGroup title={t('nvr.setup.before')} id="nvr-setup-privacy">
	<SettingRow icon="shield" title={t('nvr.privacy.title')} desc={t('nvr.privacy.text')}>
		{#snippet control()}
			{#if privacySeen}<span class="chip ok">{t('nvr.privacy.read')}</span>
			{:else}<button type="button" class="btn" onclick={dismissPrivacy}
					>{t('nvr.privacy.dismiss')}</button
				>{/if}
		{/snippet}
	</SettingRow>
</SettingGroup>

<SettingGroup title={t('nvr.setup.choices')} id="nvr-setup-choices">
	<SettingRow
		icon="disk"
		title={t('nvr.storage.volume')}
		desc={t('nvr.storage.volumeHint')}
		for="nvr-setup-volume"
	>
		{#snippet control()}
			{#if volumes.length}
				<select id="nvr-setup-volume" class="field" bind:value={volume}>
					{#each volumes as v (v.name)}
						<option value={v.name}
							>{v.sizeBytes > 0
								? t('nvr.storage.volumeFree', {
										name: v.name,
										free: formatBytes(Math.max(0, v.sizeBytes - v.usedBytes)),
									})
								: v.name}</option
						>
					{/each}
				</select>
			{:else}
				<span>{status?.volume || t('nvr.storage.noVolumes')}</span>
			{/if}
		{/snippet}
	</SettingRow>
	<SettingRow
		icon="zap"
		title={t('nvr.detector.title')}
		desc={t('nvr.detector.found', {
			devices: devices.map((d) => deviceName(d.kind, devices)).join(', ') || '–',
		})}
		for="nvr-setup-detector"
	>
		{#snippet control()}
			<select id="nvr-setup-detector" class="field" bind:value={detector}>
				<option value="auto"
					>{t('nvr.detector.auto', { device: deviceName(hardware?.auto ?? 'cpu') })}</option
				>
				{#each devices as d (d.kind)}
					<option value={d.kind} disabled={!d.available}
						>{deviceName(d.kind, devices)}{d.available ? '' : ` – ${reasonText(d.reason)}`}</option
					>
				{/each}
			</select>
		{/snippet}
	</SettingRow>
	{#if coralNoPlugin}
		<SettingRow
			href="/plugins"
			icon="chip"
			title={t('nvr.detector.coralPlugin')}
			desc={t('nvr.detector.coralPluginHint')}
			summary={t('nvr.detector.coralPluginOpen')}
		/>
	{/if}
	{#if detector === 'cpu'}
		<p class="muted note">{t('nvr.detector.cpuHint')}</p>
	{/if}
	<SettingRow
		icon="archive"
		title={t('nvr.storage.cap')}
		desc={t('nvr.storage.capHint')}
		for="nvr-setup-cap"
	>
		{#snippet control()}
			<label class="unit"
				><input
					id="nvr-setup-cap"
					class="field"
					type="number"
					min="0"
					max="100000"
					step="10"
					bind:value={limitGB}
				/><span>GB</span></label
			>
		{/snippet}
	</SettingRow>
	{#if hardware && !hardware.enrichmentsPossible}
		<p class="muted note">{t('nvr.setup.enrichNo')}</p>
	{/if}
	<SettingRow icon="download" title={t('nvr.setup.image')} desc={t('nvr.setup.imageHint')} />
</SettingGroup>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
<div class="actions">
	<button type="button" class="btn btn-primary" disabled={busy || !loaded} onclick={start}
		>{t('nvr.setup.enable')}</button
	>
</div>

{#if ask === 'docker'}
	<Confirm
		title={t('nvr.setup.dockerTitle')}
		text={t('nvr.setup.dockerText')}
		confirm={t('nvr.setup.dockerOn')}
		{busy}
		onconfirm={confirmed}
		onclose={() => (ask = '')}
	/>
{:else if ask === 'sleep'}
	<Confirm
		title={t('nvr.setup.sleepTitle')}
		text={t('nvr.setup.sleepText', { volume })}
		confirm={t('nvr.setup.sleepOk')}
		{busy}
		onconfirm={confirmed}
		onclose={() => (ask = '')}
	/>
{/if}

<style>
	.note {
		margin: 0;
	}
	.unit {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
	}
</style>
