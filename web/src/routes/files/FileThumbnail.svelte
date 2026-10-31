<script lang="ts">
	import FileIcon from './FileIcon.svelte';
	import { thumbnailKind, thumbnailURL } from '#lib/file-actions.ts';
	let {
		root,
		path,
		modified,
		box = 320,
		height = 96,
	}: { root: string; path: string; modified?: string; box?: number; height?: number } = $props();
	let host = $state<HTMLSpanElement>();
	let visible = $state(false);
	let failed = $state(false);
	const name = $derived(path.split('/').pop() ?? path);
	const kind = $derived(thumbnailKind(name));
	// titand makes the thumbnail and caches it on the volume; the browser asks
	// only once the entry scrolls into view.
	$effect(() => {
		if (!host || !kind) return;
		failed = false;
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((entry) => entry.isIntersecting)) {
					observer.disconnect();
					visible = true;
				}
			},
			{ rootMargin: '200px' },
		);
		observer.observe(host);
		return () => observer.disconnect();
	});
</script>

<span class="thumbnail" bind:this={host} aria-hidden="true" style:height="{height}px">
	{#if kind && visible && !failed}<img
			src={thumbnailURL(root, path, box, modified)}
			alt=""
			decoding="async"
			style:max-height="{height}px"
			onerror={() => {
				failed = true;
			}}
		/>{#if kind === 'video'}<span class="play">▶</span>{/if}{:else}<FileIcon
			{name}
			dir={false}
			size={48}
		/>{/if}
</span>

<style>
	.thumbnail {
		position: relative;
		display: flex;
		align-items: center;
		justify-content: center;
		width: 100%;
	}
	img {
		max-width: 100%;
		object-fit: contain;
		border-radius: var(--radius-small);
	}
	.play {
		position: absolute;
		inset: auto auto 6px 6px;
		padding: 2px 6px;
		border-radius: var(--radius-small);
		background: var(--surface);
		color: var(--text);
		font-size: var(--text-sm);
	}
</style>
