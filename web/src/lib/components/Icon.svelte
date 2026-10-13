<script lang="ts">
	// Line icons drawn for TitanOS on a 24 px grid; they take the text color.
	const PATHS = {
		chevronDown: 'm6 9 6 6 6-6',
		palette: 'M12 3a9 9 0 1 0 0 18c1.1 0 1.8-.9 1.8-1.9 0-.5-.2-.9-.5-1.3-.3-.3-.4-.7-.4-1.1 0-1 .8-1.8 1.8-1.8H17a4 4 0 0 0 4-4c0-4.4-4-7.9-9-7.9ZM7.5 12h.01M9.5 8h.01M14.5 8h.01',
		restart: 'M20 11a8 8 0 1 0-2.3 6.3M20 4v7h-7',
		power: 'M12 3v9M6.3 7.3a8 8 0 1 0 11.4 0',
		logout: 'M9 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h3M16 16l4-4-4-4M20 12H9',
		globe: 'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18ZM3 12h18M12 3c2.5 2.6 3.8 5.6 3.8 9s-1.3 6.4-3.8 9c-2.5-2.6-3.8-5.6-3.8-9S9.500 5.600 12 3Z',
		grip: 'M9 6h.01M15 6h.01M9 12h.01M15 12h.01M9 18h.01M15 18h.01',
		close: 'M6 6l12 12M18 6 6 18',
		plus: 'M12 5v14M5 12h14',
		edit: 'M4 20h4L19 9l-4-4L4 16v4ZM13.5 6.5l4 4',
		resize: 'M20 12v8h-8M20 20 10 10',
		check: 'm5 12 5 5 9-10',
		key: 'M15 7a4 4 0 1 1-3.9 4.9L4 19v-3l2-1v-2l2.200-.9A4 4 0 0 1 15 7ZM16 10h.01',
		copy: 'M9 9h10v10H9zM5 15V5h10'
	} as const;

	type Props = { name: keyof typeof PATHS; size?: number };
	let { name, size = 18 }: Props = $props();
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width={name === 'grip' ? 2.6 : 1.8} stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
	<path d={PATHS[name]} />
</svg>
