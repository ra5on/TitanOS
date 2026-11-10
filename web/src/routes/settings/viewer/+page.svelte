<script lang="ts">
	// Fernansicht (PLAN 63, draft 02): how Docker apps are shown in windows –
	// GPU, quality, frame rate, sound, the volume for the apps' browser
	// profiles – and what runs now (viewer program, open views, room for more).
	import { onMount } from 'svelte';
	import { api, type StorageStatus } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { poll } from '#lib/poll.ts';
	import {
		QUALITIES,
		sanitizeImage,
		sanitizeSettings,
		sanitizeStatus,
		type Quality,
		type ViewerSettings,
		type ViewerGpu,
		type ViewerStatus,
	} from '#lib/desktop/remote.ts';

	let draft = $state<ViewerSettings | null>(null);
	let status = $state<ViewerStatus | null>(null);
	let volumes = $state<string[]>([]);
	let idleMinutes = $state(-1);
	let loadError = $state('');
	let error = $state('');
	let busy = $state(false);
	let saved = $state(false);
	let dirty = $state(false);
	let loading = $state(false);

	async function load() {
		loadError = '';
		try {
			const [settings, state] = await Promise.all([
				api.get('/viewer/settings'),
				api.get('/viewer/status'),
			]);
			draft = sanitizeSettings(settings);
			status = sanitizeStatus(state);
			dirty = false;
		} catch (err) {
			loadError = apiErrorText(err);
		}
		// Volumes and the consoles' idle time only fill in choices and a line.
		api
			.get<StorageStatus>('/storage')
			.then(
				(s) =>
					(volumes = (s.volumes ?? [])
						.filter((v) => v.mounted !== false)
						.map((v) => v.name)
						.filter(Boolean)),
			)
			.catch(() => (volumes = []));
		if (status && status.idleMinutes >= 0) idleMinutes = status.idleMinutes;
		else
			api
				.get<{ idleMinutes?: number }>('/terminal')
				.then((v) => (idleMinutes = typeof v.idleMinutes === 'number' ? v.idleMinutes : 15))
				.catch(() => (idleMinutes = -1));
	}

	async function refresh() {
		if (!status) return;
		try {
			status = sanitizeStatus(await api.get('/viewer/status', AbortSignal.timeout(8000)));
		} catch {
			// Keep the last state; the next round tries again.
		}
	}

	onMount(() => {
		void load();
		// The status lives in RAM on the NAS; while the program loads, its progress moves.
		return poll(refresh, { interval: 5000 });
	});

	function changed() {
		dirty = true;
		saved = false;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		busy = true;
		error = '';
		try {
			draft = sanitizeSettings(await api.put('/viewer/settings', draft));
			dirty = false;
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function loadImage() {
		loading = true;
		error = '';
		try {
			const next = sanitizeImage(await api.post('/viewer/image'));
			if (status) status.image = next;
			await refresh();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			loading = false;
		}
	}

	const gpuName = $derived(status?.gpus.find((g) => g.available)?.title ?? '');
	function gpuLabel(gpu: ViewerGpu): string {
		const name = `${gpu.title} (${gpu.id})`;
		if (gpu.available) return name;
		if (gpu.reason === 'vm') return t('viewer.settings.gpuVm', { name });
		if (gpu.reason === 'driver') return t('viewer.settings.gpuDriver', { name });
		return t('viewer.settings.gpuUnavailable', { name });
	}
	function sessionState(state: string): string {
		return ['starting', 'open', 'idle'].includes(state)
			? t(`viewer.settings.state.${state}` as MessageKey)
			: state;
	}
	const qualityOptions = $derived(
		QUALITIES.map((q) => ({ value: q, label: t(`viewer.q.${q}` as MessageKey) })),
	);
	const fpsOptions = [
		{ value: '30', label: '30 fps' },
		{ value: '60', label: '60 fps' },
	] as const;
	const memoryOpen = $derived(status?.sessions.reduce((sum, v) => sum + v.memoryBytes, 0) ?? 0);
	const image = $derived(status?.image);
	const imageText = $derived.by(() => {
		if (!image) return '';
		if (image.state === 'ready')
			return image.version
				? t('viewer.settings.imageReadyVersion', { version: image.version })
				: t('viewer.settings.imageReady');
		const progress = image.size
			? `${formatPercent(100 * image.progress)} (${formatBytes(image.bytes)} / ${formatBytes(image.size)})`
			: '…';
		if (image.state === 'downloading') return t('viewer.settings.imageDownloading', { progress });
		if (image.state === 'loading') return t('viewer.settings.imageLoading', { progress });
		if (image.state === 'error') return t('viewer.settings.imageError', { error: image.error });
		return t('viewer.settings.imageMissing');
	});
	const idleText = $derived(
		idleMinutes < 0
			? ''
			: t('viewer.settings.idleText', {
					value:
						idleMinutes === 0
							? t('viewer.settings.idleNever')
							: t('viewer.settings.idleMinutes', { minutes: idleMinutes }),
				}),
	);
</script>

<PageHead title={t('viewer.settings.title')}>
	<p class="muted">{t('viewer.settings.intro')}</p>
</PageHead>

{#if loadError}<p class="notice-error" role="alert">
		{loadError} <button class="btn" onclick={load}>{t('common.retry')}</button>
	</p>{/if}

{#snippet openViews()}
	<div class="srow-block">
		<ul class="views">
			{#each status?.sessions ?? [] as view, i (i)}
				<li>
					<span
						>{t('viewer.settings.sessionRow', {
							app: view.app,
							user: view.user,
							state: sessionState(view.state),
						})}</span
					>
					<span class="muted mono">{formatBytes(view.memoryBytes)}</span>
				</li>
			{/each}
		</ul>
	</div>
{/snippet}

{#if draft && status}
	{@const d = draft}
	{@const st = status}
	<!-- PLAN 78-1: every option is a row; one Save for all of them (the
	     request carries the whole set). -->
	<form onsubmit={save}>
		<SettingGroup title={t('viewer.settings.options')} id="viewer-options">
			<SettingRow icon="gpu" title={t('viewer.settings.gpu')} for="viewer-gpu">
				{#snippet description()}
					<p>{t('viewer.settings.gpuHint')}</p>
					<p>
						{t('viewer.settings.encoder', { gpu: t(`viewer.gpu.${st.gpu}` as MessageKey) })}
					</p>
				{/snippet}
				{#snippet control()}
					<select id="viewer-gpu" class="field" bind:value={d.gpu} onchange={changed}>
						<option value="auto"
							>{gpuName
								? t('viewer.settings.gpuAutoWith', { name: gpuName })
								: t('viewer.settings.gpuAuto')}</option
						>
						{#each st.gpus as gpu (gpu.id)}
							<option value={gpu.id} disabled={!gpu.available && d.gpu !== gpu.id}
								>{gpuLabel(gpu)}</option
							>
						{/each}
						{#if d.gpu !== 'auto' && d.gpu !== 'off' && !st.gpus.some((g) => g.id === d.gpu)}
							<option value={d.gpu}>{d.gpu}</option>
						{/if}
						<option value="off">{t('viewer.settings.gpuOff')}</option>
					</select>
				{/snippet}
			</SettingRow>
			<SettingRow icon="image" title={t('viewer.quality')} desc={t('viewer.settings.qualityHint')}>
				{#snippet control()}
					<Segments
						options={qualityOptions}
						bind:value={d.quality}
						label={t('viewer.quality')}
						onchange={(_: Quality) => changed()}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="video" title={t('viewer.fps')}>
				{#snippet control()}
					<Segments
						options={[...fpsOptions]}
						value={String(d.fps) as '30' | '60'}
						label={t('viewer.fps')}
						onchange={(v) => {
							d.fps = v === '30' ? 30 : 60;
							changed();
						}}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="globe"
				title={t('viewer.settings.transport')}
				desc={t('viewer.settings.transportHint')}
			>
				{#snippet control()}
					<Segments
						options={[
							{ value: 'auto', label: t('viewer.transport.auto') },
							{ value: 'video', label: t('viewer.transport.video') },
						]}
						bind:value={d.transport}
						label={t('viewer.settings.transport')}
						onchange={() => changed()}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="volume"
				title={t('viewer.settings.audio')}
				desc={t('viewer.settings.audioHint')}
			>
				{#snippet control()}
					<input
						type="checkbox"
						role="switch"
						class="switch"
						aria-label={t('viewer.settings.audioCheck')}
						bind:checked={d.audio}
						onchange={changed}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="windows" title={t('viewer.settings.views')}>
				{#snippet description()}
					{t('viewer.settings.viewsAll')}{#if st.memory.more >= 0}
						&nbsp;·
						{st.memory.perViewBytes > 0
							? t('viewer.settings.viewsNow', {
									open: st.sessions.length,
									room: st.memory.more,
									per: formatBytes(st.memory.perViewBytes),
								})
							: t('viewer.settings.viewsNowShort', {
									open: st.sessions.length,
									room: st.memory.more,
								})}{/if}
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="disk"
				title={t('viewer.settings.profiles')}
				desc={t('viewer.settings.profilesHint')}
				for="viewer-volume"
			>
				{#snippet control()}
					<select id="viewer-volume" class="field" bind:value={d.volume} onchange={changed}>
						<option value="">{t('viewer.settings.profilesDocker')}</option>
						{#each volumes as name (name)}
							<option value={name}>{t('viewer.settings.volumeName', { name })}</option>
						{/each}
						{#if d.volume && !volumes.includes(d.volume)}
							<option value={d.volume}>{t('viewer.settings.volumeName', { name: d.volume })}</option
							>
						{/if}
					</select>
				{/snippet}
			</SettingRow>
			<!-- The idle time is set with the consoles: this row leads there. -->
			<SettingRow
				href="/settings/access#access-terminal"
				icon="clock"
				title={t('viewer.settings.idle')}
				desc={t('viewer.settings.idleChange')}
				summary={idleText}
			/>
			<div class="row-actions">
				<button type="submit" class="btn btn-primary" disabled={busy || !dirty}
					>{t('common.save')}</button
				>
				{#if saved}<span class="muted" role="status">{t('viewer.settings.saved')}</span>{/if}
				{#if error}<span class="error-text" role="alert">{error}</span>{/if}
			</div>
		</SettingGroup>
	</form>

	<SettingGroup title={t('viewer.settings.status')} id="viewer-status">
		<SettingRow icon="download" title={t('viewer.settings.image')}>
			{#snippet control()}
				<span role="status"
					><span
						class="chip {image?.state === 'ready'
							? 'ok'
							: image?.state === 'error'
								? 'error'
								: 'info'}">{imageText}</span
					></span
				>
				{#if (image?.state === 'downloading' || image?.state === 'loading') && image.size > 0}
					<progress max={image.size} value={image.bytes}></progress>
				{/if}
				{#if image?.state === 'missing' || image?.state === 'error'}
					<button type="button" class="btn" disabled={loading} onclick={loadImage}
						>{t('viewer.settings.loadNow')}</button
					>
				{/if}
			{/snippet}
		</SettingRow>
		<SettingRow
			id="viewer-open"
			icon="monitor"
			title={t('viewer.settings.openViews')}
			summary={st.sessions.length
				? `${st.sessions.length} · ${formatBytes(memoryOpen)}`
				: t('viewer.settings.noViews')}
			open
			children={st.sessions.length ? openViews : undefined}
		/>
	</SettingGroup>
{/if}

<style>
	form {
		display: contents;
	}
	p {
		margin: 0;
	}
	.row-actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2-5);
		margin-top: var(--space-2);
	}
	progress {
		width: min(20rem, 100%);
	}
	.chip {
		white-space: normal;
		height: auto;
		line-height: 1.4;
		padding-block: 2px;
		text-align: left;
	}
	.views {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.views li {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-3);
		justify-content: space-between;
	}
	.error-text {
		color: var(--error-text);
	}
</style>
