<script lang="ts">
	// The head of a page: where it sits (optional), the title, a line or two
	// below it and the page's buttons on the right.
	import type { Snippet } from 'svelte';
	import { pageTitle } from '#lib/page-title.svelte.ts';

	type Props = {
		title: string;
		parent?: { href: string; label: string };
		// What the way back calls this page, if not its title.
		crumb?: string;
		id?: string;
		children?: Snippet;
		actions?: Snippet;
	};
	let { title, parent, crumb, id, children, actions }: Props = $props();

	// The browser tab shows the title of the page; the last head that leaves clears it.
	$effect(() => {
		const shown = title;
		pageTitle.value = shown;
		return () => {
			if (pageTitle.value === shown) pageTitle.value = '';
		};
	});
</script>

<div class="page-head">
	<div class="title">
		{#if parent}<p class="muted crumbs"><a href={parent.href}>{parent.label}</a> › {crumb ?? title}</p>{/if}
		<h1 {id}>{title}</h1>
		{@render children?.()}
	</div>
	{#if actions}<div class="actions">{@render actions()}</div>{/if}
</div>

<style>
	.page-head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
		font-size: 0.875rem;
	}
	.title :global(p) {
		margin: 0;
		max-width: 60rem;
	}
	.crumbs {
		font-size: 0.8125rem;
	}
	h1 {
		font-size: var(--text-page);
		font-weight: 600;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
</style>
