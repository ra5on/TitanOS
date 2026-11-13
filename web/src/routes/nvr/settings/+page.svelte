<script lang="ts">
	// PLAN 79-7 (drawing 79-nvr/entwurf, part 4): the NVR's settings as rows –
	// where recordings go and how much room they may take, which device
	// detects, known faces and plates, the RTSP output for Home Assistant,
	// the cameras' messages (PLAN 82-3), the privacy notice again and the
	// switch for the whole NVR. Every row saves its own change.
	import { api, ApiError } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { formatBytes, formatDecimal } from '#lib/format.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Confirm from '#lib/nvr/Confirm.svelte';
	import FacesRow from '#lib/nvr/FacesRow.svelte';
	import NotifyOverview from '#lib/nvr/NotifyOverview.svelte';
	import { nvr, refreshNvr } from '#lib/nvr/nvr.svelte.ts';
	import PlatesRow from '#lib/nvr/PlatesRow.svelte';
	import { dayClock, decoderName, decoderReason, deviceName } from '#lib/nvr/texts.ts';
	import type { DetectorKind, NvrSettings } from '#lib/nvr/types.ts';
	import { storage } from '#lib/storage.svelte.ts';

	const status = $derived(nvr.status);
	const hardware = $derived(status?.hardware);
	const devices = $derived(hardware?.devices ?? []);
	const store = $derived(status?.storage);
	const volumes = $derived((storage.status?.volumes ?? []).filter((v) => v.mounted));

	let settings = $state<NvrSettings | null>(null);
	let loadError = $state('');
	let limit = $state(0);
	/**
	 * Takes the settings as the NAS has them now. The cap field keeps a value
	 * being typed into it while another row saves or a failed save reads the
	 * settings again; its own save (`own`) takes the NAS's value.
	 */
	function take(next: NvrSettings, own = false) {
		if (own || !settings || Number(limit) === settings.storage.limitGB)
			limit = next.storage.limitGB;
		settings = next;
	}
	async function load() {
		try {
			take(await api.get<NvrSettings>('/nvr/settings'));
			loadError = '';
		} catch (err) {
			loadError = apiErrorText(err);
		}
	}
	$effect(() => {
		void load();
	});

	let busy = $state(false);
	let error = $state('');
	let saved = $state('');
	let ask = $state<null | { kind: 'docker' | 'sleep'; body: Record<string, unknown> }>(null);
	async function save(body: Record<string, unknown>, what: string) {
		busy = true;
		error = '';
		saved = '';
		try {
			take(await api.put<NvrSettings>('/nvr/settings', body), what === 'cap');
			ask = null;
			saved = what;
			await refreshNvr();
		} catch (err) {
			if (
				err instanceof ApiError &&
				err.code === 'nvr_volume_sleeps' &&
				!body.confirmSleepingVolume
			)
				ask = { kind: 'sleep', body };
			else if (err instanceof ApiError && err.code === 'nvr_docker_off' && !body.enableDocker)
				ask = { kind: 'docker', body };
			else {
				ask = null;
				error = apiErrorText(err);
				await load();
			}
		} finally {
			busy = false;
		}
	}

	let askOff = $state(false);
	async function switchOff() {
		busy = true;
		error = '';
		try {
			await api.post('/nvr/disable', {});
			askOff = false;
			await refreshNvr();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	const usedText = $derived.by(() => {
		if (!store || store.usedBytes === null) return t('nvr.storage.usedUnknown');
		const parts = [t('nvr.storage.used', { used: formatBytes(store.usedBytes) })];
		if (store.recordingsBytes != null && store.exportsBytes != null)
			parts.push(
				t('nvr.storage.usedParts', {
					recordings: formatBytes(store.recordingsBytes),
					exports: formatBytes(store.exportsBytes),
				}),
			);
		if (store.spanDays != null)
			parts.push(
				formatDecimal(store.spanDays, 1) === formatDecimal(1, 1)
					? t('nvr.storage.spanOne')
					: t('nvr.storage.span', { days: formatDecimal(store.spanDays, 1) }),
			);
		return parts.join(' · ');
	});
	const capNote = $derived(
		store?.cap === 'cleaning'
			? t('nvr.storage.capCleaning')
			: store?.cap === 'exports_exceed'
				? t('nvr.storage.capExports')
				: '',
	);
	// PLAN 82-4: the GPUs that can decode.
	const decoders = $derived(hardware?.decoders ?? []);
	const reasonText = (reason?: string) =>
		t(
			`nvr.device.reason.${['driver', 'vm', 'plugin'].includes(reason ?? '') ? reason : 'other'}` as MessageKey,
		);
	const host = $derived(typeof location === 'undefined' ? '' : location.hostname);
	let copied = $state(false);
	async function copyPassword() {
		try {
			await navigator.clipboard.writeText(settings?.rtspPassword ?? '');
			copied = true;
			setTimeout(() => (copied = false), 2500);
		} catch {
			/* the password stays readable in the row */
		}
	}
	const decimal = $derived(i18n.locale === 'de' ? ',' : '.');
	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
</script>

<PageHead title={t('nvr.menu.settings')} />

{#if loadError}<p class="notice-error" role="alert">
		{loadError} <button class="btn" onclick={load}>{t('common.retry')}</button>
	</p>{/if}
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#if settings}
	{@const s = settings}
	<SettingGroup title={t('nvr.settings.storage')} id="nvr-storage">
		<SettingRow
			icon="disk"
			title={t('nvr.storage.volume')}
			desc={t('nvr.storage.volumeHint')}
			for="nvr-volume"
		>
			{#snippet control()}
				<select
					id="nvr-volume"
					class="field"
					value={s.volume}
					disabled={busy}
					onchange={(e) => save({ volume: e.currentTarget.value }, 'volume')}
				>
					{#each volumes as v (v.name)}
						<option value={v.name}
							>{t('nvr.storage.volumeFree', {
								name: v.name,
								free: formatBytes(Math.max(0, v.sizeBytes - v.usedBytes)),
							})}</option
						>
					{/each}
					{#if s.volume && !volumes.some((v) => v.name === s.volume)}
						<option value={s.volume}>{s.volume}</option>
					{/if}
				</select>
			{/snippet}
		</SettingRow>
		<SettingRow icon="archive" title={t('nvr.storage.cap')} for="nvr-cap" id="nvr-cap-row">
			{#snippet description()}
				<p>{t('nvr.storage.capHint')}</p>
				<p>{usedText}</p>
				{#if capNote}<p class="warn" id="nvr-cap-note" role="status">
						<Icon name="info" size={14} />
						<span>{capNote}</span>
					</p>{/if}
				{#if store?.lastCleanup}<p>
						{t('nvr.storage.lastCleanup', { time: dayClock(store.lastCleanup, locale) })}
					</p>{/if}
			{/snippet}
			{#snippet control()}
				<label class="unit"
					><input
						id="nvr-cap"
						class="field"
						type="number"
						min="0"
						max="100000"
						step="10"
						bind:value={limit}
					/><span>GB</span></label
				>
				<button
					type="button"
					class="btn"
					disabled={busy || Number(limit) === s.storage.limitGB}
					onclick={() =>
						save({ storage: { limitGB: Math.max(0, Math.round(Number(limit) || 0)) } }, 'cap')}
					>{t('common.save')}</button
				>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('nvr.settings.detection')} id="nvr-detection">
		<SettingRow icon="zap" title={t('nvr.detector.title')} for="nvr-detector" id="nvr-detector-row">
			{#snippet description()}
				<p>
					{t('nvr.detector.found', {
						devices: devices.map((d) => deviceName(d.kind, devices)).join(', ') || '–',
					})}
				</p>
				{#if status?.detector && status.state === 'running'}
					<p>
						{t('nvr.detector.active', { device: deviceName(status.detector.active, devices) })}
						{#if status.detector.inferenceMs !== null}
							· {t('nvr.diag.ms', {
								ms: formatDecimal(status.detector.inferenceMs, 1),
							})}{/if}
					</p>
				{/if}
				{#if s.detector.kind === 'cpu'}<p>{t('nvr.detector.cpuHint')}</p>{/if}
			{/snippet}
			{#snippet control()}
				<select
					id="nvr-detector"
					class="field"
					value={s.detector.kind}
					disabled={busy}
					onchange={(e) =>
						save({ detector: { kind: e.currentTarget.value as DetectorKind } }, 'detector')}
				>
					<option value="auto"
						>{t('nvr.detector.auto', { device: deviceName(hardware?.auto ?? 'cpu') })}</option
					>
					{#each devices as d (d.kind)}
						<option value={d.kind} disabled={!d.available && s.detector.kind !== d.kind}
							>{deviceName(d.kind, devices)}{d.available
								? ''
								: ` – ${reasonText(d.reason)}`}</option
						>
					{/each}
					{#if s.detector.kind !== 'auto' && !devices.some((d) => d.kind === s.detector.kind)}
						<option value={s.detector.kind}>{deviceName(s.detector.kind)}</option>
					{/if}
				</select>
			{/snippet}
		</SettingRow>
		<SettingRow icon="gpu" title={t('nvr.decoder.title')} for="nvr-decoder" id="nvr-decoder-row">
			{#snippet description()}
				<p>{t('nvr.decoder.hint')}</p>
				{#if status?.decoder}
					<p>{t('nvr.decoder.active', { gpu: decoderName(status.decoder.active, decoders) })}</p>
					{#if status.decoder.reason}<p class="warn" id="nvr-decoder-reason" role="status">
							<Icon name="info" size={14} />
							<span>{decoderReason(status.decoder.reason)}</span>
						</p>{/if}
				{/if}
			{/snippet}
			{#snippet control()}
				<select
					id="nvr-decoder"
					class="field"
					value={s.decoder || 'auto'}
					disabled={busy}
					onchange={(e) => save({ decoder: e.currentTarget.value }, 'decoder')}
				>
					<option value="auto">{t('nvr.decoder.auto')}</option>
					{#each decoders as d (d.id)}
						<option value={d.id} disabled={!d.available && s.decoder !== d.id}
							>{decoderName(d.id, decoders)}{d.available || s.decoder === d.id
								? ''
								: ` – ${reasonText(d.reason)}`}</option
						>
					{/each}
					{#if s.decoder && !['auto', 'cpu'].includes(s.decoder) && !decoders.some((d) => d.id === s.decoder)}
						<option value={s.decoder}>{t('nvr.decoder.goneOption', { id: s.decoder })}</option>
					{/if}
					<option value="cpu">{t('nvr.decoder.cpu')}</option>
				</select>
			{/snippet}
		</SettingRow>
		{#if devices.some((d) => d.kind === 'coral' && !d.available && d.reason === 'plugin')}
			<SettingRow
				href="/plugins"
				icon="chip"
				title={t('nvr.detector.coralPlugin')}
				desc={t('nvr.detector.coralPluginHint')}
				summary={t('nvr.detector.coralPluginOpen')}
			/>
		{/if}
		<SettingRow
			href="/nvr/diagnosis"
			icon="activity"
			title={t('nvr.menu.diagnosis')}
			desc={t('nvr.settings.diagnosisHint')}
		/>
	</SettingGroup>

	<SettingGroup title={t('nvr.settings.known')} id="nvr-known">
		<FacesRow />
		<PlatesRow
			plates={s.plates ?? []}
			supported={s.plates !== undefined}
			{busy}
			onsave={(plates) => save({ plates }, 'plates')}
		/>
	</SettingGroup>

	<SettingGroup title={t('nvr.settings.homeAssistant')} id="nvr-ha">
		<SettingRow icon="home" title={t('nvr.rtsp.title')} desc={t('nvr.rtsp.hint')} id="nvr-rtsp">
			{#snippet control()}
				<input
					type="checkbox"
					role="switch"
					class="switch"
					aria-label={t('nvr.rtsp.title')}
					checked={s.rtspOut}
					disabled={busy}
					onchange={(e) => save({ rtspOut: e.currentTarget.checked }, 'rtsp')}
				/>
			{/snippet}
		</SettingRow>
		{#if s.rtspOut}
			<SettingRow icon="key" title={t('nvr.rtsp.login')} id="nvr-rtsp-login">
				{#snippet description()}
					<p>{t('nvr.rtsp.loginHint')}</p>
					<p class="mono wrap">rtsp://{host}:8554/{nvr.cameras[0]?.id ?? 'kamera'}</p>
					<p>
						{t('nvr.rtsp.user')}: <span class="mono">{s.rtspUser}</span> ·
						{t('nvr.rtsp.password')}: <span class="mono wrap">{s.rtspPassword}</span>
					</p>
				{/snippet}
				{#snippet control()}
					<button type="button" class="btn" onclick={copyPassword}
						><Icon name="copy" size={15} />{copied
							? t('nvr.diag.copied')
							: t('nvr.rtsp.copy')}</button
					>
				{/snippet}
			</SettingRow>
		{/if}
	</SettingGroup>

	<NotifyOverview
		cameras={nvr.cameras}
		off={!!s.notifyOff}
		{busy}
		onswitch={(on) => save({ notifyOff: !on }, 'notify')}
	/>

	<SettingGroup title={t('nvr.settings.general')} id="nvr-general">
		<SettingRow
			icon="shield"
			title={t('nvr.privacy.title')}
			desc={t('nvr.privacy.text')}
			id="nvr-privacy-row"
		>
			{#snippet control()}
				{#if s.privacyNoticeDismissed}
					<button
						type="button"
						class="btn"
						disabled={busy}
						onclick={() => save({ privacyNoticeDismissed: false }, 'privacy')}
						>{t('nvr.privacy.showAgain')}</button
					>
				{/if}
			{/snippet}
		</SettingRow>
		<SettingRow
			icon="archive"
			title={t('nvr.settings.backup')}
			desc={t('nvr.settings.backupHint')}
		/>
		<SettingRow
			icon="power"
			title={t('nvr.settings.power')}
			desc={status
				? t('nvr.settings.powerState', {
						state: t(`nvr.diag.stateName.${status.state}` as MessageKey),
						cameras: status.cameras.enabled,
					}) +
					(status.detector?.inferenceMs != null
						? ` · ${t('nvr.diag.ms', { ms: formatDecimal(status.detector.inferenceMs, 1).replace('.', decimal) })}`
						: '')
				: ''}
			id="nvr-power"
		>
			{#snippet control()}
				<button
					type="button"
					class="btn btn-danger-outline"
					disabled={busy}
					onclick={() => (askOff = true)}>{t('nvr.settings.off')}</button
				>
			{/snippet}
		</SettingRow>
	</SettingGroup>
	{#if saved}<p class="muted" role="status">{t('nvr.settings.saved')}</p>{/if}
{/if}

{#if ask?.kind === 'sleep'}
	<Confirm
		title={t('nvr.setup.sleepTitle')}
		text={t('nvr.setup.sleepText', { volume: String(ask.body.volume ?? '') })}
		confirm={t('nvr.setup.sleepOk')}
		{busy}
		onconfirm={() => ask && save({ ...ask.body, confirmSleepingVolume: true }, 'volume')}
		onclose={() => ((ask = null), load())}
	/>
{:else if ask?.kind === 'docker'}
	<Confirm
		title={t('nvr.setup.dockerTitle')}
		text={t('nvr.setup.dockerText')}
		confirm={t('nvr.setup.dockerOn')}
		{busy}
		onconfirm={() => ask && save({ ...ask.body, enableDocker: true }, 'docker')}
		onclose={() => ((ask = null), load())}
	/>
{/if}
{#if askOff}
	<Confirm
		title={t('nvr.settings.offTitle')}
		text={[t('nvr.settings.offText'), t('nvr.settings.offKeeps')]}
		confirm={t('nvr.settings.off')}
		danger
		{busy}
		{error}
		onconfirm={switchOff}
		onclose={() => (askOff = false)}
	/>
{/if}

<style>
	p {
		margin: 0;
	}
	.unit {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
	}
	.warn {
		display: flex;
		align-items: flex-start;
		gap: var(--space-1-5);
		color: var(--warn-text);
	}
	.warn :global(svg) {
		flex: none;
		margin-top: 3px;
	}
	.wrap {
		overflow-wrap: anywhere;
		word-break: break-all;
	}
	:global(.srow-ctl) .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
</style>
