<script lang="ts">
	// PLAN 75-20 (owner 9.11.2026): Einstellungen › Programme is gone – who
	// may use which app is set per account under Benutzer (draft U2) and per
	// app on its page in Docker. Old links land on the accounts.
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	onMount(() => void goto('/users', { replaceState: true }));
</script>
