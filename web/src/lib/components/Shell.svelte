<script lang="ts">
	// The frame around every page once someone is logged in.
	import type { Snippet } from 'svelte';
	import { session } from '#lib/session.svelte.ts';
	import { shell } from '#lib/shell.svelte.ts';
	import { watchStorage } from '#lib/storage.svelte.ts';
	import { watchSystem } from '#lib/system.svelte.ts';
	import { terminal, toggleTerminal } from '#lib/terminal.svelte.ts';
	import Header from './Header.svelte';
	import StatusBar from './StatusBar.svelte';
	import TerminalPanel from './TerminalPanel.svelte';

	let { children }: { children: Snippet } = $props();

	// A shell is a way into the system: administrators only.
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));

	function hotkey(event: KeyboardEvent) {
		// Ctrl+` wherever the key is; layouts differ in what it types.
		if (admin && event.ctrlKey && !event.altKey && !event.metaKey && (event.key === '`' || event.code === 'Backquote')) {
			event.preventDefault();
			toggleTerminal();
		}
	}

	$effect(() => watchSystem());
	$effect(() => watchStorage());
</script>

<svelte:window onkeydown={hotkey} />

<!-- The page ends above the open terminal, so that all of it can be scrolled into view. -->
<div class="shell" style:padding-bottom={admin && terminal.open && !terminal.full ? `${terminal.height}px` : undefined}>
	<Header />
	<main>{@render children()}</main>
	<StatusBar note={shell.statusNote} />
</div>
{#if admin}<TerminalPanel />{/if}

<style>
	.shell {
		position: relative;
		z-index: 1;
		min-height: 100vh;
		display: flex;
		flex-direction: column;
	}
	main {
		flex: 1 1 auto;
		width: 100%;
		max-width: 1440px;
		margin: 0 auto;
		padding: 28px 32px 36px;
		display: flex;
		flex-direction: column;
		gap: 20px;
	}
	@media (max-width: 760px) {
		main {
			padding: 20px 16px 28px;
		}
	}
</style>
