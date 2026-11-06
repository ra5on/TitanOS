<script lang="ts">
	// The terminal in a browser tab of its own: the panel fills the page.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { page } from '$app/state';
	import { openRequested, terminal } from '#lib/terminal.svelte.ts';

	// A console asked for by another window of the desktop (PLAN 62-5).
	$effect(() => {
		const raw = page.url.searchParams.get('open');
		if (!raw) return;
		try {
			openRequested(JSON.parse(raw));
		} catch {
			// Not a request this page understands.
		}
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
