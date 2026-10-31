<script lang="ts">
	// A modal window. Escape and "Cancel" close it unless `locked` (work in progress).
	import type { Snippet } from 'svelte';

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

	$effect(() => {
		dialog.showModal();
	});

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
	style:max-width={maxWidth ? `${Math.max(320, Math.min(1400, maxWidth))}px` : undefined}
	aria-labelledby="{id}-title"
	{onclose}
	oncancel={cancel}
>
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
		gap: 10px;
		font-size: var(--text-md);
	}
	dialog :global(.confirm input) {
		width: 18px;
		height: 18px;
		flex: none;
		margin: 2px 0 0;
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
