<script lang="ts">
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

	// ---- Live video ----
	let failures = 0;
	$effect(() => {
		// Read here so that a change starts the stream anew.
		const camera = id;
		const small = sub;
		const el = video;
		if (!active || stillOnly || !el) return;
		let session: MseSession | null = null;
		let timer: ReturnType<typeof setTimeout> | undefined;
		let stopped = false;

		const start = () => {
			if (stopped) return;
			session = startMse(
				el,
				apiSocket(`/nvr/cameras/${encodeURIComponent(camera)}/live${small ? '?stream=sub' : ''}`),
				() => {
					failures = 0;
					reason = '';
					mode = 'live';
				},
				(end: MseEnd) => {
					session = null;
					if (stopped) return;
					if (end.kind === 'bye') {
						reason = end.reason === 'forbidden' ? 'forbidden' : '';
						mode = 'still';
						return;
					}
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
	const wantsStill = $derived(active && (stillOnly || mode !== 'live'));
	$effect(() => {
		const camera = id;
		const px = height;
		if (!wantsStill) return;
		let stopped = false;
		let timer: ReturnType<typeof setTimeout> | undefined;
		const load = () => {
			if (stopped) return;
			const src = apiPath(snapshotPath(camera, px, Date.now()));
			const img = new Image();
			img.onload = () => {
				if (stopped) return;
				still = src;
				stillFailed = false;
				timer = setTimeout(load, stillMs);
			};
			img.onerror = () => {
				if (stopped) return;
				stillFailed = true;
				timer = setTimeout(load, Math.max(stillMs, 5000));
			};
			img.src = src;
		};
		load();
		return () => {
			stopped = true;
			clearTimeout(timer);
		};
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
	{#if shown === 'none'}
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
