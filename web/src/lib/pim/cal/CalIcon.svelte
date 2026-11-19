<script lang="ts">
	// 1.15 (drafts 36–39): the two signs of Kalender's floating buttons that
	// the shared icons do not have – the views (two stacked bars) and the
	// reminders (a list with ticks). Drawn like Icon.svelte (stroke in the
	// text's color).
	const PATHS = {
		views:
			'M5 5h14a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2ZM5 13h14a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-2a2 2 0 0 1 2-2Z',
		todo: 'M9 6h11M9 12h11M9 18h11M4 6l1 1 2-2M4 12l1 1 2-2M4 18l1 1 2-2',
		month: 'M4 6h16v14H4ZM4 10h16M8 3v4M16 3v4M8 14h.01M12 14h.01M16 14h.01M8 17h.01M12 17h.01',
	};
	let { name, size = 22 }: { name: keyof typeof PATHS; size?: number } = $props();
</script>

<svg
	viewBox="0 0 24 24"
	width={size}
	height={size}
	fill="none"
	stroke="currentColor"
	stroke-width="1.8"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"><path d={PATHS[name]} /></svg
>
