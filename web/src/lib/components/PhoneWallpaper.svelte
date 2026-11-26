<script lang="ts">
	import { desktop } from '#lib/desktop/wm.svelte.ts';
	import { dayTint, wallpaperCss } from '#lib/desktop/wallpapers.ts';
	import LiveBackground from '#lib/live/LiveBackground.svelte';
	import { isLivePicture } from '#lib/live/shaders.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { resolvePalette } from '#lib/theme/themes.ts';
	import { sanitizeDesktop } from '#lib/desktop/prefs.ts';
	import { session } from '#lib/session.svelte.ts';

	const wallpaper = $derived(
		desktop.from && desktop.from === session.prefsLoaded
			? desktop.prefs.wallpaper
			: sanitizeDesktop(session.desktop).wallpaper,
	);
	const light = $derived(!resolvePalette(appearance.current, appearance.systemLight).dark);
	const picture = $derived(
		wallpaper.source === 'builtin' && isLivePicture(wallpaper.id) ? wallpaper.id : null,
	);
	const image = $derived(
		wallpaper.source === 'custom'
			? `url("/api/v1/background?v=${wallpaper.version}")`
			: wallpaperCss(wallpaper.id, light),
	);
	let tint = $state(dayTint());
	$effect(() => {
		const timer = setInterval(() => (tint = dayTint()), 600000);
		return () => clearInterval(timer);
	});
</script>

<div
	class="phone-wallpaper"
	aria-hidden="true"
	style:background-image={image}
	style:--tint="{tint}deg"
>
	{#if picture && wallpaper.live}<LiveBackground {picture} pointer={false} />{/if}
</div>

<style>
	.phone-wallpaper {
		position: fixed;
		inset: 0;
		z-index: -1;
		pointer-events: none;
		background-size: cover;
		background-position: center;
		filter: hue-rotate(var(--tint)) var(--wallpaper-filter, saturate(1));
	}
	.phone-wallpaper::after {
		content: '';
		position: absolute;
		inset: 0;
		background: linear-gradient(
			to bottom,
			color-mix(in srgb, var(--bg) 56%, transparent),
			color-mix(in srgb, var(--bg) 35%, transparent) 48%,
			color-mix(in srgb, var(--bg) 66%, transparent)
		);
	}
</style>
