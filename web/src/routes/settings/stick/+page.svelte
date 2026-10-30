<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	onMount(() => {
		void goto(
			location.hash === '#stick-replace'
				? '/settings/backups/restore#stick-replace'
				: '/settings/backups/configuration',
			{ replaceState: true },
		);
	});
</script>
