<script lang="ts">
	// Fernansicht (PLAN 63, draft 02): how Docker apps are shown in windows –
	// GPU, quality, frame rate, sound, the volume for the apps' browser
	// profiles – and what runs now (viewer program, open views, room for more).
	import { onMount } from 'svelte';
	import { api, type StorageStatus } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
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

{#if draft && status}
	<form class="tile panel" onsubmit={save} aria-labelledby="viewer-options">
		<h2 class="tile-title" id="viewer-options">{t('viewer.settings.options')}</h2>
		<div class="rows">
			<div class="row">
				<div class="label">
					<label for="viewer-gpu"><b>{t('viewer.settings.gpu')}</b></label>
					<p class="muted hint">{t('viewer.settings.gpuHint')}</p>
				</div>
				<div class="control">
					<select id="viewer-gpu" class="field" bind:value={draft.gpu} onchange={changed}>
						<option value="auto"
							>{gpuName
								? t('viewer.settings.gpuAutoWith', { name: gpuName })
								: t('viewer.settings.gpuAuto')}</option
						>
						{#each status.gpus as gpu (gpu.id)}
							<option value={gpu.id} disabled={!gpu.available && draft.gpu !== gpu.id}
								>{gpuLabel(gpu)}</option
							>
						{/each}
						{#if draft.gpu !== 'auto' && draft.gpu !== 'off' && !status.gpus.some((g) => g.id === draft?.gpu)}
							<option value={draft.gpu}>{draft.gpu}</option>
						{/if}
						<option value="off">{t('viewer.settings.gpuOff')}</option>
					</select>
					<p class="muted hint">
						{t('viewer.settings.encoder', { gpu: t(`viewer.gpu.${status.gpu}` as MessageKey) })}
					</p>
				</div>
			</div>
			<div class="row">
				<div class="label">
					<b id="viewer-quality">{t('viewer.quality')}</b>
					<p class="muted hint">{t('viewer.settings.qualityHint')}</p>
				</div>
				<div class="control">
					<Segments
						options={qualityOptions}
						bind:value={draft.quality}
						label={t('viewer.quality')}
						onchange={(_: Quality) => changed()}
					/>
				</div>
			</div>
			<div class="row">
				<div class="label"><b>{t('viewer.fps')}</b></div>
				<div class="control">
					<Segments
						options={[...fpsOptions]}
						value={String(draft.fps) as '30' | '60'}
						label={t('viewer.fps')}
						onchange={(v) => {
							if (draft) draft.fps = v === '30' ? 30 : 60;
							changed();
						}}
					/>
				</div>
			</div>
			<div class="row">
				<div class="label">
					<b>{t('viewer.settings.transport')}</b>
					<p class="muted hint">{t('viewer.settings.transportHint')}</p>
				</div>
				<div class="control">
					<Segments
						options={[
							{ value: 'auto', label: t('viewer.transport.auto') },
							{ value: 'video', label: t('viewer.transport.video') },
						]}
						bind:value={draft.transport}
						label={t('viewer.settings.transport')}
						onchange={() => changed()}
					/>
				</div>
			</div>
			<div class="row">
				<div class="label">
					<b>{t('viewer.settings.audio')}</b>
					<p class="muted hint">{t('viewer.settings.audioHint')}</p>
				</div>
				<label class="check"
					><input type="checkbox" bind:checked={draft.audio} onchange={changed} />
					{t('viewer.settings.audioCheck')}</label
				>
			</div>
			<div class="row">
				<div class="label"><b>{t('viewer.settings.views')}</b></div>
				<p>
					{t('viewer.settings.viewsAll')}{#if status.memory.more >= 0}
						<span class="muted">
							&nbsp;·
							{status.memory.perViewBytes > 0
								? t('viewer.settings.viewsNow', {
										open: status.sessions.length,
										room: status.memory.more,
										per: formatBytes(status.memory.perViewBytes),
									})
								: t('viewer.settings.viewsNowShort', {
										open: status.sessions.length,
										room: status.memory.more,
									})}</span
						>{/if}
				</p>
			</div>
			<div class="row">
				<div class="label">
					<label for="viewer-volume"><b>{t('viewer.settings.profiles')}</b></label>
				</div>
				<div class="control">
					<select id="viewer-volume" class="field" bind:value={draft.volume} onchange={changed}>
						<option value="">{t('viewer.settings.profilesDocker')}</option>
						{#each volumes as name (name)}
							<option value={name}>{t('viewer.settings.volumeName', { name })}</option>
						{/each}
						{#if draft.volume && !volumes.includes(draft.volume)}
							<option value={draft.volume}
								>{t('viewer.settings.volumeName', { name: draft.volume })}</option
							>
						{/if}
					</select>
					<p class="muted hint">{t('viewer.settings.profilesHint')}</p>
				</div>
			</div>
			<div class="row">
				<div class="label"><b>{t('viewer.settings.idle')}</b></div>
				<p>
					{idleText}
					<a href="/settings/access#access-terminal">{t('viewer.settings.idleChange')}</a>
				</p>
			</div>
		</div>
		<div class="row-actions">
			<button type="submit" class="btn btn-primary" disabled={busy || !dirty}
				>{t('common.save')}</button
			>
			{#if saved}<span class="muted" role="status">{t('viewer.settings.saved')}</span>{/if}
			{#if error}<span class="error-text" role="alert">{error}</span>{/if}
		</div>
	</form>

	<section class="tile panel" aria-labelledby="viewer-status">
		<h2 class="tile-title" id="viewer-status">{t('viewer.settings.status')}</h2>
		<div class="rows">
			<div class="row">
				<div class="label"><b>{t('viewer.settings.image')}</b></div>
				<div class="control">
					<p role="status">
						<span
							class="chip {image?.state === 'ready'
								? 'ok'
								: image?.state === 'error'
									? 'error'
									: 'info'}">{imageText}</span
						>
					</p>
					{#if (image?.state === 'downloading' || image?.state === 'loading') && image.size > 0}
						<progress max={image.size} value={image.bytes}></progress>
					{/if}
					{#if image?.state === 'missing' || image?.state === 'error'}
						<button type="button" class="btn" disabled={loading} onclick={loadImage}
							>{t('viewer.settings.loadNow')}</button
						>
					{/if}
				</div>
			</div>
			<div class="row">
				<div class="label">
					<b>{t('viewer.settings.openViews')}</b>
					{#if status.sessions.length}<p class="muted hint">{formatBytes(memoryOpen)}</p>{/if}
				</div>
				{#if status.sessions.length}
					<ul class="views">
						{#each status.sessions as view, i (i)}
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
				{:else}<p class="muted">{t('viewer.settings.noViews')}</p>{/if}
			</div>
		</div>
	</section>
{/if}

<style>
	.rows {
		display: flex;
		flex-direction: column;
		container-type: inline-size;
	}
	.row {
		display: grid;
		grid-template-columns: minmax(12rem, 1fr) 2fr;
		gap: var(--space-2) var(--space-5);
		align-items: start;
		padding: var(--space-3) 0;
		border-top: 1px solid var(--panel-border);
	}
	.row:first-child {
		border-top: 0;
		padding-top: 0;
	}
	.label b {
		font-weight: 600;
	}
	.hint {
		margin: var(--space-1) 0 0;
		font-size: var(--text-sm);
		hyphens: auto;
		overflow-wrap: break-word;
	}
	.row > p {
		margin: 0;
	}
	.control {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		align-items: flex-start;
		min-width: 0;
	}
	.control .field {
		max-width: 26rem;
		width: 100%;
	}
	.control progress {
		width: min(20rem, 100%);
	}
	.control .chip {
		white-space: normal;
		height: auto;
		line-height: 1.4;
		padding-block: 2px;
	}
	.views {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.views li {
		display: flex;
		gap: var(--space-3);
		justify-content: space-between;
		max-width: 26rem;
	}
	.error-text {
		color: var(--error-text);
	}
	.muted {
		color: var(--muted);
	}
	@container (max-width: 560px) {
		.row {
			grid-template-columns: 1fr;
		}
	}
</style>
