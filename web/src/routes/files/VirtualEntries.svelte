<script lang="ts" generics="T">
	import { onMount } from 'svelte';
	import type { Snippet } from 'svelte';
	let {
		items,
		children,
		key,
		minWidth = 0,
		horizontal = false,
		rowHeight = 64,
		label,
		reset = '',
	}: {
		items: T[];
		children: Snippet<[T]>;
		key: (item: T) => string;
		minWidth?: number;
		horizontal?: boolean;
		rowHeight?: number;
		label: string;
		reset?: string;
	} = $props();
	let viewport: HTMLDivElement;
	export function getColumns() {
		return columns;
	}
	export function reveal(index: number) {
		if (!viewport || (!virtual && !horizontal)) return;
		if (horizontal) {
			const x = index * (minWidth + 8);
			if (x < viewport.scrollLeft) viewport.scrollLeft = x;
			else if (x + minWidth > viewport.scrollLeft + viewport.clientWidth)
				viewport.scrollLeft = x + minWidth - viewport.clientWidth;
			left = viewport.scrollLeft;
			return;
		}
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
		left = $state(0),
		top = $state(0);
	const virtual = $derived(items.length > 200);
	const columns = $derived(
		!horizontal && minWidth ? Math.max(1, Math.floor((width + 8) / (minWidth + 8))) : 1,
	);
	const verticalVirtual = $derived(virtual && !horizontal);
	const start = $derived.by(() => {
		if (!virtual) return 0;
		if (horizontal) return Math.max(0, Math.floor(left / (minWidth + 8)) - 3);
		return Math.max(0, Math.floor(top / rowHeight) - 3) * columns;
	});
	const end = $derived.by(() => {
		if (!virtual) return items.length;
		const last = horizontal
			? Math.ceil((left + width) / (minWidth + 8)) + 3
			: (Math.ceil((top + height) / rowHeight) + 3) * columns;
		return Math.min(items.length, last);
	});
	const total = $derived(Math.ceil(items.length / columns) * rowHeight);
	$effect(() => {
		reset;
		if (viewport) {
			viewport.scrollTop = 0;
			if (horizontal) viewport.scrollLeft = left = 0;
			top = 0;
		}
	});
	$effect(() => {
		if (verticalVirtual && top > total - height && viewport) {
			viewport.scrollTop = Math.max(0, total - height);
			top = viewport.scrollTop;
		}
	});
	onMount(() => {
		// Changing the number of grid columns changes its height. Apply the
		// measurement on the next frame, outside WebKit's ResizeObserver delivery.
		let frame = 0;
		const observer = new ResizeObserver(() => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => {
				width = viewport.clientWidth;
				height = viewport.clientHeight;
			});
		});
		observer.observe(viewport);
		return () => {
			cancelAnimationFrame(frame);
			observer.disconnect();
		};
	});
</script>

<!-- Scroll regions must be keyboard reachable for PageUp/PageDown and arrow scrolling. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
	bind:this={viewport}
	class="viewport"
	class:virtual
	class:horizontal
	tabindex={virtual ? 0 : undefined}
	role="region"
	aria-label={label}
	onscroll={() => {
		top = viewport.scrollTop;
		if (horizontal) left = viewport.scrollLeft;
	}}
>
	<div
		class="items"
		style:width={horizontal ? `${items.length * (minWidth + 8) - 8}px` : undefined}
		style:height={verticalVirtual ? `${total}px` : undefined}
		style:grid-template-columns={horizontal ? undefined : `repeat(${columns},minmax(0,1fr))`}
		style:grid-auto-columns={horizontal ? `${minWidth}px` : undefined}
		style:gap={minWidth ? '8px' : '0'}
	>
		{#each items.slice(start, end) as item, index (key(item))}
			<div
				class="cell"
				class:positioned={verticalVirtual}
				style:top={verticalVirtual
					? `${Math.floor((start + index) / columns) * rowHeight}px`
					: undefined}
				style:left={verticalVirtual
					? `calc(${(start + index) % columns} * (100% / ${columns}))`
					: undefined}
				style:width={verticalVirtual ? `calc(100% / ${columns})` : undefined}
				style:height={verticalVirtual ? `${rowHeight}px` : undefined}
				style:grid-column={horizontal && virtual ? start + index + 1 : undefined}
				style:padding={verticalVirtual && minWidth ? '4px' : undefined}
			>
				{@render children(item)}
			</div>
		{/each}
	</div>
</div>

<style>
	.horizontal,
	.horizontal.virtual {
		height: auto;
		overflow-x: auto;
		overscroll-behavior-x: contain;
	}
	.horizontal .items {
		grid-auto-flow: column;
	}
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
