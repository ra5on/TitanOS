<script lang="ts">
	// Settings › IPv64 is gone (0.34.0); IPv64.net stays a provider of dynamic DNS.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	onMount(() => { void goto('/settings/ddns', { replaceState: true }); });
</script>
