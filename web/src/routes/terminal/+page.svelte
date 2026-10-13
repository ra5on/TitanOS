<script lang="ts">
	// The terminal in a browser tab of its own: the panel fills the page.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { terminal } from '#lib/terminal.svelte.ts';

	$effect(() => {
		terminal.full = true;
		terminal.open = true;
		return () => {
			terminal.full = false;
		};
	});
</script>

<svelte:head><title>{t('terminal.title')} · TitanOS</title></svelte:head>
