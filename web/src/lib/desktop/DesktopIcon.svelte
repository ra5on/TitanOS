<script lang="ts" module>
	// PLAN 61: the filled, two-tone symbols of the desktop's programs, drawn
	// for TitanOS on a 24 px grid. White on a gradient; `c` is the darker
	// colour of the gradient for details.
	const GLYPHS: Record<string, string> = {
		files:
			'<path d="M2.5 6.5A2.5 2.5 0 0 1 5 4h4.2l2.3 2.3H19a2.5 2.5 0 0 1 2.5 2.5V9H2.5Z" fill="#fff" fill-opacity=".55"/><path d="M2.5 9.5h19v8A2.5 2.5 0 0 1 19 20H5a2.5 2.5 0 0 1-2.5-2.5Z" fill="#fff"/>',
		docker:
			'<rect x="4" y="8" width="4" height="3.4" rx=".8" fill="#fff"/><rect x="9" y="8" width="4" height="3.4" rx=".8" fill="#fff"/><rect x="14" y="8" width="4" height="3.4" rx=".8" fill="#fff"/><rect x="9" y="4" width="4" height="3.4" rx=".8" fill="#fff" fill-opacity=".7"/><path d="M2.5 12.6h19c-.3 4.4-4.2 7.4-9.5 7.4s-9-3-9.5-7.4Z" fill="#fff"/><circle cx="7" cy="15.6" r="1" fill="var(--c)"/>',
		vm: '<rect x="2.5" y="3.5" width="19" height="13" rx="2.5" fill="#fff"/><rect x="5" y="6" width="14" height="8" rx="1" fill="var(--c)" fill-opacity=".35"/><path d="M9 20.5h6M12 16.5v4" stroke="#fff" stroke-width="2" stroke-linecap="round"/>',
		lxc: '<path d="M12 2.5 3 7.5l9 5 9-5Z" fill="#fff"/><path d="M3 8.5v8l8.4 4.7v-8Z" fill="#fff" fill-opacity=".7"/><path d="M21 8.5v8l-8.4 4.7v-8Z" fill="#fff" fill-opacity=".45"/>',
		storage:
			'<rect x="3.5" y="3" width="17" height="5.2" rx="1.6" fill="#fff"/><rect x="3.5" y="9.4" width="17" height="5.2" rx="1.6" fill="#fff" fill-opacity=".8"/><rect x="3.5" y="15.8" width="17" height="5.2" rx="1.6" fill="#fff" fill-opacity=".6"/><circle cx="17" cy="5.6" r="1" fill="#4ade80"/><circle cx="17" cy="12" r="1" fill="#4ade80"/><circle cx="17" cy="18.4" r="1" fill="#4ade80"/>',
		shares:
			'<circle cx="6" cy="12" r="3.2" fill="#fff"/><circle cx="18" cy="5.5" r="3.2" fill="#fff"/><circle cx="18" cy="18.5" r="3.2" fill="#fff"/><path d="M8.5 10.6 15.4 7M8.5 13.4l6.9 3.6" stroke="#fff" stroke-width="2"/>',
		backup:
			'<path d="M12 3a9 9 0 1 1-8.5 6" stroke="#fff" stroke-width="2.6" fill="none" stroke-linecap="round"/><path d="M2.5 3.8v5.6h5.6Z" fill="#fff"/><path d="M12 7.5V12l3.2 2" stroke="#fff" stroke-width="2.4" fill="none" stroke-linecap="round"/>',
		store:
			'<rect x="3" y="3" width="8" height="8" rx="2.4" fill="#fff"/><rect x="13" y="3" width="8" height="8" rx="2.4" fill="#fff" fill-opacity=".7"/><rect x="3" y="13" width="8" height="8" rx="2.4" fill="#fff" fill-opacity=".7"/><path d="M17 13.5v7M13.5 17h7" stroke="#fff" stroke-width="2.6" stroke-linecap="round"/>',
		// 0.95 (owner 15 Nov 2026, "Kern und Ringe"): the core of the
		// Sprechmodus with two arcs around it, in the agent's orange on its
		// own dark ground (gradient "ember").
		agent:
			'<circle cx="12" cy="12" r="3.7" fill="#f2915f"/><circle cx="10.8" cy="10.7" r="1.3" fill="#ffd9bf" fill-opacity=".8"/><path d="M12 5.2a6.8 6.8 0 0 1 6.4 4.5M12 18.8a6.8 6.8 0 0 1-6.4-4.5" stroke="#e07a4c" stroke-width="1.9" fill="none" stroke-linecap="round"/><path d="M20.3 12a8.3 8.3 0 0 1-2.4 5.9M3.7 12a8.3 8.3 0 0 1 2.4-5.9" stroke="#e07a4c" stroke-opacity=".6" stroke-width="1" fill="none" stroke-linecap="round"/>',
		monitor:
			'<rect x="2.5" y="3.5" width="19" height="17" rx="3" fill="#fff" fill-opacity=".25"/><path d="M5 16l3.5-5 3 2.5L16 7l3 5" stroke="#fff" stroke-width="2.4" fill="none" stroke-linecap="round" stroke-linejoin="round"/>',
		logs: '<path d="M6 2.5h8.5L19 7v13a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 5 20V4A1.5 1.5 0 0 1 6 2.5Z" fill="#fff"/><path d="M8.5 11h7M8.5 14.5h7M8.5 18h4" stroke="var(--c)" stroke-width="1.8" stroke-linecap="round"/>',
		terminal:
			'<rect x="2.5" y="3.5" width="19" height="17" rx="3" fill="#fff" fill-opacity=".2"/><path d="m6.5 9 3.5 3-3.5 3" stroke="#fff" stroke-width="2.4" fill="none" stroke-linecap="round" stroke-linejoin="round"/><path d="M12.5 16h5" stroke="#fff" stroke-width="2.4" stroke-linecap="round"/>',
		shield:
			'<path d="M12 2.5 4 5.5v6c0 5.2 3.4 8.8 8 10 4.6-1.2 8-4.8 8-10v-6Z" fill="#fff"/><path d="m8.5 12 2.5 2.5 4.5-5" stroke="var(--c)" stroke-width="2.2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>',
		settings:
			'<path d="M4 6.5h16M4 12h16M4 17.5h16" stroke="var(--c)" stroke-opacity=".45" stroke-width="2.2" stroke-linecap="round"/><circle cx="15" cy="6.5" r="2.6" fill="var(--c)"/><circle cx="8.5" cy="12" r="2.6" fill="var(--c)"/><circle cx="13" cy="17.5" r="2.6" fill="var(--c)"/>',
		// PLAN 79-7: a camera on its mount with a recording dot.
		nvr: '<rect x="3" y="5" width="13.5" height="9.5" rx="2.2" fill="#fff"/><path d="m16.5 8.2 4.3-2.4v8.4l-4.3-2.4Z" fill="#fff" fill-opacity=".7"/><circle cx="7" cy="8.6" r="1.5" fill="var(--c)"/><path d="M9.7 14.5v3.2M6 19.5h7.4" stroke="#fff" stroke-width="2" stroke-linecap="round"/>',
		// 1.11: T-Kalender, a calendar sheet; Kontakte, a card with a person.
		calendar:
			'<rect x="3" y="4.5" width="18" height="16.5" rx="3" fill="#fff"/><path d="M3 9.5h18V7.5a3 3 0 0 0-3-3H6a3 3 0 0 0-3 3Z" fill="var(--c)" fill-opacity=".55"/><path d="M8 2.8v3.6M16 2.8v3.6" stroke="#fff" stroke-width="2" stroke-linecap="round"/><rect x="6.5" y="12" width="3" height="2.6" rx=".6" fill="var(--c)"/><rect x="10.5" y="12" width="3" height="2.6" rx=".6" fill="var(--c)" fill-opacity=".45"/><rect x="14.5" y="12" width="3" height="2.6" rx=".6" fill="var(--c)" fill-opacity=".45"/><rect x="6.5" y="16" width="3" height="2.6" rx=".6" fill="var(--c)" fill-opacity=".45"/><rect x="10.5" y="16" width="3" height="2.6" rx=".6" fill="var(--c)" fill-opacity=".45"/>',
		contacts:
			'<rect x="4" y="2.5" width="16" height="19" rx="3" fill="#fff"/><circle cx="12" cy="10" r="3.2" fill="var(--c)"/><path d="M6.8 18.2a5.2 5.2 0 0 1 10.4 0Z" fill="var(--c)"/><path d="M2.5 7h2.2M2.5 12h2.2M2.5 17h2.2" stroke="#fff" stroke-opacity=".7" stroke-width="1.8" stroke-linecap="round"/>',
		// 1.05: T-Mail, a closed letter.
		mail: '<rect x="2.5" y="5" width="19" height="14" rx="2.5" fill="#fff"/><path d="m4 7.2 8 6 8-6" stroke="var(--c)" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>',
		users:
			'<circle cx="9" cy="8" r="3.6" fill="#fff"/><path d="M2.5 20a6.5 6.5 0 0 1 13 0Z" fill="#fff"/><circle cx="17" cy="9" r="2.6" fill="#fff" fill-opacity=".7"/><path d="M15.8 14.2A5 5 0 0 1 21.5 20h-4.2a8 8 0 0 0-1.5-5.8Z" fill="#fff" fill-opacity=".7"/>',
		planner:
			'<rect x="3" y="4.5" width="18" height="16" rx="2.5" fill="#fff"/><rect x="3" y="4.5" width="18" height="4.5" rx="2" fill="#fff" fill-opacity=".6"/><path d="M8 3v3M16 3v3" stroke="#fff" stroke-width="2" stroke-linecap="round"/><path d="m8.5 14.5 2.3 2.3 4.7-4.8" stroke="var(--c)" stroke-width="2.1" fill="none" stroke-linecap="round" stroke-linejoin="round"/>',
		account:
			'<circle cx="12" cy="8.5" r="4.2" fill="var(--c)"/><path d="M4 20.5a8 8 0 0 1 16 0Z" fill="var(--c)"/>',
		overview:
			'<rect x="3" y="3" width="8" height="10" rx="2" fill="#fff"/><rect x="13" y="3" width="8" height="6" rx="2" fill="#fff" fill-opacity=".7"/><rect x="13" y="11" width="8" height="10" rx="2" fill="#fff"/><rect x="3" y="15" width="8" height="6" rx="2" fill="#fff" fill-opacity=".7"/>',
		screen:
			'<rect x="2.5" y="4" width="19" height="13" rx="2.5" fill="#fff"/><path d="m10 8 5 2.5-5 2.5Z" fill="var(--c)"/><path d="M8 20.5h8" stroke="#fff" stroke-width="2" stroke-linecap="round"/>',
	};
	export const GRADIENTS: Record<string, [string, string]> = {
		blue: ['#5aa9ff', '#1f6fe0'],
		teal: ['#36d1c4', '#0e8f9a'],
		green: ['#4fd67a', '#1b9a4b'],
		orange: ['#ffb547', '#f07a1a'],
		violet: ['#a78bfa', '#6d3fe0'],
		grey: ['#e8ecf1', '#a4b0bd'],
		dark: ['#4a5563', '#1f2730'],
		red: ['#ff7a7a', '#e0393e'],
		yellow: ['#ffd84d', '#f0a91a'],
		pink: ['#ff8fc7', '#e0479a'],
		ember: ['#2c1f18', '#0d0f14'],
	};
</script>

<script lang="ts">
	import { onMount } from 'svelte';
	import { publicURL } from '#lib/app-links.ts';
	let {
		glyph = '',
		gradient = 'blue',
		image = '',
		name = '',
		size = 56,
	}: { glyph?: string; gradient?: string; image?: string; name?: string; size?: number } = $props();
	let failed = $state('');
	const url = $derived(
		image.startsWith('https://') || image.startsWith('/')
			? image.startsWith('/')
				? image
				: publicURL(image)
			: '',
	);
	const art = $derived(
		[
			'files',
			'docker',
			'mail',
			'calendar',
			'shares',
			'store',
			'settings',
			'storage',
			'updates',
		].includes(glyph),
	);
	let today = $state(new Date().getDate());
	onMount(() => {
		if (glyph !== 'calendar') return;
		const update = () => (today = new Date().getDate());
		const timer = setInterval(update, 60000);
		document.addEventListener('visibilitychange', update);
		return () => {
			clearInterval(timer);
			document.removeEventListener('visibilitychange', update);
		};
	});
	const colors = $derived(GRADIENTS[gradient] ?? GRADIENTS.blue);
	const initials = $derived(
		name
			.replace(/[^\p{L}\p{N}]/gu, '')
			.slice(0, 1)
			.toUpperCase() || '?',
	);
</script>

{#if url && failed !== url}
	<span class="icon app" style:width="{size}px" style:height="{size}px" aria-hidden="true">
		<img src={url} alt="" referrerpolicy="no-referrer" onerror={() => (failed = url)} />
	</span>
{:else if art}
	<span
		class="icon artwork"
		style:width="{size}px"
		style:height="{size}px"
		style:background-image={'url("/icons/nordlicht/' + glyph + '.webp")'}
		aria-hidden="true"
	>
		{#if glyph === 'calendar'}<span class="date" style:font-size="{size * 0.44}px">{today}</span
			>{/if}
	</span>
{:else}
	<span
		class="icon"
		class:light={gradient === 'grey'}
		style:width="{size}px"
		style:height="{size}px"
		style:--a={colors[0]}
		style:--c={colors[1]}
		aria-hidden="true"
	>
		{#if GLYPHS[glyph]}
			<svg width={size * 0.58} height={size * 0.58} viewBox="0 0 24 24">{@html GLYPHS[glyph]}</svg>
		{:else}
			<b style:font-size="{size * 0.42}px">{initials}</b>
		{/if}
	</span>
{/if}

<style>
	.icon {
		display: inline-grid;
		place-items: center;
		flex: none;
		border-radius: 23%;
		background: linear-gradient(160deg, var(--a), var(--c));
		color: #fff;
		box-shadow:
			0 6px 16px rgb(0 0 0 / 0.28),
			inset 0 1px 0 rgb(255 255 255 / 0.35);
	}
	.artwork {
		position: relative;
		background-size: cover;
		background-position: center;
		box-shadow:
			inset 0 1px rgb(255 255 255 / 0.17),
			0 2px 6px rgb(0 0 0 / 0.18);
	}
	.date {
		position: absolute;
		inset: 39% 8% 13%;
		display: grid;
		place-items: center;
		line-height: 1;
		color: #141517;
		font-family: system-ui, sans-serif;
		font-weight: 750;
		font-variant-numeric: tabular-nums;
	}
	.icon.light {
		color: #2b3440;
	}
	.icon.app {
		background: linear-gradient(160deg, #fff, #e9eef5);
	}
	.icon.app img {
		width: 68%;
		height: 68%;
		object-fit: contain;
	}
	b {
		font-weight: 700;
		line-height: 1;
	}
</style>
