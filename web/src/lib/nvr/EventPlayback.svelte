<script lang="ts">
	import { api } from '../api.ts';
	import { apiPath } from '../cluster-node.ts';
	import { i18n, t } from '../i18n/index.svelte.ts';
	import { clip, mediaToWall } from './timeline.ts';
	import { cameraName } from './nvr.svelte.ts';
	import { clock } from './texts.ts';
	import type { NvrEvent, NvrExport, RecordingList, Segment } from './types.ts';
	import { attachVod, autoplay } from './vod.ts';

	let { event, phone }: { event: NvrEvent; phone: boolean } = $props();
	const base = $derived(`/nvr/cameras/${encodeURIComponent(event.camera)}`);
	const poster = $derived(
		event.hasSnapshot
			? apiPath(`/nvr/events/${encodeURIComponent(event.id)}/snapshot.jpg`)
			: undefined,
	);
	// Frigate's clip download does not support seeking on iPhone. Its VOD
	// represents the same event, with enough pre- and post-roll to orient oneself.
	const from = $derived(Math.floor(event.start) - 5);
	const to = $derived(Math.max(from + 30, Math.ceil(event.end ?? Date.now() / 1000) + 5));
	let vodFailed = $state(false);
	let exportFailed = $state(false);
	let unsupported = $state(false);
	let playingMuted = $state(false);
	let video = $state<HTMLVideoElement>();
	let duration = $state(0);
	let position = $state(0);
	let timing = $state<{
		from: number;
		to: number;
		segments: Segment[];
	} | null>(null);
	const saved = $derived(vodFailed && !!event.keptExport && !exportFailed);
	const playable = $derived(!vodFailed || saved);
	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');
	const indexedDuration = $derived(
		timing?.segments.reduce((sum, part) => sum + part.end - part.start, 0) ?? 0,
	);
	const wallTime = $derived(wallAt(position));

	function wallAt(seconds: number): number | null {
		if (!timing) return null;
		if (timing.segments.length && (!saved || Math.abs(duration - indexedDuration) < 1))
			return mediaToWall(timing.segments, timing.from, timing.to, seconds);
		// With no remaining record index, only use the export bounds when its
		// duration covers that interval (the recording could contain gaps).
		if (duration > 0 && Math.abs(duration - (timing.to - timing.from)) < 1)
			return timing.from + seconds;
		return null;
	}

	$effect(() => {
		const el = video;
		if (!el || vodFailed) return;
		let active = true;
		let detach: (() => void) | undefined;
		void attachVod(
			el,
			apiPath(`${base}/vod/${from}/${to}/master.m3u8`),
			async () => {
				if (!active) return;
				const muted = await autoplay(el);
				if (active) playingMuted = muted;
			},
			(codec) => {
				if (active) {
					unsupported = codec;
					vodFailed = true;
				}
			},
		).then((stop) => {
			if (active) detach = stop;
			else stop();
		});
		return () => {
			active = false;
			detach?.();
		};
	});

	// The media timeline joins recorded parts together. Use the NVR's segment
	// index to place them at their actual wall times, including after a gap.
	$effect(() => {
		const cameraBase = base,
			keptId = event.keptExport,
			useExport = saved;
		const start = from,
			end = to;
		timing = null;
		if (!phone || !playable) return;
		const controller = new AbortController();
		const { signal } = controller;
		async function readTiming() {
			let a = start,
				b = end;
			if (useExport) {
				const { exports } = await api.get<{ exports: NvrExport[] }>('/nvr/exports', signal);
				const item = exports?.find((item) => item.id === keptId);
				if (!item?.from || item.to <= item.from) return;
				a = item.from;
				b = item.to;
			}
			let segments: Segment[] = [];
			try {
				const result = await api.get<RecordingList>(
					`${cameraBase}/recordings?from=${a}&to=${b}`,
					signal,
				);
				segments = clip(result.segments ?? [], a, b);
			} finally {
				if (!signal.aborted) timing = { from: a, to: b, segments };
			}
		}
		void readTiming().catch(() => {});
		return () => controller.abort();
	});

	function sync(el: HTMLVideoElement) {
		duration = Number.isFinite(el.duration) && el.duration > 0 ? el.duration : 0;
		position = Number.isFinite(el.currentTime) ? el.currentTime : 0;
	}
	function seek(seconds: number) {
		if (!video || !duration || !Number.isFinite(seconds)) return;
		video.currentTime = Math.min(duration, Math.max(0, seconds));
		sync(video);
	}
	function format(seconds: number) {
		const whole = Math.max(0, Math.floor(seconds));
		return Math.floor(whole / 60) + ':' + String(whole % 60).padStart(2, '0');
	}
	function failed() {
		if (saved) exportFailed = true;
		else vodFailed = true;
		playingMuted = false;
		duration = position = 0;
	}
	const frames = $derived.by(() => {
		if (!phone || !playable || saved || !duration || !timing?.segments.length) return [];
		return Array.from({ length: 6 }, (_, i) => wallAt((duration * (i + 0.5)) / 6));
	});
</script>

{#if playable}
	{#key saved}
		<!-- svelte-ignore a11y_media_has_caption -->
		<video
			bind:this={video}
			controls
			playsinline
			preload="auto"
			{poster}
			src={saved && event.keptExport
				? apiPath(`/nvr/exports/${encodeURIComponent(event.keptExport)}/download`)
				: undefined}
			aria-label={t('nvr.rec.videoOf', { name: cameraName(event.camera) })}
			onloadedmetadata={(e) => sync(e.currentTarget)}
			ondurationchange={(e) => sync(e.currentTarget)}
			ontimeupdate={(e) => sync(e.currentTarget)}
			onseeked={(e) => sync(e.currentTarget)}
			onerror={failed}
		></video>
	{/key}
	{#if phone && duration > 0}
		<div class="clip-control">
			<div class="times">
				<strong>{format(position)} / {format(Math.round(duration))}</strong>
				{#if wallTime !== null}<time
						datetime={new Date(wallTime * 1000).toISOString()}
						aria-label={t('nvr.events.captureTime')}>{clock(wallTime, locale, true)}</time
					>{/if}
			</div>
			<div class="scrub" data-no-edge-swipe>
				<div class="frames" aria-hidden="true">
					{#each frames as at, i (i)}
						{#if at !== null}<img
								alt=""
								src={apiPath(`${base}/recordings/frame.jpg?at=${at.toFixed(3)}&height=90`)}
								onerror={(e) => e.currentTarget.classList.add('missing')}
							/>{/if}
					{/each}
				</div>
				<input
					type="range"
					min="0"
					max={duration}
					step="0.1"
					value={position}
					aria-label={t('nvr.events.clipTimeline')}
					aria-valuetext={format(position) +
						(wallTime === null ? '' : ' · ' + clock(wallTime, locale, true))}
					oninput={(e) => seek(e.currentTarget.valueAsNumber)}
				/>
			</div>
		</div>
	{/if}
	{#if saved}<p class="muted">{t('nvr.events.keptVideo')}</p>
	{:else if !event.hasClip}<p class="muted">{t('nvr.events.fromRecording')}</p>{/if}
	{#if playingMuted}<p class="muted" role="status">{t('nvr.events.autoplayMuted')}</p>{/if}
{:else}
	{#if poster}<img class="snapshot" src={poster} alt={t('nvr.events.snapshot')} />{/if}
	<p class="muted" role="status">
		{t(unsupported ? 'nvr.rec.unsupported' : poster ? 'nvr.rec.playFailed' : 'nvr.events.noMedia')}
	</p>
{/if}

<style>
	video,
	.snapshot {
		display: block;
		width: 100%;
		max-height: 60dvh;
		border-radius: 12px;
		background: #0b0f14;
		object-fit: contain;
	}
	p {
		margin: 0;
		font-size: var(--text-sm);
	}
	.clip-control {
		padding: 9px 12px;
		border: 1px solid var(--border);
		border-radius: 13px;
		background: color-mix(in srgb, var(--surface) 86%, transparent);
	}
	.times {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		font-variant-numeric: tabular-nums;
		font-size: var(--text-sm);
	}
	time {
		color: var(--muted);
	}
	.scrub {
		position: relative;
		margin-top: 4px;
		height: 48px;
	}
	.frames {
		position: absolute;
		inset: 5px 0;
		display: grid;
		grid-template-columns: repeat(6, minmax(0, 1fr));
		background: var(--chip);
		border-radius: 7px;
		overflow: hidden;
	}
	.frames img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		filter: brightness(0.76);
	}
	.frames img:global(.missing) {
		visibility: hidden;
	}
	input {
		position: relative;
		width: 100%;
		height: 48px;
		margin: 0;
		appearance: none;
		-webkit-appearance: none;
		background: transparent;
		cursor: pointer;
		touch-action: pan-y;
	}
	input::-webkit-slider-runnable-track {
		height: 38px;
		background: transparent;
	}
	input::-webkit-slider-thumb {
		appearance: none;
		-webkit-appearance: none;
		height: 38px;
		width: 10px;
		border-radius: 4px;
		background: var(--text);
		box-shadow: 0 0 0 2px var(--bg);
	}
	input::-moz-range-thumb {
		height: 38px;
		width: 10px;
		border: 0;
		border-radius: 4px;
		background: var(--text);
		box-shadow: 0 0 0 2px var(--bg);
	}
	input:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
		border-radius: 8px;
	}
</style>
