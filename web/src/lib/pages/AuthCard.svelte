<script lang="ts">
	// Frame of the pages shown before anyone is logged in.
	import type { Snippet } from 'svelte';
	import Logo from '#lib/components/Logo.svelte';
	import LanguageSwitch from '#lib/components/LanguageSwitch.svelte';

	type Props = { title: string; hostname?: string; children: Snippet };
	let { title, hostname = '', children }: Props = $props();
</script>

<main>
	<section class="tile card">
		<Logo size={52} stacked />
		<div class="head">
			<h1>{title}</h1>
			{#if hostname}<span class="mono host">{hostname}</span>{/if}
		</div>
		{@render children()}
	</section>
	<LanguageSwitch />
</main>

<style>
	main {
		position: relative;
		z-index: 1;
		min-height: 100vh;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 20px;
		padding: 24px;
	}
	.card {
		width: 100%;
		max-width: 440px;
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: 22px;
		padding: 36px 32px 32px;
	}
	.card :global(.logo) {
		align-self: center;
	}
	.head {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		text-align: center;
	}
	h1 {
		font-size: 1.375rem;
		font-weight: 600;
	}
	.host {
		padding: 3px 8px;
		border: 1px solid var(--border);
		border-radius: 6px;
		font-size: 0.75rem;
		color: var(--muted);
	}
</style>
