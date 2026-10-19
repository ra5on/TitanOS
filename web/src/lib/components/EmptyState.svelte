<script lang="ts">
	// "Nothing here yet": as a tile of its own where a page has nothing to
	// show, or as a muted line inside a list (`inline`).
	import type { Snippet } from 'svelte';

	type Props = { title: string; text?: string | string[]; inline?: boolean; children?: Snippet };
	let { title, text, inline = false, children }: Props = $props();

	const lines = $derived(text === undefined ? [] : Array.isArray(text) ? text : [text]);
</script>

{#if inline}
	<p class="muted inline" role="status">{title}</p>
{:else}
	<div class="tile empty">
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
	.inline {
		margin: 0;
		padding: 28px 14px;
		font-size: 0.875rem;
	}
</style>
