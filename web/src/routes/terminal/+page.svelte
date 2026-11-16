<script lang="ts">
	// The terminal in a browser tab of its own: the panel fills the page.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { openRequested, terminal } from '#lib/terminal.svelte.ts';

	// A console asked for by another window of the desktop (PLAN 62-5).
	// Only the address counts: opening reads the sessions, and without
	// untrack every later change to them (closing that tab, a new shell)
	// would answer the old request again (Durchsicht 102).
	$effect(() => {
		const raw = page.url.searchParams.get('open');
		if (!raw) return;
		untrack(() => {
			try {
				openRequested(JSON.parse(raw));
			} catch {
				// Not a request this page understands.
			}
		});
	});

	$effect(() => {
		terminal.full = true;
		terminal.open = true;
		return () => {
			terminal.full = false;
		};
	});
</script>

<svelte:head><title>{t('terminal.title')} · TitanOS</title></svelte:head>
