<script lang="ts">
	import Backdrop from '#lib/components/Backdrop.svelte';
	import type { Appearance } from '#lib/theme/themes.ts';
	import { backgroundURL, type DashboardVisual } from './visual.ts';
	let {
		visual,
		inherited,
		urls,
	}: { visual: DashboardVisual; inherited: Appearance; urls: Record<string, string> } = $props();
	const background = $derived(visual.background);
	const url = $derived(backgroundURL(visual, urls));
</script>

{#if background.source === 'inherit'}
	<Backdrop
		appearance={{ ...inherited, dim: visual.dim }}
		dashboard
		fit={background.fit}
		x={background.x}
		y={background.y}
		zoom={background.zoom}
		fixed={background.fixed}
	/>
{:else if url}
	<div class="dashboard-background" class:scrolling={!background.fixed} aria-hidden="true">
		<div
			class="picture"
			style:background-image={`url("${url}")`}
			style:background-size={background.fit}
			style:background-position={`${background.x}% ${background.y}%`}
			style:transform={background.fit === 'cover' ? `scale(${background.zoom / 100})` : undefined}
		></div>
		<div class="dim" style:opacity={visual.dim / 100}></div>
	</div>
{/if}

<style>
	.dashboard-background {
		position: fixed;
		inset: var(--header-height, 60px) 0 var(--status-height, 34px);
		z-index: 0;
		pointer-events: none;
		overflow: hidden;
		background: var(--bg);
	}
	.dashboard-background.scrolling {
		position: absolute;
		bottom: auto;
		height: calc(100svh - var(--header-height, 60px) - var(--status-height, 34px));
	}
	.picture,
	.dim {
		position: absolute;
		inset: 0;
	}
	.picture {
		background-repeat: no-repeat;
		transform-origin: center;
	}
	.dim {
		background: var(--dim);
	}
</style>
