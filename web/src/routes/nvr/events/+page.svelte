<script lang="ts">
	// PLAN 79-7: what the cameras saw – filtered by camera, kind and time, as
	// pictures or as a list; an event opens with its clip, its snapshot, the
	// recognized face or plate, download and (administrators) delete.
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
	import { mayRecordings, nvr, nvrAdmin } from '#lib/nvr/nvr.svelte.ts';
	import { clock, dayClock, labelName, OBJECT_GROUPS } from '#lib/nvr/texts.ts';
	import type { EventList, NvrEvent } from '#lib/nvr/types.ts';

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const admin = $derived(nvrAdmin());
	const cameras = $derived(nvr.cameras.filter(mayRecordings));
	const nameOf = (id: string) => nvr.cameras.find((c) => c.id === id)?.name ?? id;

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

	// ---- One event ----
	let open = $state<NvrEvent | null>(null);
	let detailError = $state('');
	function show(event: NvrEvent) {
		open = event;
		detailError = '';
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

	let removing = $state(false);
	let removeBusy = $state(false);
	async function remove() {
		if (!open) return;
		removeBusy = true;
		detailError = '';
		try {
			await api.delete(eventBase);
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

{#if cameras.length === 0}
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

	<p class="muted seen-hint">{t('nvr.events.seenHint')}</p>
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
			{#if e.hasClip}
				<!-- svelte-ignore a11y_media_has_caption -->
				<video
					controls
					playsinline
					preload="metadata"
					poster={e.hasSnapshot ? apiPath(`${eventBase}/snapshot.jpg`) : undefined}
					src={apiPath(`${eventBase}/clip.mp4`)}
				></video>
			{:else if e.hasSnapshot}
				<img src={apiPath(`${eventBase}/snapshot.jpg`)} alt={t('nvr.events.snapshot')} />
			{:else}
				<p class="muted">{t('nvr.events.noMedia')}</p>
			{/if}
			<dl>
				<div>
					<dt>{t('nvr.events.when')}</dt>
					<dd>
						{dayClock(e.start, locale)}{e.end === null
							? ` · ${t('nvr.events.running')}`
							: ` – ${clock(e.end, locale)} (${t('nvr.events.seconds', { seconds: Math.max(1, Math.round(e.end - e.start)) })})`}
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
				{#if e.kept}<div>
						<dt>{t('nvr.events.kept')}</dt>
						<dd>{t('nvr.events.keptText')}</dd>
					</div>{/if}
			</dl>
			{#if detailError}<p class="notice-error" role="alert">{detailError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (open = null)}>{t('common.close')}</button>
				<a
					class="btn"
					href="/nvr/recordings?camera={encodeURIComponent(e.camera)}&at={Math.floor(e.start)}"
					>{t('nvr.events.inRecordings')}</a
				>
				{#if e.hasSnapshot}<a class="btn" href={apiPath(`${eventBase}/snapshot.jpg`)} download
						><Icon name="image" size={15} />{t('nvr.events.snapshotSave')}</a
					>{/if}
				{#if e.hasClip}<a class="btn" href={apiPath(`${eventBase}/clip.mp4?download=1`)} download
						><Icon name="download" size={15} />{t('nvr.events.clipSave')}</a
					>{/if}
				{#if admin}<button type="button" class="btn btn-danger" onclick={() => (removing = true)}
						>{t('common.delete')}</button
					>{/if}
			</div>
		</div>
	</Dialog>
{/if}
{#if open && removing}
	<Confirm
		title={t('nvr.events.deleteTitle')}
		text={t('nvr.events.deleteText')}
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
	.seen-hint {
		margin: 0;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
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
