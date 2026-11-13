<script lang="ts">
	import { untrack } from 'svelte';
	// PLAN 79-7: the picture of one camera. Live video over MSE (mse.ts); when
	// the browser has no MSE (iPhone before iOS 17.1), the stream cannot be
	// played or the connection keeps failing, a still picture refreshed every
	// two seconds stands in and live video is tried again from time to time.
	// Nothing is loaded while nobody can see the page (hidden tab, minimized
	// window) or the tile (scrolled away).
	import { apiPath, apiSocket } from '../cluster-node.ts';
	import { PAGE_VISIBILITY, pageHidden } from '../embedded.ts';
	import { errorText, t } from '../i18n/index.svelte.ts';
	import { startMse, type MseEnd, type MseSession } from './mse.ts';
	import { snapshotPath } from './nvr.svelte.ts';

	let {
		id,
		name,
		/** The small stream is enough for a small tile. */
		sub = false,
		/** Sound on (only for cameras whose sound is switched on). */
		sound = false,
		/** Only still pictures, at this interval (the desktop's widget). */
		stillOnly = false,
		stillMs = 2000,
		/** The height asked for still pictures. */
		height = 480,
		fit = 'cover',
		/** The tile says itself why there is no picture (camera offline). */
		quiet = false,
		onmode,
	}: {
		id: string;
		name: string;
		sub?: boolean;
		sound?: boolean;
		stillOnly?: boolean;
		stillMs?: number;
		height?: number;
		fit?: 'cover' | 'contain';
		quiet?: boolean;
		/** Tells the tile what is shown: live video, still pictures or nothing. */
		onmode?: (mode: 'live' | 'still' | 'none') => void;
	} = $props();

	let video = $state<HTMLVideoElement>();
	let root = $state<HTMLElement>();
	let mode = $state<'connecting' | 'live' | 'still'>('connecting');
	/** Why there is no live video, as an error code; '' = none. */
	let reason = $state('');
	let still = $state('');
	let stillFailed = $state(false);
	let visible = $state(true);
	let hidden = $state(pageHidden());

	$effect(() => {
		const update = () => (hidden = pageHidden());
		document.addEventListener('visibilitychange', update);
		document.addEventListener(PAGE_VISIBILITY, update);
		return () => {
			document.removeEventListener('visibilitychange', update);
			document.removeEventListener(PAGE_VISIBILITY, update);
		};
	});
	$effect(() => {
		if (!root || typeof IntersectionObserver === 'undefined') return;
		const observer = new IntersectionObserver((entries) => {
			visible = entries[entries.length - 1].isIntersecting;
		});
		observer.observe(root);
		return () => observer.disconnect();
	});
	const active = $derived(visible && !hidden);

	// The props as plain values. A tile passes `camera.id`, and the NVR's
	// poller hands over new camera objects every few seconds: an effect that
	// read the prop itself would follow the object and start the stream anew
	// with every poll (the picture went black every 6 s, owner 13.11.2026).
	// A derived only passes on a value that really changed.
	const cameraId = $derived(id);
	const small = $derived(sub);
	const stillOnlyNow = $derived(stillOnly);
	const stillEvery = $derived(stillMs);
	const stillHeight = $derived(height);

	// ---- Live video ----
	let failures = 0;
	$effect(() => {
		// Read here so that a change starts the stream anew.
		const camera = cameraId;
		const sub = small;
		const el = video;
		if (!active || stillOnlyNow || !el) return;
		let session: MseSession | null = null;
		let timer: ReturnType<typeof setTimeout> | undefined;
		let stopped = false;

		const start = () => {
			if (stopped) return;
			session = startMse(
				el,
				apiSocket(`/nvr/cameras/${encodeURIComponent(camera)}/live${sub ? '?stream=sub' : ''}`),
				() => {
					failures = 0;
					reason = '';
					mode = 'live';
				},
				(end: MseEnd, frame?: HTMLCanvasElement) => {
					session = null;
					if (stopped) return;
					if (end.kind === 'bye') {
						reason = end.reason === 'forbidden' ? 'forbidden' : '';
						mode = 'still';
						return;
					}
					// The last picture stays until a new still or the video is back:
					// never an empty tile in between.
					frame?.toBlob(
						(blob) => {
							if (blob && !stopped && mode !== 'live') showStill(URL.createObjectURL(blob));
						},
						'image/jpeg',
						0.85,
					);
					failures++;
					if (end.kind === 'error') reason = end.code;
					// Still pictures stand in after the first failure; live video is
					// tried again, slower each time (2 s … 30 s).
					mode = 'still';
					if (end.kind === 'unsupported') return;
					timer = setTimeout(start, Math.min(30000, 2000 * 2 ** Math.min(failures - 1, 4)));
				},
			);
		};
		start();
		return () => {
			stopped = true;
			clearTimeout(timer);
			session?.close();
		};
	});
	$effect(() => {
		if (video) video.muted = !sound;
	});

	// ---- Still pictures ----
	/** Shows a picture; the old one stays until the new one is painted. */
	function showStill(url: string) {
		const old = untrack(() => still);
		still = url;
		stillFailed = false;
		if (old) setTimeout(() => URL.revokeObjectURL(old), 1000);
	}
	const wantsStill = $derived(active && (stillOnlyNow || mode !== 'live'));
	$effect(() => {
		const camera = cameraId;
		const px = stillHeight;
		const every = stillEvery;
		if (!wantsStill) return;
		let stopped = false;
		let timer: ReturnType<typeof setTimeout> | undefined;
		// Fetched as a blob: the answer is never cached (no-store), so handing
		// the address to the picture would load it twice and flicker.
		const load = async () => {
			if (stopped) return;
			let ok = false;
			try {
				const res = await fetch(apiPath(snapshotPath(camera, px, Date.now())), {
					credentials: 'same-origin',
				});
				if (res.ok && res.headers.get('content-type')?.startsWith('image/')) {
					const url = URL.createObjectURL(await res.blob());
					if (stopped) URL.revokeObjectURL(url);
					else {
						showStill(url);
						ok = true;
					}
				}
			} catch {
				// No connection: tried again below.
			}
			if (stopped) return;
			stillFailed = !ok;
			timer = setTimeout(load, ok ? every : Math.max(every, 5000));
		};
		void load();
		return () => {
			stopped = true;
			clearTimeout(timer);
		};
	});
	$effect(() => () => {
		if (still) URL.revokeObjectURL(untrack(() => still));
	});

	const shown = $derived<'live' | 'still' | 'none'>(
		mode === 'live' && !stillOnly ? 'live' : still && !stillFailed ? 'still' : 'none',
	);
	$effect(() => onmode?.(shown));
</script>

<div class="live-video" class:contain={fit === 'contain'} bind:this={root}>
	{#if still}<img src={still} alt="" class:off={shown === 'live'} />{/if}
	{#if !stillOnly}
		<!-- svelte-ignore a11y_media_has_caption -->
		<video
			bind:this={video}
			class:off={shown !== 'live'}
			autoplay
			playsinline
			muted
			aria-label={t('nvr.live.videoOf', { name })}
		></video>
	{/if}
	{#if shown === 'none' && !quiet}
		<p class="none" role="status">
			{reason === 'forbidden'
				? t('nvr.live.forbidden')
				: reason && reason !== 'nvr_no_picture'
					? errorText(reason)
					: stillFailed
						? t('nvr.live.noPicture')
						: t('nvr.live.connecting')}
		</p>
	{/if}
</div>

<style>
	.live-video {
		position: absolute;
		inset: 0;
		background: #0b0f14;
		overflow: hidden;
	}
	img,
	video {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.contain img,
	.contain video {
		object-fit: contain;
	}
	.off {
		visibility: hidden;
	}
	.none {
		position: absolute;
		inset: 0;
		margin: 0;
		display: grid;
		place-items: center;
		padding: var(--space-3);
		text-align: center;
		color: #c9d1d9;
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
</style>
