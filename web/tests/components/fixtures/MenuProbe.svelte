<script lang="ts">
	// PLAN 67-1: a page that registers menu commands (tests/components/menu-commands.test.ts).
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	let { onmkdir }: { onmkdir: () => void } = $props();
	let hidden = $state(false);
	let writable = $state(false);
	registerMenuCommands({
		'files.mkdir': { run: () => onmkdir(), enabled: () => writable },
		'files.hidden': { run: () => (hidden = !hidden), checked: () => hidden },
	});
</script>

<button type="button" onclick={() => (writable = true)}>writable</button>
