<script lang="ts">
	// "Nothing here yet": as a tile of its own where a page has nothing to
	// show, or as a muted line inside a list (`inline`).
	import type { Snippet } from 'svelte';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { effectOn } from '#lib/theme/themes.ts';

	type Props = { title: string; text?: string | string[]; inline?: boolean; children?: Snippet };
	let { title, text, inline = false, children }: Props = $props();

	const lines = $derived(text === undefined ? [] : Array.isArray(text) ? text : [text]);
</script>

{#if inline}
	<p class="muted inline" role="status">{title}</p>
{:else}
	<div class="tile empty">
		{#if effectOn(appearance.current, 'illustrations')}
			<!-- An empty hexagon box (the logo's frame), drawn with the theme colors. -->
			<svg class="art" viewBox="0 0 120 84" width="120" height="84" aria-hidden="true">
				<ellipse cx="60" cy="76" rx="40" ry="5" fill="var(--track)" />
				<path d="M60 8 92 26v34L60 78 28 60V26Z" fill="color-mix(in srgb, var(--accent) 10%, transparent)" stroke="var(--border)" stroke-width="2" stroke-linejoin="round" />
				<path d="M28 26l32 18 32-18M60 44v34" fill="none" stroke="var(--border)" stroke-width="2" stroke-linejoin="round" />
				<path d="M60 8 92 26 60 44 28 26Z" fill="color-mix(in srgb, var(--accent) 22%, transparent)" stroke="var(--accent)" stroke-width="2" stroke-linejoin="round" />
				<circle cx="98" cy="12" r="3" fill="var(--accent)" opacity="0.7" />
				<circle cx="18" cy="20" r="2" fill="var(--muted)" opacity="0.6" />
				<circle cx="104" cy="42" r="1.6" fill="var(--muted)" opacity="0.5" />
			</svg>
		{/if}
		<p class="title">{title}</p>
		{#each lines as line, index (index)}<p class="muted">{line}</p>{/each}
		{@render children?.()}
	</div>
{/if}

<style>
	.empty {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 4px;
		width: 100%;
		padding: 28px 22px;
		font-size: 0.875rem;
	}
	.empty p {
		margin: 0;
		max-width: 52rem;
	}
	.title {
		font-weight: 500;
	}
	.art {
		margin-bottom: 8px;
	}
	.inline {
		margin: 0;
		padding: 28px 14px;
		font-size: 0.875rem;
	}
</style>
