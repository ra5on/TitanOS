<script lang="ts" generics="T">
	import { onMount } from 'svelte';
	import type { Snippet } from 'svelte';
	let {
		items,
		children,
		key,
		minWidth = 0,
		rowHeight = 64,
		label,
		reset = '',
	}: {
		items: T[];
		children: Snippet<[T]>;
		key: (item: T) => string;
		minWidth?: number;
		rowHeight?: number;
		label: string;
		reset?: string;
	} = $props();
	let viewport: HTMLDivElement;
	export function getColumns() {
		return columns;
	}
	export function reveal(index: number) {
		if (!viewport || !virtual) return;
		const y = Math.floor(index / columns) * rowHeight;
		if (y < viewport.scrollTop) viewport.scrollTop = y;
		else if (y + rowHeight > viewport.scrollTop + viewport.clientHeight)
			viewport.scrollTop = y + rowHeight - viewport.clientHeight;
		top = viewport.scrollTop;
	}
	export function getScroll() {
		return viewport?.scrollTop ?? 0;
	}
	export function setScroll(value: number) {
		if (viewport) {
			viewport.scrollTop = value;
			top = viewport.scrollTop;
		}
	}
	let width = $state(600),
		height = $state(500),
		top = $state(0);
	const virtual = $derived(items.length > 200);
	const columns = $derived(minWidth ? Math.max(1, Math.floor((width + 8) / (minWidth + 8))) : 1);
	const start = $derived(virtual ? Math.max(0, Math.floor(top / rowHeight) - 3) * columns : 0);
	const end = $derived(
		virtual
			? Math.min(items.length, (Math.ceil((top + height) / rowHeight) + 3) * columns)
			: items.length,
	);
	const total = $derived(Math.ceil(items.length / columns) * rowHeight);
	$effect(() => {
		reset;
		if (viewport) {
			viewport.scrollTop = 0;
			top = 0;
		}
	});
	$effect(() => {
		if (virtual && top > total - height && viewport) {
			viewport.scrollTop = Math.max(0, total - height);
			top = viewport.scrollTop;
		}
	});
	onMount(() => {
		const observer = new ResizeObserver(() => {
			width = viewport.clientWidth;
			height = viewport.clientHeight;
		});
		observer.observe(viewport);
		return () => observer.disconnect();
	});
</script>

<!-- Scroll regions must be keyboard reachable for PageUp/PageDown and arrow scrolling. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
	bind:this={viewport}
	class="viewport"
	class:virtual
	tabindex={virtual ? 0 : undefined}
	role="region"
	aria-label={label}
	onscroll={() => (top = viewport.scrollTop)}
>
	<div
		class="items"
		style:height={virtual ? `${total}px` : undefined}
		style:grid-template-columns={`repeat(${columns},minmax(0,1fr))`}
		style:gap={minWidth ? '8px' : '0'}
	>
		{#each items.slice(start, end) as item, index (key(item))}
			<div
				class="cell"
				class:positioned={virtual}
				style:top={virtual ? `${Math.floor((start + index) / columns) * rowHeight}px` : undefined}
				style:left={virtual
					? `calc(${(start + index) % columns} * (100% / ${columns}))`
					: undefined}
				style:width={virtual ? `calc(100% / ${columns})` : undefined}
				style:height={virtual ? `${rowHeight}px` : undefined}
				style:padding={virtual && minWidth ? '4px' : undefined}
			>
				{@render children(item)}
			</div>
		{/each}
	</div>
</div>

<style>
	.viewport {
		min-width: 0;
		grid-column: 1/-1;
	}
	.virtual {
		height: min(60vh, 640px);
		overflow: auto;
		overflow-anchor: none;
	}
	.items {
		position: relative;
		display: grid;
		min-width: 0;
	}
	.cell {
		min-width: 0;
		box-sizing: border-box;
	}
	.positioned {
		position: absolute;
		overflow: hidden;
	}
	.positioned :global(.row),
	.positioned :global(.entry-row) {
		height: 100%;
		box-sizing: border-box;
	}
	.virtual:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
</style>
