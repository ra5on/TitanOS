<script lang="ts">
	// A modal window. Escape and "Cancel" close it unless `locked` (work in progress).
	import type { Snippet } from 'svelte';

	type Props = { title: string; locked?: boolean; wide?: boolean; onclose: () => void; children: Snippet };
	let { title, locked = false, wide = false, onclose, children }: Props = $props();

	let dialog: HTMLDialogElement;
	const id = $props.id();

	$effect(() => {
		dialog.showModal();
	});

	function cancel(event: Event) {
		if (locked) event.preventDefault();
	}
</script>

<dialog bind:this={dialog} class="tile" class:wide aria-labelledby="{id}-title" {onclose} oncancel={cancel}>
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
		background: var(--surface);
		border-color: var(--border);
		color: var(--text);
		box-shadow: var(--shadow-menu);
	}
	dialog.wide {
		max-width: 760px;
	}
	dialog[open] {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	dialog::backdrop {
		background: var(--overlay);
	}
	h2 {
		font-size: 1.1875rem;
		font-weight: 600;
	}
	dialog :global(.dialog-actions) {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: 8px;
	}
	dialog :global(.confirm) {
		display: flex;
		align-items: flex-start;
		gap: 10px;
		font-size: 0.875rem;
	}
	dialog :global(.confirm input) {
		width: 18px;
		height: 18px;
		flex: none;
		margin: 2px 0 0;
	}
 @media (max-width: 760px) {
 dialog { width: calc(100% - 16px); max-height: calc(100dvh - 16px); overflow-x: hidden; }
 dialog :global(.dialog-actions) { flex-wrap: wrap; }
 dialog :global(.dialog-actions > *) { flex: 1 1 auto; }
 }
</style>
