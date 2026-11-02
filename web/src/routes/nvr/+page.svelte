<script lang="ts">
	// NVR: cameras, live view, recordings. Decisions: docs/rules/NVR.md.
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { api } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import CameraDialog from './CameraDialog.svelte';
	import QualityDialog from './QualityDialog.svelte';
	import {
		clockOf,
		fileOf,
		modeOf,
		pictureUrl,
		type Camera,
		type NvrEvent,
		type NvrState,
		type Recordings,
	} from './nvr.ts';

	let nvr = $state<NvrState | null>(null);
	let error = $state('');
	let editing = $state<Camera | 'new' | null>(null);
	let removing = $state<Camera | null>(null);
	let live = $state('');
	let browsing = $state('');
	let recordings = $state<Recordings | null>(null);
	let day = $state('');
	let playing = $state('');
	let playingProtected = $state(false);
	let eventsOf = $state('');
	let events = $state<NvrEvent[]>([]);
	let busy = $state(false);
	let timing = $state<Camera | null>(null);
	let tuning = $state<Camera | null>(null);
	let timeDone = $state('');

	// General settings, edited in place.
	let volume = $state('');
	let keepDays = $state(7);
	let eventKeepDays = $state(30);
	let reserveGB = $state(20);
	let touched = false;

	async function load() {
		try {
			nvr = await api.get<NvrState>('/nvr');
			error = '';
			if (!touched) {
				volume = nvr.settings.volume ?? '';
				keepDays = nvr.settings.keepDays;
				eventKeepDays = nvr.settings.eventKeepDays || 30;
				reserveGB = nvr.settings.reserveGB;
			}
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	onMount(() => poll(load, { interval: 5000 }));

	async function saveSettings() {
		busy = true;
		try {
			nvr = await api.put<NvrState>('/nvr/settings', {
				volume,
				keepDays,
				eventKeepDays,
				reserveGB,
			});
			touched = false;
			error = '';
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	async function arm(on: boolean) {
		if (!nvr) return;
		busy = true;
		try {
			nvr = await api.put<NvrState>('/nvr/arming', {
				...nvr.settings.arming,
				mode: on ? 'on' : 'off',
			});
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (!removing) return;
		busy = true;
		try {
			nvr = await api.delete<NvrState>(`/nvr/cameras/${removing.id}`);
			if (live === removing.id) live = '';
			if (browsing === removing.id) browsing = '';
			removing = null;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	async function setTime() {
		if (!timing) return;
		busy = true;
		error = '';
		try {
			const res = await api.post<{ method: string; server: string }>(
				`/nvr/cameras/${timing.id}/time`,
			);
			timeDone = t('nvr.timeDone', { name: timing.name, server: res.server });
			timing = null;
			await load();
		} catch (e) {
			error = apiErrorText(e);
			timing = null;
		} finally {
			busy = false;
		}
	}

	async function showEvents(camera: Camera) {
		if (eventsOf === camera.id) {
			eventsOf = '';
			return;
		}
		eventsOf = camera.id;
		try {
			events = (await api.get<{ events: NvrEvent[] }>(`/nvr/cameras/${camera.id}/events`)).events;
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	/** Opens the recording file that holds the moment of an event. */
	async function playEvent(camera: Camera, ev: NvrEvent) {
		browsing = camera.id;
		recordings = null;
		const day = ev.id.split('/')[0] ?? '';
		const loaded = await loadDay(camera, day);
		const start = new Date(ev.start).getTime();
		const hit = loaded?.segments.filter((s) => new Date(s.start).getTime() <= start).at(-1);
		playing = hit?.name ?? '';
	}

	function eventTitle(ev: NvrEvent): string {
		const time = new Date(ev.start).toLocaleTimeString();
		const what = ev.labels?.length ? ev.labels.join(', ') : t('nvr.motion');
		return `${time} · ${what}`;
	}

	async function browse(camera: Camera) {
		if (browsing === camera.id) {
			browsing = '';
			return;
		}
		browsing = camera.id;
		playing = '';
		recordings = null;
		await loadDay(camera, '');
	}

	async function loadDay(camera: Camera, wanted: string): Promise<Recordings | null> {
		try {
			const first = await api.get<Recordings>(`/nvr/cameras/${camera.id}/recordings`);
			day = wanted || first.days[0] || '';
			recordings = day
				? await api.get<Recordings>(`/nvr/cameras/${camera.id}/recordings?day=${day}`)
				: first;
			return recordings;
		} catch (e) {
			error = apiErrorText(e);
			return null;
		}
	}

	async function protect(camera: Camera, name: string, on: boolean) {
		const f = fileOf(name);
		const url = `/nvr/cameras/${camera.id}/recordings/${f.day}/${f.file}/protect`;
		try {
			if (on) await api.post(url);
			else await api.delete(url);
			playing = '';
			await loadDay(camera, day);
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	function since(camera: Camera): string {
		return camera.latest ? new Date(camera.latest * 1000).toLocaleString() : '–';
	}

	const noVolume = $derived(nvr !== null && !nvr.settings.volume);
	const settingsChanged = $derived(
		nvr !== null &&
			(volume !== (nvr.settings.volume ?? '') ||
				keepDays !== nvr.settings.keepDays ||
				eventKeepDays !== (nvr.settings.eventKeepDays || 30) ||
				reserveGB !== nvr.settings.reserveGB),
	);
</script>

<PageHead title={t('nvr.title')}>
	<p class="muted">{t('nvr.intro')}</p>
	{#snippet actions()}
		<a class="btn" href="/nvr/views">{t('nvr.viewsLink')}</a>
		<button type="button" class="btn btn-primary" onclick={() => (editing = 'new')}
			><Icon name="plus" size={16} /> {t('nvr.add')}</button
		>
	{/snippet}
</PageHead>

{#if error}<div class="notice-error" role="alert">{error}</div>{/if}
{#if timeDone}<div class="notice-ok" role="status">{timeDone}</div>{/if}

{#if nvr}
	<section class="tile panel">
		<h2 class="tile-title">{t('nvr.storageTitle')}</h2>
		{#if noVolume}<div class="notice-warn">{t('nvr.noVolume')}</div>{/if}
		<div class="grid">
			<label
				><span>{t('nvr.volume')}</span>
				<select
					class="field"
					bind:value={volume}
					onchange={() => (touched = true)}
					disabled={nvr.volumes.length === 0}
				>
					<option value="">{t('nvr.volumeNone')}</option>
					{#each nvr.volumes as v (v)}<option value={v}>{v}</option>{/each}
				</select>
			</label>
			<label
				><span>{t('nvr.keepDays')}</span>
				<input
					class="field"
					type="number"
					min="1"
					max="3650"
					bind:value={keepDays}
					oninput={() => (touched = true)}
				/>
			</label>
			<label
				><span>{t('nvr.eventKeepDays')}</span>
				<input
					class="field"
					type="number"
					min="1"
					max="3650"
					bind:value={eventKeepDays}
					oninput={() => (touched = true)}
				/>
			</label>
			<label
				><span>{t('nvr.reserve')}</span>
				<input
					class="field"
					type="number"
					min="0"
					bind:value={reserveGB}
					oninput={() => (touched = true)}
				/>
			</label>
		</div>
		<p class="muted small">
			{t('nvr.storageHint')}
			{#if nvr.root}· {t('nvr.usage', {
					used: formatBytes(nvr.usedBytes),
					free: formatBytes(nvr.freeBytes),
				})}{/if}
		</p>
		<div class="row-actions">
			<button
				type="button"
				class="btn btn-primary"
				onclick={saveSettings}
				disabled={busy || !settingsChanged}>{t('common.save')}</button
			>
		</div>
	</section>

	<section class="tile panel armbar">
		<span class="chip {nvr.armedNow ? 'ok' : ''}"
			>{nvr.armedNow ? '● ' + t('nvrArm.armedNow') : '○ ' + t('nvrArm.disarmedNow')}</span
		>
		{#if nvr.settings.arming.mode === 'schedule'}<span class="muted small"
				>{t('nvrArm.bySchedule')}</span
			>{/if}
		<div class="row-actions">
			<button type="button" class="btn" onclick={() => arm(!nvr?.armedNow)} disabled={busy}
				>{nvr.armedNow ? t('nvrArm.disarm') : t('nvrArm.arm')}</button
			>
			<a class="btn" href="/settings/notifications">{t('nvrArm.settings')}</a>
		</div>
	</section>

	{#if !nvr.settings.enabled}
		<div class="notice-warn">{t('nvr.off')}</div>
	{/if}

	{#if nvr.cameras.length === 0}
		<EmptyState title={t('nvr.emptyTitle')} text={t('nvr.emptyText')} />
	{/if}

	{#each nvr.cameras as camera (camera.id)}
		<section class="tile panel camera">
			<div class="head">
				<div>
					<strong>{camera.name}</strong>
					<span class="chip {camera.status.recording ? 'ok' : ''}"
						>{camera.status.recording
							? '● ' + t('nvr.recording')
							: modeOf(camera) !== 'off'
								? '○ ' + t('nvr.notRecording')
								: '○ ' + t('nvr.recordOff')}</span
					>
					<span class="chip"
						>{t(
							`nvr.mode${modeOf(camera) === 'events' ? 'Events' : modeOf(camera) === 'continuous' ? 'Continuous' : 'Off'}` as 'nvr.modeOff',
						)}</span
					>
					<p class="muted small">
						{#if camera.watch.motion}<strong>{t('nvr.motionNow')}</strong> ·
						{/if}{#if camera.timeSync}{t('nvr.timeFromNas')} ·
						{/if}{camera.host} · {t('nvr.stored', {
							size: formatBytes(camera.bytes),
							count: camera.segments,
						})} · {t('nvr.latest')}
						{since(camera)}
					</p>
					{#if camera.status.lastError && !camera.status.recording}
						<p class="small error-text">{camera.status.lastError}</p>
					{/if}
				</div>
				<div class="row-actions">
					<button
						type="button"
						class="btn"
						aria-pressed={live === camera.id}
						onclick={() => (live = live === camera.id ? '' : camera.id)}
						>{live === camera.id ? t('nvr.liveStop') : t('nvr.live')}</button
					>
					{#if camera.detect || modeOf(camera) === 'events'}
						<button
							type="button"
							class="btn"
							aria-expanded={eventsOf === camera.id}
							onclick={() => showEvents(camera)}>{t('nvr.events')}</button
						>
					{/if}
					<button
						type="button"
						class="btn"
						aria-expanded={browsing === camera.id}
						onclick={() => browse(camera)}>{t('nvr.recordings')}</button
					>
					<button type="button" class="btn" onclick={() => (timing = camera)}
						>{camera.timeSync ? t('nvr.timeAgain') : t('nvr.timeSet')}</button
					>
					{#if camera.onvifPort}
						<button type="button" class="btn" onclick={() => (tuning = camera)}
							>{t('nvr.quality')}</button
						>
					{/if}
					<button type="button" class="btn" onclick={() => (editing = camera)}
						>{t('nvr.edit')}</button
					>
					<button type="button" class="btn btn-danger-outline" onclick={() => (removing = camera)}
						>{t('nvr.remove')}</button
					>
				</div>
			</div>

			{#if live === camera.id}
				<img class="live" src={`/api/v1/nvr/cameras/${camera.id}/live`} alt={camera.name} />
			{/if}

			{#if eventsOf === camera.id}
				<div class="events">
					{#each events as ev (ev.id)}
						<button type="button" class="event" onclick={() => playEvent(camera, ev)}>
							{#if ev.snapshot}
								<img src={pictureUrl(camera.id, ev.id)} alt="" loading="lazy" />
							{:else}
								<span class="nopic"><Icon name="camera" size={20} /></span>
							{/if}
							<span class="small">{eventTitle(ev)}</span>
						</button>
					{:else}
						<p class="muted">{t('nvr.noEvents')}</p>
					{/each}
				</div>
			{/if}

			{#if browsing === camera.id}
				<div class="browse">
					{#if recordings && recordings.days.length > 0}
						<label
							><span>{t('nvr.day')}</span>
							<select class="field" bind:value={day} onchange={() => loadDay(camera, day)}>
								{#each recordings.days as d (d)}<option value={d}>{d}</option>{/each}
							</select>
						</label>
						<div class="segments">
							{#each recordings.segments as seg (seg.name)}
								<button
									type="button"
									class="btn"
									class:active={playing === seg.name}
									onclick={() => {
										playing = seg.name;
										playingProtected = false;
									}}
									>{clockOf(seg.name)} <small class="muted">{formatBytes(seg.size)}</small></button
								>
							{/each}
						</div>
						{#if recordings.protected.length > 0}
							<p class="muted small">{t('nvr.protectedList')}</p>
							<div class="segments">
								{#each recordings.protected as seg (seg.name)}
									<button
										type="button"
										class="btn"
										class:active={playing === seg.name && playingProtected}
										onclick={() => {
											playing = seg.name;
											playingProtected = true;
										}}>{seg.name.replace('.mp4', '')}</button
									>
								{/each}
							</div>
						{/if}
						{#if playing}
							{@const f = fileOf(playing)}
							<!-- svelte-ignore a11y_media_has_caption -->
							<video
								class="player"
								controls
								autoplay
								src={`/api/v1/nvr/cameras/${camera.id}/recordings/${f.day}/${f.file}${playingProtected ? '?protected=1' : ''}`}
							></video>
							<div class="row-actions">
								<button
									type="button"
									class="btn"
									onclick={() => protect(camera, playing, playingProtected ? false : true)}
									>{playingProtected ? t('nvr.unprotect') : t('nvr.protect')}</button
								>
							</div>
						{/if}
					{:else if recordings}
						<p class="muted">{t('nvr.noRecordings')}</p>
					{/if}
				</div>
			{/if}
		</section>
	{/each}
{/if}

{#if editing && nvr}
	<CameraDialog
		camera={editing === 'new' ? undefined : editing}
		vendors={nvr.vendors}
		onclose={() => (editing = null)}
		onsaved={() => {
			editing = null;
			void load();
		}}
	/>
{/if}

{#if tuning}
	<QualityDialog camera={tuning} onclose={() => (tuning = null)} />
{/if}

{#if timing}
	<Dialog
		title={t('nvr.timeTitle', { name: timing.name })}
		locked={busy}
		onclose={() => (timing = null)}
	>
		<p>{t('nvr.timeText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (timing = null)} disabled={busy}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" onclick={setTime} disabled={busy}
				>{t('nvr.timeSet')}</button
			>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog
		title={t('nvr.removeTitle', { name: removing.name })}
		locked={busy}
		onclose={() => (removing = null)}
	>
		<p>{t('nvr.removeText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)} disabled={busy}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" onclick={remove} disabled={busy}
				>{t('nvr.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.armbar {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		align-items: center;
	}
	.armbar .row-actions {
		margin-left: auto;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
		gap: 12px;
	}
	label {
		display: grid;
		gap: 4px;
	}
	.camera {
		display: grid;
		gap: 12px;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		justify-content: space-between;
		align-items: start;
	}
	.head p {
		margin: 4px 0 0;
	}
	.error-text {
		color: var(--error, #c0392b);
	}
	.live,
	.player {
		width: 100%;
		max-height: 70vh;
		background: #000;
		border-radius: 8px;
		object-fit: contain;
	}
	.segments {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin: 8px 0;
	}
	.events {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 8px;
	}
	.event {
		display: grid;
		gap: 4px;
		text-align: left;
		padding: 6px;
		border: 1px solid var(--border);
		border-radius: 8px;
		background: none;
		color: inherit;
		cursor: pointer;
	}
	.event img,
	.nopic {
		width: 100%;
		aspect-ratio: 16 / 9;
		object-fit: cover;
		border-radius: 6px;
		background: #000;
		display: grid;
		place-items: center;
	}
	.segments .active {
		outline: 2px solid var(--accent, currentColor);
	}
</style>
