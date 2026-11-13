<script lang="ts">
	// A modal window. Escape and "Cancel" close it unless `locked` (work in progress).
	// PLAN 87: on the phone it comes up from the bottom as a sheet with large
	// targets; a swipe down closes it like Escape.
	import type { Snippet } from 'svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { swipeDown } from './sheet.ts';

	type Props = {
		title: string;
		locked?: boolean;
		wide?: boolean;
		maxWidth?: number;
		/** "menu" for pop-ups that act as a menu (command palette): menu glass values. */
		surface?: 'dialog' | 'menu';
		onrequestclose?: () => void;
		onclose: () => void;
		children: Snippet;
	};
	let {
		title,
		locked = false,
		wide = false,
		maxWidth,
		surface = 'dialog',
		onrequestclose,
		onclose,
		children,
	}: Props = $props();

	let dialog: HTMLDialogElement;
	const id = $props.id();
	const sheet = phoneView();

	/** The swipe down asks like Escape: a locked dialog stays, a page with its own question is asked. */
	function swipedAway() {
		if (locked) return;
		if (onrequestclose) onrequestclose();
		else dialog.close();
	}

	$effect(() => {
		dialog.showModal();
	});

	// Escape is answered here: browsers close a dialog at the second Escape in
	// a row even when "cancel" was refused – a locked dialog (work in
	// progress) must stay.
	function keydown(event: KeyboardEvent) {
		if (event.key !== 'Escape' || event.defaultPrevented) return;
		if (locked) event.preventDefault();
		else if (onrequestclose) {
			event.preventDefault();
			onrequestclose();
		}
	}
	function cancel(event: Event) {
		if (locked) event.preventDefault();
		else if (onrequestclose) {
			event.preventDefault();
			onrequestclose();
		}
	}
</script>

<dialog
	bind:this={dialog}
	class="tile"
	class:wide
	class:as-menu={surface === 'menu'}
	class:sheet
	style:max-width={maxWidth ? `${Math.max(320, Math.min(1400, maxWidth))}px` : undefined}
	aria-labelledby="{id}-title"
	{onclose}
	oncancel={cancel}
	onkeydown={keydown}
	use:swipeDown={{ close: swipedAway, enabled: () => sheet && !locked }}
>
	{#if sheet}<span class="grip" aria-hidden="true"></span>{/if}
	<h2 id="{id}-title">{title}</h2>
	{@render children()}
</dialog>

<style>
	dialog {
		width: calc(100% - 32px);
		max-width: 520px;
		max-height: calc(100dvh - 32px);
		padding: var(--tile-pad);
		overflow-y: auto;
		scrollbar-width: thin;
		scrollbar-color: var(--muted) transparent;
		/* Surface per Settings › Appearance › Menus and windows (app.css). */
		background: var(--dialog-bg);
		border-color: var(--dialog-border);
		-webkit-backdrop-filter: var(--dialog-backdrop);
		backdrop-filter: var(--dialog-backdrop);
		color: var(--text);
		box-shadow: var(--dialog-shadow);
	}
	dialog.as-menu {
		background: var(--menu-bg);
		border-color: var(--menu-border);
		-webkit-backdrop-filter: var(--menu-backdrop);
		backdrop-filter: var(--menu-backdrop);
		box-shadow: var(--menu-shadow);
	}
	dialog.wide {
		max-width: 760px;
	}
	dialog[open] {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	dialog::backdrop {
		background: var(--overlay);
	}
	h2 {
		font-size: var(--text-lg);
		font-weight: 600;
	}
	dialog :global(.dialog-actions) {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2);
	}
	/* The dangerous action stands left of "Cancel". Only the picture moves:
	   in the markup "Cancel" stays first, so it gets the focus and Tab
	   reaches it before the action. */
	dialog :global(.dialog-actions > :is(.btn-danger, .btn-warn)) {
		order: -1;
	}
	dialog :global(.confirm) {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2-5);
		font-size: var(--text-md);
	}
	dialog :global(.confirm input) {
		width: 18px;
		height: 18px;
		flex: none;
		margin: 2px 0 0;
	}
	/* PLAN 87: the phone's sheet – at the bottom, across the width, large targets. */
	dialog.sheet {
		width: 100%;
		max-width: 640px !important;
		max-height: 90dvh;
		margin: auto auto 0;
		padding: var(--space-2) var(--tile-pad) calc(var(--tile-pad) + env(safe-area-inset-bottom));
		border-radius: 20px 20px 0 0;
		border-bottom: 0;
		overscroll-behavior: contain;
	}
	/* Comes up from the bottom with the effect "Knopfdruck und Menüs" (micro), never with reduced motion. */
	dialog.sheet[open] {
		animation: none;
	}
	:global(:root[data-fx-micro='on']) dialog.sheet[open] {
		animation: sheet-up 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
	}
	@keyframes sheet-up {
		from {
			transform: translateY(100%);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		:global(:root[data-fx-micro='on']) dialog.sheet[open] {
			animation: none;
		}
	}
	.grip {
		align-self: center;
		flex: none;
		width: 40px;
		height: 5px;
		margin-bottom: calc(-1 * var(--space-2));
		border-radius: 3px;
		background: color-mix(in srgb, var(--text) 35%, transparent);
	}
	dialog.sheet :global(.dialog-actions > *) {
		flex: 1 1 auto;
		min-height: 48px;
	}
	@media (max-width: 760px) {
		dialog {
			width: calc(100% - 16px);
			max-height: calc(100dvh - 16px);
			overflow-x: hidden;
		}
		dialog :global(.dialog-actions) {
			flex-wrap: wrap;
		}
		dialog :global(.dialog-actions > *) {
			flex: 1 1 auto;
		}
	}
</style>
