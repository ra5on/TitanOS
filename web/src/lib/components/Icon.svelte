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
		trash: 'M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13',
		close: 'M6 6l12 12M18 6 6 18',
		plus: 'M12 5v14M5 12h14',
		edit: 'M4 20h4L19 9l-4-4L4 16v4ZM13.5 6.5l4 4',
		resize: 'M20 12v8h-8M20 20 10 10',
		check: 'm5 12 5 5 9-10',
		key: 'M15 7a4 4 0 1 1-3.9 4.9L4 19v-3l2-1v-2l2.200-.9A4 4 0 0 1 15 7ZM16 10h.01',
		copy: 'M9 9h10v10H9zM5 15V5h10',
		folder: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z',
		file: 'M7 3h7l5 5v11a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2ZM14 3v5h5',
		upload: 'M12 16V4M7 9l5-5 5 5M4 20h16',
		download: 'M12 4v12M7 11l5 5 5-5M4 20h16',
		pause: 'M9 5v14M15 5v14',
		play: 'M7 5v14l11-7Z',
		stop: 'M6 6h12v12H6Z',
		bell: 'M6 9a6 6 0 0 1 12 0c0 6 2.5 7 2.5 8h-17C3.5 16 6 15 6 9ZM10 20a2 2 0 0 0 4 0',
		disk: 'M4 6a8 3 0 0 0 16 0A8 3 0 0 0 4 6v12a8 3 0 0 0 16 0V6M4 12a8 3 0 0 0 16 0'
	} as const;

	type Props = { name: keyof typeof PATHS; size?: number };
	let { name, size = 18 }: Props = $props();
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width={name === 'grip' ? 2.6 : 1.8} stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
	<path d={PATHS[name]} />
</svg>
