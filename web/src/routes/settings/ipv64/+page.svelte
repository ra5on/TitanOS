<script lang="ts">
	// Settings › IPv64 is the tab "IPv64" of Settings › Dynamic DNS since
	// 29 Oct 2026; old links (also with ?tab=) land on the same sub-tab there.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ipv64Href } from '#lib/ipv64/ipv64.ts';
	onMount(() => {
		void goto(ipv64Href(page.url.searchParams.get('tab')) + page.url.hash, { replaceState: true });
	});
</script>
