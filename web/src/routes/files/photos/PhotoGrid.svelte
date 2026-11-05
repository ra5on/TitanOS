<script lang="ts">
	// A grid of photos (timeline, search, album, place), thumbnails from
	// titand, loaded lazily; long lists grow while scrolling.
	import { thumbnailURL } from '#lib/file-actions.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { photoName, type PhotoItem } from './photos.ts';

	let {
		items,
		captions = false,
		onopen,
	}: {
		items: PhotoItem[];
		/** Show the start of the description under each photo (search). */
		captions?: boolean;
		onopen: (p: PhotoItem) => void;
	} = $props();

	let limit = $state(240);
	let sentinel = $state<HTMLElement>();
	$effect(() => {
		if (!sentinel || limit >= items.length) return;
		const io = new IntersectionObserver((e) => {
			if (e.some((x) => x.isIntersecting)) limit += 240;
		});
		io.observe(sentinel);
		return () => io.disconnect();
	});
	const shown = $derived(items.slice(0, limit));
</script>

<div class="grid">
	{#each shown as p (p.root + ':' + p.path)}
		<button
			type="button"
			class="cell"
			title={p.desc ? p.desc.split('\n')[0] : photoName(p)}
			onclick={() => onopen(p)}
		>
			<img
				src={thumbnailURL(p.root, p.path, 320, p.modified)}
				alt={p.desc ? p.desc.split('\n')[0] : photoName(p)}
				loading="lazy"
				decoding="async"
			/>
			{#if p.kind === 'video'}<span class="badge">▶</span>{/if}
			{#if captions && p.desc}<span class="cap">{p.desc.split('\n')[0]}</span>{/if}
		</button>
	{/each}
</div>
{#if limit < items.length}<div class="more" bind:this={sentinel}>{t('photos.more')}</div>{/if}

<style>
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 6px;
	}
	.cell {
		position: relative;
		aspect-ratio: 4 / 3;
		padding: 0;
		border: 0;
		border-radius: 10px;
		overflow: hidden;
		background: color-mix(in srgb, var(--text) 8%, transparent);
		cursor: pointer;
	}
	.cell img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
		transition: transform 0.2s ease;
	}
	.cell:hover img {
		transform: scale(1.04);
	}
	.cell:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.badge {
		position: absolute;
		top: 6px;
		right: 6px;
		padding: 1px 7px;
		border-radius: 999px;
		background: rgb(0 0 0 / 0.6);
		color: #fff;
		font-size: 11px;
	}
	.cap {
		position: absolute;
		left: 0;
		right: 0;
		bottom: 0;
		padding: 18px 8px 6px;
		font-size: 12px;
		color: #fff;
		text-align: left;
		background: linear-gradient(transparent, rgb(0 0 0 / 0.75));
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	.more {
		padding: var(--space-3);
		text-align: center;
		color: var(--muted);
	}
	@media (max-width: 760px) {
		.grid {
			grid-template-columns: repeat(3, minmax(0, 1fr));
			gap: 3px;
		}
		.cell {
			aspect-ratio: 1;
			border-radius: 4px;
		}
	}
</style>
