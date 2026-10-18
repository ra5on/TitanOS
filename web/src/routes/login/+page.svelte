<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';

	// The root layout renders this page only after authentication. Replace the
	// login URL so Back cannot return to the former placeholder after signing in.
	onMount(() => {
		void goto('/', { replaceState: true });
	});
</script>
