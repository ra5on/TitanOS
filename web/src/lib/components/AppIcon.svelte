<script lang="ts">
	import { publicURL } from '#lib/app-links.ts';
	let { src = '', name }: { src?: string; name: string } = $props();
	let failed = $state('');
	const url = $derived(src.startsWith('https://') ? publicURL(src) : '');
	const initials = $derived(name.replace(/[^a-zA-Z0-9]/g, '').slice(0, 2) || '?');
</script>

{#if url && failed !== url}<img
		src={url}
		alt=""
		loading="lazy"
		referrerpolicy="no-referrer"
		onerror={() => (failed = url)}
	/>{:else}<span aria-hidden="true">{initials}</span>{/if}

<style>
	img {
		width: 100%;
		height: 100%;
		object-fit: contain;
		padding: 3px;
	}
	span {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 100%;
		height: 100%;
	}
</style>
