<script lang="ts">
	// PLAN 79-7: what the cameras saw – filtered by camera, kind and time, as
	// pictures or as a list; an event opens with its clip, its snapshot, the
	// recognized face or plate, download and (administrators) delete. PLAN
	// 88: "seen" is per account; administrators keep an event for good
	// ("Behalten": the NAS saves its video) and give it back.
	import { page } from '$app/state';
	import { api } from '#lib/api.ts';
	import { apiPath } from '#lib/cluster-node.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import Confirm from '#lib/nvr/Confirm.svelte';
	import EventCard from '#lib/nvr/EventCard.svelte';
	import { cameraName, mayRecordings, nvr, nvrAdmin } from '#lib/nvr/nvr.svelte.ts';
	import { clock, dayClock, labelName, OBJECT_GROUPS } from '#lib/nvr/texts.ts';
	import type { Camera, EventList, NvrEvent } from '#lib/nvr/types.ts';
	import { attachVod, autoplay } from '#lib/nvr/vod.ts';
	import { untrack } from 'svelte';

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const admin = $derived(nvrAdmin());
	const cameras = $derived(nvr.cameras.filter(mayRecordings));
	const nameOf = cameraName;

	let camera = $state('');
	let kind = $state<'' | 'persons' | 'vehicles' | 'animals'>('');
	let span = $state<'day' | 'week' | 'all'>('week');
	let view = $state<'grid' | 'list'>('grid');
	try {
		if (localStorage.getItem('titanos.nvr.events.view') === 'list') view = 'list';
	} catch {
		/* private window */
	}

	let events = $state<NvrEvent[]>([]);
	let next = $state<number | null>(null);
	let loading = $state(true);
	let error = $state('');
	let missing = $state(false);

	const query = $derived.by(() => {
		const params = new URLSearchParams({ limit: '60' });
		if (camera) params.set('camera', camera);
		if (kind) params.set('label', OBJECT_GROUPS[kind].join(','));
		if (span !== 'all')
			params.set('from', String(Math.floor(Date.now() / 1000) - (span === 'day' ? 86400 : 604800)));
		return params.toString();
	});
	async function load(more = false) {
		const q = query;
		loading = true;
		error = '';
		try {
			const list = await api.get<EventList>(
				`/nvr/events?${q}${more && next !== null ? `&before=${next}` : ''}`,
			);
			if (q !== query) return;
			events = more ? [...events, ...(list.events ?? [])] : (list.events ?? []);
			next = list.next ?? null;
			missing = false;
		} catch (err) {
			if (q !== query) return;
			missing = (err as { status?: number }).status === 404;
			error = missing ? '' : apiErrorText(err);
			if (!more) events = [];
		} finally {
			if (q === query) loading = false;
		}
	}
	$effect(() => {
		void query;
		void load();
	});

	// ---- Seen (per account, PLAN 88) ----
	let seenBusy = $state(false);
	async function allSeen() {
		seenBusy = true;
		error = '';
		try {
			await api.post('/nvr/events/seen', {});
			// The NAS marks every event that began before now, running ones too.
			events = events.map((e) => ({ ...e, reviewed: true }));
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			seenBusy = false;
		}
	}
	const anyUnseen = $derived(events.some((e) => !e.reviewed));

	// ---- One event ----
	let open = $state<NvrEvent | null>(null);
	let detailError = $state('');
	function show(event: NvrEvent) {
		open = event;
		detailError = '';
		// A failure of the event before must not keep the next one from playing.
		recordingFailed = false;
		playMuted = false;
		if (!event.reviewed)
			api
				.post<NvrEvent>(`/nvr/events/${encodeURIComponent(event.id)}/reviewed`, { reviewed: true })
				.then(
					() => {
						events = events.map((e) => (e.id === event.id ? { ...e, reviewed: true } : e));
					},
					() => {},
				);
	}
	// A link from the live view or a notification: /nvr/events?event=<id>.
	let linked = '';
	$effect(() => {
		const id = page.url.searchParams.get('event');
		if (!id || id === linked) return;
		linked = id;
		api.get<NvrEvent>(`/nvr/events/${encodeURIComponent(id)}`).then(show, (err) => {
			error = apiErrorText(err);
		});
	});
	const eventBase = $derived(open ? `/nvr/events/${encodeURIComponent(open.id)}` : '');

	// PLAN 82-5: an event opened (a tap, a push) plays at once in this
	// dialog, with sound where the browser allows it. It always plays the
	// recording around it (HLS, from five seconds before to five seconds
	// after; a running event up to now), never clip.mp4: Frigate streams the
	// clip from ffmpeg without a length and ignores Range, so iPhones do not
	// play it and seeking fails elsewhere (Frigate itself says to use HLS on
	// iOS). The clip stays for "Ausschnitt herunterladen".
	let player = $state<HTMLVideoElement>();
	let playMuted = $state(false);
	let fromRecording = $state(false);
	let recordingFailed = $state(false);
	let stopRecording: (() => void) | null = null;
	$effect(() => {
		const e = open;
		const el = player;
		if (!e || !el) return;
		untrack(() => void play(e, el));
		return () => {
			stopRecording?.();
			stopRecording = null;
		};
	});
	async function play(e: NvrEvent, el: HTMLVideoElement) {
		playMuted = false;
		fromRecording = !e.hasClip;
		recordingFailed = false;
		const start = async () => {
			playMuted = await autoplay(el);
		};
		const from = Math.floor(e.start) - 5;
		const to = Math.max(from + 30, Math.ceil(e.end ?? Date.now() / 1000) + 5);
		const src = apiPath(
			`/nvr/cameras/${encodeURIComponent(e.camera)}/vod/${from}/${to}/master.m3u8`,
		);
		el.addEventListener('error', () => (recordingFailed = true), { once: true });
		const stop = await attachVod(
			el,
			src,
			() => void start(),
			() => (recordingFailed = true),
		);
		if (open?.id === e.id) stopRecording = stop;
		else stop();
	}

	// A kept event whose recording is gone plays the video the NAS saved.
	const keptSrc = $derived(
		open?.keptExport ? apiPath(`/nvr/exports/${encodeURIComponent(open.keptExport)}/download`) : '',
	);

	// ---- Keep (administrators, PLAN 88) ----
	/** Whether an event's video is complete (the NAS waits its seconds after and 20 s). */
	function settled(e: NvrEvent) {
		if (e.end === null) return false;
		const post = cameraOf(e.camera)?.record?.postSeconds ?? 5;
		return Date.now() / 1000 > e.end + post + 25;
	}
	const cameraOf = (id: string): Camera | undefined =>
		nvr.cameras.find((c) => c.id === id) ?? nvr.removed.find((c) => c.id === id);
	const eventDaysOf = (id: string) => {
		const r = cameraOf(id)?.record;
		return r ? r.eventDays || r.retainDays : 0;
	};
	let unkeeping = $state(false);
	let keepBusy = $state(false);
	async function keep(on: boolean) {
		if (!open) return;
		keepBusy = true;
		detailError = '';
		try {
			const changed = on
				? await api.post<NvrEvent>(`${eventBase}/keep`, {})
				: await api.delete<NvrEvent>(`${eventBase}/keep`);
			open = { ...open, kept: changed.kept, keptExport: changed.keptExport };
			const id = open.id;
			events = events.map((e) =>
				e.id === id ? { ...e, kept: changed.kept, keptExport: changed.keptExport } : e,
			);
			unkeeping = false;
		} catch (err) {
			detailError = apiErrorText(err);
		} finally {
			keepBusy = false;
		}
	}

	let removing = $state(false);
	let removeBusy = $state(false);
	async function remove() {
		if (!open) return;
		removeBusy = true;
		detailError = '';
		try {
			// A kept event: the dialog said that its saved video goes too.
			await api.delete(open.kept ? `${eventBase}?confirmKept=1` : eventBase);
			const id = open.id;
			events = events.filter((e) => e.id !== id);
			removing = false;
			open = null;
		} catch (err) {
			detailError = apiErrorText(err);
		} finally {
			removeBusy = false;
		}
	}
</script>

<PageHead title={t('nvr.menu.events')} />

{#if cameras.length === 0 && !(admin && nvr.removed.length)}
	<EmptyState
		title={t('nvr.events.noneTitle')}
		text={admin ? t('nvr.live.noneAdmin') : t('nvr.rec.noneUser')}
	/>
{:else}
	<div class="filters">
		<label
			><span>{t('nvr.rec.camera')}</span>
			<select class="field" bind:value={camera} aria-label={t('nvr.rec.camera')}>
				<option value="">{t('nvr.events.allCameras')}</option>
				{#each cameras as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
				{#if admin && nvr.removed.length}
					<optgroup label={t('nvr.removed.optgroup')}>
						{#each nvr.removed as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
					</optgroup>
				{/if}
			</select>
		</label>
		<label
			><span>{t('nvr.events.kind')}</span>
			<select class="field" bind:value={kind} aria-label={t('nvr.events.kind')}>
				<option value="">{t('nvr.events.allKinds')}</option>
				<option value="persons">{t('nvr.detect.persons')}</option>
				<option value="vehicles">{t('nvr.detect.vehicles')}</option>
				<option value="animals">{t('nvr.detect.animals')}</option>
			</select>
		</label>
		<label
			><span>{t('nvr.events.time')}</span>
			<select class="field" bind:value={span} aria-label={t('nvr.events.time')}>
				<option value="day">{t('nvr.events.spanDay')}</option>
				<option value="week">{t('nvr.events.spanWeek')}</option>
				<option value="all">{t('nvr.events.spanAll')}</option>
			</select>
		</label>
		<Segments
			compact
			options={[
				{ value: 'grid', label: t('nvr.events.viewGrid') },
				{ value: 'list', label: t('nvr.events.viewList') },
			]}
			bind:value={view}
			label={t('nvr.events.view')}
			onchange={(v) => {
				try {
					localStorage.setItem('titanos.nvr.events.view', v);
				} catch {
					/* only for this visit */
				}
			}}
		/>
	</div>

	<div class="seen-row">
		<p class="muted seen-hint">{t('nvr.events.seenHint')}</p>
		<button type="button" class="btn" disabled={seenBusy || !anyUnseen} onclick={allSeen}
			><Icon name="check" size={15} />{t('nvr.events.allSeen')}</button
		>
	</div>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if missing}
		<p class="muted" role="status">{t('nvr.events.unavailable')}</p>
	{:else if !loading && events.length === 0}
		<p class="muted" role="status">{t('nvr.events.noneFiltered')}</p>
	{:else if view === 'list'}
		<ul class="list">
			{#each events as event (event.id)}
				<li><EventCard {event} camera={nameOf(event.camera)} onopen={() => show(event)} /></li>
			{/each}
		</ul>
	{:else}
		<ul class="grid">
			{#each events as event (event.id)}
				<li>
					<button
						type="button"
						class="card"
						class:new={!event.reviewed}
						onclick={() => show(event)}
					>
						<span class="pic"
							><img
								src={apiPath(`/nvr/events/${encodeURIComponent(event.id)}/thumbnail.jpg`)}
								alt=""
								loading="lazy"
							/></span
						>
						<b
							>{labelName(event.label)} · {nameOf(event.camera)}{#if !event.reviewed}<span
									class="sr-only">, {t('nvr.events.unseen')}</span
								>{/if}</b
						>
						<small
							>{dayClock(event.start, locale)}{event.subLabel
								? ` · ${event.subLabel}`
								: ''}{event.plate && event.plate !== event.subLabel
								? ` · ${event.plate}`
								: ''}</small
						>
						{#if event.kept}<span class="kept"
								><Icon name="pin" size={13} />{t('nvr.events.keptBadge')}</span
							>{/if}
					</button>
				</li>
			{/each}
		</ul>
	{/if}
	{#if loading}<p class="muted" role="status">{t('common.loading')}</p>{/if}
	{#if next !== null && !loading}
		<div class="more">
			<button type="button" class="btn" onclick={() => load(true)}>{t('nvr.events.more')}</button>
		</div>
	{/if}
{/if}

{#if open}
	{@const e = open}
	<Dialog
		wide
		title="{labelName(e.label)} · {nameOf(e.camera)}"
		onclose={() => ((open = null), (removing = false))}
	>
		<div class="detail">
			{#if !recordingFailed}
				<!-- svelte-ignore a11y_media_has_caption -->
				<video
					bind:this={player}
					controls
					playsinline
					preload="auto"
					poster={e.hasSnapshot ? apiPath(`${eventBase}/snapshot.jpg`) : undefined}
				></video>
			{:else if keptSrc}
				<!-- svelte-ignore a11y_media_has_caption -->
				<video
					src={keptSrc}
					controls
					playsinline
					preload="auto"
					poster={e.hasSnapshot ? apiPath(`${eventBase}/snapshot.jpg`) : undefined}
				></video>
				<p class="muted play-note">{t('nvr.events.keptVideo')}</p>
			{:else if e.hasSnapshot}
				<img src={apiPath(`${eventBase}/snapshot.jpg`)} alt={t('nvr.events.snapshot')} />
			{:else}
				<p class="muted">{t('nvr.events.noMedia')}</p>
			{/if}
			{#if fromRecording && !recordingFailed}<p class="muted play-note">
					{t('nvr.events.fromRecording')}
				</p>{/if}
			{#if playMuted}<p class="muted play-note" role="status">
					{t('nvr.events.autoplayMuted')}
				</p>{/if}
			<dl>
				<div>
					<dt>{t('nvr.events.when')}</dt>
					<dd>
						{dayClock(e.start, locale)}{e.end === null
							? ` · ${t('nvr.events.running')}`
							: ` – ${clock(e.end, locale)} (${Math.max(1, Math.round(e.end - e.start)) === 1 ? t('nvr.events.secondsOne') : t('nvr.events.seconds', { seconds: Math.round(e.end - e.start) })})`}
					</dd>
				</div>
				{#if e.subLabel}<div>
						<dt>{t('nvr.events.recognized')}</dt>
						<dd>{e.subLabel}</dd>
					</div>{/if}
				{#if e.plate}<div>
						<dt>{t('nvr.events.plate')}</dt>
						<dd class="mono">{e.plate}</dd>
					</div>{/if}
				<div>
					<dt>{t('nvr.events.score')}</dt>
					<dd>{Math.round(e.score * 100)} %</dd>
				</div>
				{#if e.zones?.length}<div>
						<dt>{t('nvr.events.zones')}</dt>
						<dd>{e.zones.join(', ')}</dd>
					</div>{/if}
				<div>
					<dt>{t('nvr.events.kept')}</dt>
					<dd id="nvr-event-kept">
						{#if e.kept}<Icon name="pin" size={14} />
							{e.keptExport ? t('nvr.events.keptText') : t('nvr.events.keptNoVideo')}
						{:else}{t('nvr.events.notKept', { days: eventDaysOf(e.camera) || '–' })}{/if}
					</dd>
				</div>
			</dl>
			<div class="links">
				<a
					class="btn"
					href="/nvr/recordings?camera={encodeURIComponent(e.camera)}&at={Math.floor(e.start)}"
					><Icon name="play" size={15} />{t('nvr.events.inRecordings')}</a
				>
				{#if e.hasSnapshot}<a class="btn" href={apiPath(`${eventBase}/snapshot.jpg`)} download
						><Icon name="image" size={15} />{t('nvr.events.snapshotSave')}</a
					>{/if}
				{#if e.hasClip}<a class="btn" href={apiPath(`${eventBase}/clip.mp4?download=1`)} download
						><Icon name="download" size={15} />{t('nvr.events.clipSave')}</a
					>{/if}
			</div>
			{#if detailError}<p class="notice-error" role="alert">{detailError}</p>{/if}
			{#if admin && !e.kept && !settled(e)}<p class="muted">
					{t('nvr.events.keepRunning')}
				</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (open = null)}>{t('common.close')}</button>
				{#if admin}
					{#if e.kept}
						<button type="button" class="btn" disabled={keepBusy} onclick={() => (unkeeping = true)}
							>{t('nvr.events.unkeep')}</button
						>
					{:else}
						<button
							type="button"
							class="btn"
							disabled={keepBusy || !settled(e)}
							onclick={() => keep(true)}><Icon name="pin" size={15} />{t('nvr.events.keep')}</button
						>
					{/if}
					<button type="button" class="btn btn-danger" onclick={() => (removing = true)}
						>{t('common.delete')}</button
					>
				{/if}
			</div>
		</div>
	</Dialog>
{/if}
{#if open && unkeeping}
	<Confirm
		title={t('nvr.events.unkeepTitle')}
		text={[
			t('nvr.events.unkeepText', { days: eventDaysOf(open.camera) || '–' }),
			...(open.keptExport ? [t('nvr.events.unkeepVideo')] : []),
		]}
		confirm={t('nvr.events.unkeepConfirm')}
		danger
		busy={keepBusy}
		error={detailError}
		onconfirm={() => keep(false)}
		onclose={() => (unkeeping = false)}
	/>
{/if}
{#if open && removing}
	<Confirm
		title={open.kept ? t('nvr.events.deleteKeptTitle') : t('nvr.events.deleteTitle')}
		text={open.kept ? t('nvr.events.deleteKeptText') : t('nvr.events.deleteText')}
		confirm={t('common.delete')}
		danger
		busy={removeBusy}
		error={detailError}
		onconfirm={remove}
		onclose={() => (removing = false)}
	/>
{/if}

<style>
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		min-width: 0;
	}
	.play-note {
		margin: 0;
	}
	.links {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.links .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.seen-hint {
		margin: 0;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
		flex: 1 1 16rem;
	}
	.seen-row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
	}
	.seen-row .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	/* PLAN 88: a kept event says so in words, not only with a mark. */
	.kept {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		font-size: var(--text-sm);
		font-weight: 600;
	}
	dd :global(svg) {
		vertical-align: -2px;
	}
	.filters label {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--muted);
		font-size: var(--text-base);
		min-width: 0;
	}
	.filters select {
		max-width: 100%;
	}
	.grid,
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 13rem), 1fr));
		gap: var(--space-3);
	}
	.list {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: 100%;
		padding: var(--space-1-5);
		border: 1px solid var(--srow-line, var(--border));
		border-radius: 10px;
		background: var(--srow-bg, var(--surface));
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.card:hover {
		background: var(--srow-hover, var(--surface));
	}
	.card:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.pic {
		display: block;
		aspect-ratio: 16 / 10;
		border-radius: 7px;
		overflow: hidden;
		background: #0b0f14;
		margin-bottom: var(--space-1);
	}
	.pic img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.card b {
		font-weight: 600;
		font-size: var(--text-base);
	}
	.card.new b::after {
		content: '';
		display: inline-block;
		width: 7px;
		height: 7px;
		margin-left: 6px;
		border-radius: 50%;
		background: var(--accent);
		vertical-align: middle;
	}
	.card small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.more {
		display: flex;
		justify-content: center;
	}
	.detail {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
	}
	.detail video,
	.detail img {
		width: 100%;
		max-height: 60dvh;
		border-radius: 8px;
		background: #0b0f14;
		object-fit: contain;
	}
	dl {
		margin: 0;
		display: grid;
		gap: var(--space-1);
	}
	dl div {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-3);
	}
	dt {
		color: var(--muted);
		min-width: 7rem;
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.detail .dialog-actions .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	p {
		margin: 0;
	}
</style>
