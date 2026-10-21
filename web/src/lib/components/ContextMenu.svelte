<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onMount, tick } from 'svelte';
	import { focusFirstItem, moveFocus } from './menu.ts';
	import './menu.css';

	type Props = {
		label: string;
		/** Where the menu opens: the mouse position or the corner of the "⋯" button. */
		x: number;
		y: number;
		onclose: () => void;
		children: Snippet;
	};
	let { label, x, y, onclose, children }: Props = $props();

	let menu: HTMLElement | undefined = $state();
	let left = $state(0), top = $state(0);
	// Whatever had the focus before gets it back, so the keyboard stays in the list.
	const before = document.activeElement as HTMLElement | null;

	onMount(() => {
		void place();
		return () => before?.isConnected && before.focus({ preventScroll: true });
	});
	async function place() {
		left = x;
		top = y;
		await tick();
		if (!menu) return;
		// Keep the whole menu on screen.
		const rect = menu.getBoundingClientRect();
		left = Math.max(8, Math.min(x, window.innerWidth - rect.width - 8));
		top = y + rect.height > window.innerHeight - 8 ? Math.max(8, y - rect.height) : y;
		focusFirstItem(menu);
	}
	function keys(event: KeyboardEvent) {
		if (event.key === 'Escape' || event.key === 'Tab') {
			event.preventDefault();
			event.stopPropagation();
			onclose();
			return;
		}
		moveFocus(menu, event);
	}
</script>

<svelte:window
	onpointerdown={(event) => !menu?.contains(event.target as Node) && onclose()}
	onresize={onclose}
/>

<div
	class="dropdown-panel context-menu"
	bind:this={menu}
	role="menu"
	aria-label={label}
	tabindex="-1"
	style:left="{left}px"
	style:top="{top}px"
	onkeydown={keys}
	oncontextmenu={(event) => event.preventDefault()}
>
	{@render children()}
</div>

<style>
	.context-menu {
		position: fixed;
		z-index: 60;
		min-width: 14rem;
		max-width: min(20rem, calc(100vw - 16px));
		max-height: calc(100vh - 16px);
		overflow: auto;
		display: flex;
		flex-direction: column;
		padding: 6px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
	}
</style>
