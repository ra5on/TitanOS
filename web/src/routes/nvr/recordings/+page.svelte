<script lang="ts">
	// PLAN 79-7: the recordings of one camera and day – a time line with
	// recorded stretches, movement, objects and gaps, the player (HLS), speed,
	// "next event" and the export of a stretch with the list of exports.
	import { page } from '$app/state';
	import { api } from '#lib/api.ts';
	import { apiPath } from '#lib/cluster-node.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Confirm from '#lib/nvr/Confirm.svelte';
	import { cameraName, mayRecordings, nvr, nvrAdmin, unixOf } from '#lib/nvr/nvr.svelte.ts';
	import { clock, dayClock, labelName } from '#lib/nvr/texts.ts';
	import Timeline from '#lib/nvr/Timeline.svelte';
	import {
		DAY,
		dayOf,
		dayStart,
		mediaToWall,
		nextRecorded,
		playWindow,
		wallToMedia,
	} from '#lib/nvr/timeline.ts';
	import type {
		EventList,
		NvrEvent,
		NvrExport,
		RecordingList,
		RecordingSummary,
		Segment,
	} from '#lib/nvr/types.ts';
	import { attachVod } from '#lib/nvr/vod.ts';
	import { poll } from '#lib/poll.ts';

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const cameras = $derived(nvr.cameras.filter(mayRecordings));
	// Removed cameras keep their recordings for a while (administrators only).
	const removed = $derived(nvrAdmin() ? nvr.removed : []);
	const choosable = $derived([...cameras, ...removed]);
	const removedChosen = $derived(removed.find((c) => c.id === camera));
	const dayText = (seconds: number) =>
		new Date(seconds * 1000).toLocaleDateString(locale, {
			day: '2-digit',
			month: '2-digit',
			year: 'numeric',
		});
	const nameOf = cameraName;

	let camera = $state(page.url.searchParams.get('camera') ?? '');
	const wantedAt = Number(page.url.searchParams.get('at')) || 0;
	let day = $state(dayOf(wantedAt || Date.now() / 1000));
	$effect(() => {
		if (choosable.length && !choosable.some((c) => c.id === camera)) camera = choosable[0].id;
	});
	const from = $derived(dayStart(day));
	const base = $derived(`/nvr/cameras/${encodeURIComponent(camera)}`);

	// ---- What the day holds ----
	let segments = $state<Segment[]>([]);
	let events = $state<NvrEvent[]>([]);
	let days = $state<string[]>([]);
	let loading = $state(false);
	let error = $state('');
	/** This NAS has no recordings to offer (the NVR's phase 2 is missing). */
	let missing = $state(false);
	$effect(() => {
		const id = camera;
		const start = from;
		if (!id) return;
		loading = true;
		error = '';
		const still = () => id === camera && start === from;
		Promise.all([
			api.get<RecordingList>(`${base}/recordings?from=${start}&to=${start + DAY}`),
			api
				.get<EventList>(
					`/nvr/events?camera=${encodeURIComponent(id)}&from=${start}&to=${start + DAY}&limit=200`,
				)
				.catch(() => ({ events: [], next: null }) as EventList),
		]).then(
			([list, found]) => {
				if (!still()) return;
				segments = list.segments ?? [];
				events = [...(found.events ?? [])].sort((a, b) => a.start - b.start);
				missing = false;
				loading = false;
				// A link to a time, else the newest recording of the day.
				if (wantedAt >= start && wantedAt < start + DAY && at === null) pick(wantedAt);
			},
			(err) => {
				if (!still()) return;
				segments = [];
				events = [];
				loading = false;
				missing = (err as { status?: number }).status === 404;
				error = missing ? '' : apiErrorText(err);
			},
		);
	});
	$effect(() => {
		const id = camera;
		if (!id) return;
		api.get<RecordingSummary>(`${base}/recordings/summary`).then(
			(summary) => {
				if (id === camera) days = (summary.days ?? []).map((d) => d.day).sort();
			},
			() => (days = []),
		);
	});
	function shiftDay(by: number) {
		day = dayOf(from + by * DAY + 3600 * 12);
		stop();
	}
	const today = $derived(dayOf(Date.now() / 1000));

	// ---- Player ----
	let video = $state<HTMLVideoElement>();
	let at = $state<number | null>(null);
	let win = $state<{ from: number; to: number } | null>(null);
	let speed = $state('1');
	let zoom = $state<'24' | '6' | '1'>('24');
	let playError = $state('');
	let hls: { destroy: () => void } | null = null;

	function stop() {
		hls?.destroy();
		hls = null;
		win = null;
		at = null;
		playError = '';
		if (video) {
			video.pause();
			video.removeAttribute('src');
			video.load();
		}
	}
	$effect(() => () => hls?.destroy());

	async function load(next: { from: number; to: number }, wall: number, play = true) {
		if (!video) return;
		hls?.destroy();
		hls = null;
		win = next;
		playError = '';
		const el = video;
		const src = apiPath(`${base}/vod/${Math.floor(next.from)}/${Math.floor(next.to)}/master.m3u8`);
		const begin = () => {
			el.currentTime = wallToMedia(segments, next.from, next.to, wall);
			el.playbackRate = Number(speed);
			if (play) void el.play().catch(() => {});
		};
		const stop = await attachVod(el, src, begin, (unsupported) => {
			playError = t(unsupported ? 'nvr.rec.unsupported' : 'nvr.rec.playFailed');
		});
		hls = { destroy: stop };
	}
	function pick(wall: number) {
		const limit = Math.min(from + DAY, Math.floor(Date.now() / 1000));
		// A click into a gap goes to the next recording.
		const start = nextRecorded(segments, wall);
		if (start === null || start >= limit) {
			at = wall;
			playError = t('nvr.rec.nothingAfter');
			return;
		}
		at = start;
		const next = playWindow(start, from, limit);
		if (win && win.from === next.from && win.to === next.to && video && !playError) {
			video.currentTime = wallToMedia(segments, next.from, next.to, start);
			void video.play().catch(() => {});
		} else void load(next, start);
	}
	function tick() {
		if (!video || !win) return;
		at = mediaToWall(segments, win.from, win.to, video.currentTime);
	}
	function ended() {
		// The next hour follows on its own.
		if (!win) return;
		const start = nextRecorded(segments, win.to);
		if (start !== null && start < from + DAY) pick(start);
	}
	$effect(() => {
		if (video) video.playbackRate = Number(speed);
	});
	const nextEvent = $derived(events.find((e) => e.start > (at ?? from) + 1));

	// ---- Export ----
	let exportOpen = $state(false);
	let exportFrom = $state('');
	let exportTo = $state('');
	let exportName = $state('');
	let exportBusy = $state(false);
	let exportError = $state('');
	let exports = $state<NvrExport[] | null>(null);
	let exportsMissing = $state(false);
	const hm = (seconds: number) => {
		const d = new Date(seconds * 1000);
		const two = (n: number) => String(n).padStart(2, '0');
		return `${two(d.getHours())}:${two(d.getMinutes())}:${two(d.getSeconds())}`;
	};
	const fromHm = (value: string) => {
		const [h, m, s] = value.split(':').map(Number);
		return from + (h || 0) * 3600 + (m || 0) * 60 + (s || 0);
	};
	function openExport() {
		const start = at ?? from;
		exportFrom = hm(start);
		exportTo = hm(Math.min(start + 120, from + DAY - 1));
		exportName = '';
		exportError = '';
		exportOpen = true;
	}
	const exportRange = $derived(
		exportOpen && exportFrom && exportTo
			? { from: fromHm(exportFrom), to: fromHm(exportTo) }
			: null,
	);
	async function loadExports() {
		try {
			exports = (await api.get<{ exports: NvrExport[] }>('/nvr/exports')).exports ?? [];
			exportsMissing = false;
		} catch {
			exports = [];
			exportsMissing = true;
		}
	}
	$effect(() =>
		poll(loadExports, {
			interval: () => (exports?.some((e) => e.state === 'running') ? 2000 : 30000),
		}),
	);
	async function createExport(event: SubmitEvent) {
		event.preventDefault();
		if (!exportRange || exportRange.to <= exportRange.from) {
			exportError = t('nvr.export.badRange');
			return;
		}
		exportBusy = true;
		exportError = '';
		try {
			await api.post('/nvr/exports', {
				camera,
				from: exportRange.from,
				to: exportRange.to,
				name: exportName.trim(),
			});
			exportOpen = false;
			await loadExports();
		} catch (err) {
			exportError = apiErrorText(err);
		} finally {
			exportBusy = false;
		}
	}
	/** The period of an export; one not made through TitanOS has none (0). */
	function exportPeriod(item: NvrExport): string {
		if (!item.from || !item.to) return '–';
		return `${dayClock(item.from, locale)} – ${clock(item.to, locale)}`;
	}
	let renaming = $state<NvrExport | null>(null);
	let newName = $state('');
	let removing = $state<NvrExport | null>(null);
	let listBusy = $state(false);
	let listError = $state('');
	async function rename(event: SubmitEvent) {
		event.preventDefault();
		if (!renaming) return;
		listBusy = true;
		listError = '';
		try {
			await api.put(`/nvr/exports/${encodeURIComponent(renaming.id)}`, { name: newName.trim() });
			renaming = null;
			await loadExports();
		} catch (err) {
			listError = apiErrorText(err);
		} finally {
			listBusy = false;
		}
	}
	async function remove() {
		if (!removing) return;
		listBusy = true;
		listError = '';
		try {
			await api.delete(`/nvr/exports/${encodeURIComponent(removing.id)}`);
			removing = null;
			await loadExports();
		} catch (err) {
			listError = apiErrorText(err);
		} finally {
			listBusy = false;
		}
	}
</script>

<PageHead title={t('nvr.menu.recordings')} />

{#if choosable.length === 0}
	<EmptyState
		title={t('nvr.rec.noneTitle')}
		text={nvrAdmin() ? t('nvr.live.noneAdmin') : t('nvr.rec.noneUser')}
	/>
{:else}
	<div class="pick">
		<label
			><span>{t('nvr.rec.camera')}</span>
			<select class="field" bind:value={camera} onchange={stop}>
				{#each cameras as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
				{#if removed.length}
					<optgroup label={t('nvr.removed.optgroup')}>
						{#each removed as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
					</optgroup>
				{/if}
			</select>
		</label>
		<div class="day">
			<button
				type="button"
				class="btn icon"
				aria-label={t('nvr.rec.dayBefore')}
				title={t('nvr.rec.dayBefore')}
				onclick={() => shiftDay(-1)}><Icon name="chevronLeft" size={16} /></button
			>
			<label
				><span class="sr-only">{t('nvr.rec.day')}</span>
				<input
					class="field"
					type="date"
					bind:value={day}
					max={today}
					min={days[0]}
					onchange={stop}
				/>
			</label>
			<button
				type="button"
				class="btn icon"
				aria-label={t('nvr.rec.dayAfter')}
				title={t('nvr.rec.dayAfter')}
				disabled={day >= today}
				onclick={() => shiftDay(1)}><Icon name="chevronRight" size={16} /></button
			>
		</div>
		<Segments
			compact
			options={[
				{ value: '24', label: t('nvr.rec.zoom24') },
				{ value: '6', label: t('nvr.rec.zoom6') },
				{ value: '1', label: t('nvr.rec.zoom1') },
			]}
			bind:value={zoom}
			label={t('nvr.rec.zoom')}
		/>
	</div>

	{#if removedChosen}
		{@const until = unixOf(removedChosen.expiresAt)}
		<p class="muted removed-note" role="status">
			{until ? t('nvr.removed.until', { date: dayText(until) }) : t('nvr.removed.untilUnknown')}
		</p>
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if missing}
		<p class="muted" role="status">{t('nvr.rec.missing')}</p>
	{:else}
		<Timeline
			{from}
			{segments}
			{events}
			{at}
			zoom={Number(zoom)}
			range={exportRange}
			onpick={pick}
		/>
		<ul class="legend" aria-label={t('nvr.rec.legend')}>
			<li><span class="sw plain"></span>{t('nvr.rec.legendPlain')}</li>
			<li><span class="sw motion"></span>{t('nvr.rec.legendMotion')}</li>
			<li><span class="sw objects"></span>{t('nvr.rec.legendObjects')}</li>
			<li><span class="sw event"></span>{t('nvr.rec.legendEvent')}</li>
			<li><span class="sw gap"></span>{t('nvr.rec.legendGap')}</li>
		</ul>
		{#if !loading && segments.length === 0}
			<p class="muted" role="status">{t('nvr.rec.empty', { name: nameOf(camera) })}</p>
		{/if}

		<div class="player">
			<!-- svelte-ignore a11y_media_has_caption -->
			<video
				bind:this={video}
				controls
				playsinline
				ontimeupdate={tick}
				onended={ended}
				aria-label={t('nvr.rec.videoOf', { name: nameOf(camera) })}
			></video>
			{#if at === null}<p class="over">{t('nvr.rec.pickHint')}</p>{/if}
		</div>
		{#if playError}<p class="notice-warn" role="alert">{playError}</p>{/if}
		<div class="controls">
			<span class="now" role="status"
				>{at === null ? t('nvr.rec.noTime') : clock(at, locale, true)}</span
			>
			<label class="speed"
				><span>{t('nvr.rec.speed')}</span>
				<select class="field" bind:value={speed}>
					{#each ['0.5', '1', '2', '4', '8'] as s (s)}<option value={s}
							>{s.replace('.', i18n.locale === 'de' ? ',' : '.')}×</option
						>{/each}
				</select>
			</label>
			<button
				type="button"
				class="btn"
				disabled={!nextEvent}
				onclick={() => nextEvent && pick(Math.max(from, nextEvent.start - 2))}
				><Icon name="arrowRight" size={15} />{nextEvent
					? t('nvr.rec.nextEventAt', {
							what: labelName(nextEvent.label),
							time: clock(nextEvent.start, locale),
						})
					: t('nvr.rec.nextEventNone')}</button
			>
			<button type="button" class="btn" disabled={segments.length === 0} onclick={openExport}
				><Icon name="download" size={15} />{t('nvr.export.start')}</button
			>
		</div>
	{/if}

	<SettingGroup title={t('nvr.export.list')} id="nvr-exports">
		{#if listError}<p class="notice-error" role="alert">{listError}</p>{/if}
		{#if exports === null}
			<p class="muted" role="status">{t('common.loading')}</p>
		{:else if exportsMissing}
			<p class="muted" role="status">{t('nvr.export.missing')}</p>
		{:else if exports.length === 0}
			<p class="muted" role="status">{t('nvr.export.none')}</p>
		{:else}
			{#each exports as item (item.id)}
				<div class="srow export" data-export={item.id}>
					<div class="srow-head">
						<div class="srow-text">
							<span class="srow-title">{item.name}</span>
							<p class="srow-desc">
								{[
									nameOf(item.camera),
									t('nvr.export.range', { range: exportPeriod(item) }),
									item.created
										? t('nvr.export.created', { time: dayClock(item.created, locale) })
										: '',
									item.sizeBytes !== null && item.sizeBytes !== undefined
										? formatBytes(item.sizeBytes)
										: '',
									nvrAdmin() && item.owner ? item.owner : '',
								]
									.filter(Boolean)
									.join(' · ')}
							</p>
							{#if item.state === 'failed'}<p class="srow-desc">
									{t('nvr.export.failedHint')}
								</p>{/if}
						</div>
						<div class="srow-ctl">
							<span
								class="chip {item.state === 'done'
									? 'ok'
									: item.state === 'failed'
										? 'error'
										: 'info'}">{t(`nvr.export.state.${item.state}` as MessageKey)}</span
							>
							{#if item.state === 'done'}
								<a
									class="btn"
									href={apiPath(`/nvr/exports/${encodeURIComponent(item.id)}/download`)}
									download>{t('nvr.export.download')}</a
								>
							{/if}
							<button
								type="button"
								class="btn"
								onclick={() => {
									renaming = item;
									newName = item.name;
									listError = '';
								}}>{t('nvr.export.rename')}</button
							>
							<button
								type="button"
								class="btn btn-danger-outline"
								onclick={() => {
									removing = item;
									listError = '';
								}}>{t('common.delete')}</button
							>
						</div>
					</div>
				</div>
			{/each}
		{/if}
	</SettingGroup>
{/if}

{#if exportOpen}
	<Dialog title={t('nvr.export.title')} locked={exportBusy} onclose={() => (exportOpen = false)}>
		<form class="export-form" onsubmit={createExport}>
			<p class="muted">{t('nvr.export.hint', { name: nameOf(camera) })}</p>
			<label
				><span>{t('nvr.export.from')}</span>
				<input class="field" type="time" step="1" bind:value={exportFrom} required />
			</label>
			<label
				><span>{t('nvr.export.to')}</span>
				<input class="field" type="time" step="1" bind:value={exportTo} required />
			</label>
			<label
				><span>{t('nvr.export.name')}</span>
				<input
					class="field"
					type="text"
					maxlength="80"
					bind:value={exportName}
					placeholder={t('nvr.export.namePlaceholder')}
				/>
			</label>
			{#if exportError}<p class="notice-error" role="alert">{exportError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={exportBusy} onclick={() => (exportOpen = false)}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary" disabled={exportBusy}
					>{t('nvr.export.start')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}
{#if renaming}
	<Dialog title={t('nvr.export.rename')} locked={listBusy} onclose={() => (renaming = null)}>
		<form class="export-form" onsubmit={rename}>
			<label
				><span>{t('nvr.export.name')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input class="field" type="text" maxlength="80" bind:value={newName} required autofocus />
			</label>
			{#if listError}<p class="notice-error" role="alert">{listError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={listBusy} onclick={() => (renaming = null)}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary" disabled={listBusy || !newName.trim()}
					>{t('common.save')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}
{#if removing}
	<Confirm
		title={t('nvr.export.deleteTitle')}
		text={t('nvr.export.deleteText', { name: removing.name })}
		confirm={t('common.delete')}
		danger
		busy={listBusy}
		error={listError}
		onconfirm={remove}
		onclose={() => (removing = null)}
	/>
{/if}

<style>
	.pick,
	.controls {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		min-width: 0;
	}
	.pick label,
	.speed {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--muted);
		font-size: var(--text-base);
		min-width: 0;
	}
	.pick select {
		max-width: 100%;
	}
	.day {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
	}
	.btn.icon {
		display: grid;
		place-items: center;
		width: 34px;
		height: 34px;
		padding: 0;
	}
	.legend {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-4);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.legend li {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.sw {
		width: 14px;
		height: 10px;
		border-radius: 3px;
		border: 1px solid var(--border);
	}
	.sw.plain {
		background: color-mix(in srgb, var(--accent) 35%, transparent);
	}
	.sw.motion {
		background: color-mix(in srgb, var(--accent) 70%, transparent);
	}
	.sw.objects {
		background: #1f9d6b;
	}
	.sw.event {
		width: 10px;
		border-radius: 50%;
		background: #f79009;
	}
	.sw.gap {
		background: var(--track, var(--surface));
	}
	.removed-note {
		margin: 0;
		overflow-wrap: break-word;
	}
	/* The controls below stay in view: the picture is at most about half the window high. */
	.player {
		position: relative;
		width: 100%;
		max-width: min(64rem, calc(max(240px, 55vh) * 16 / 9));
		aspect-ratio: 16 / 9;
		border-radius: 10px;
		overflow: hidden;
		background: #0b0f14;
	}
	.player video {
		width: 100%;
		height: 100%;
		display: block;
	}
	.over {
		position: absolute;
		inset: 0;
		margin: 0;
		display: grid;
		place-items: center;
		padding: var(--space-4);
		text-align: center;
		color: #c9d1d9;
		pointer-events: none;
		overflow-wrap: anywhere;
	}
	.now {
		font-variant-numeric: tabular-nums;
		font-weight: 600;
		min-width: 5.5rem;
	}
	.controls .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		text-align: left;
	}
	.export .srow-ctl .btn {
		display: inline-flex;
		align-items: center;
	}
	.export-form {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.export-form label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.export-form p {
		margin: 0;
	}
</style>
