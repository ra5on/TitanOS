<script lang="ts">
	// Settings › IPv64 (an area of its own until 0.33, a tab of Settings ›
	// Dynamic DNS until 0.76) is gone (PLAN 79-2); old links land on
	// Settings › Dynamic DNS, where IPv64 stays a provider.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	onMount(() => {
		void goto('/settings/ddns', { replaceState: true });
	});
</script>
