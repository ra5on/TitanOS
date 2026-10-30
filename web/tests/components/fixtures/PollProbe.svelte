<script lang="ts">
	// The pattern that looped on the VM page: the load reads a flag that it
	// also sets, and is started from an effect.
	import { poll } from '#lib/poll.ts';

	let { onload }: { onload: () => void } = $props();
	let refreshing = $state(false);
	let count = $state(0);

	async function load() {
		if (refreshing) return;
		refreshing = true;
		onload();
		await Promise.resolve();
		count++;
		refreshing = false;
	}
	$effect(() => poll(load, { interval: 1000 }));
</script>

<p>{count}</p>
