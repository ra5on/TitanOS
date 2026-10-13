<script lang="ts">
	// The frame around every page once someone is logged in.
	import type { Snippet } from 'svelte';
	import { shell } from '#lib/shell.svelte.ts';
	import { watchStorage } from '#lib/storage.svelte.ts';
	import { watchSystem } from '#lib/system.svelte.ts';
	import Header from './Header.svelte';
	import StatusBar from './StatusBar.svelte';

	let { children }: { children: Snippet } = $props();

	$effect(() => watchSystem());
	$effect(() => watchStorage());
</script>

<div class="shell">
	<Header />
	<main>{@render children()}</main>
	<StatusBar note={shell.statusNote} />
</div>

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
