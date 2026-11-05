<script lang="ts">
	// The photos with GPS on a map (PLAN 52): tiles of OpenStreetMap, loaded
	// by the browser only when the map is switched on; photos close to each
	// other form one marker with their number. Drag to move, buttons or
	// wheel to zoom; a marker opens its photos.
	import Icon from '#lib/components/Icon.svelte';
	import { thumbnailURL } from '#lib/file-actions.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { fitZoom, project, unproject, type PhotoItem } from './photos.ts';

	let { items, onopen }: { items: PhotoItem[]; onopen: (group: PhotoItem[]) => void } = $props();

	const points = $derived(
		items.filter((p) => p.gps && p.lat !== undefined && p.lon !== undefined) as (PhotoItem & {
			lat: number;
			lon: number;
		})[],
	);
	let w = $state(800);
	let h = $state(520);
	let zoom = $state(0);
	let center = $state({ lat: 51, lon: 10 });
	let placed = false;
	$effect(() => {
		if (placed || !points.length || !w) return;
		placed = true;
		const lats = points.map((p) => p.lat);
		const lons = points.map((p) => p.lon);
		center = {
			lat: (Math.min(...lats) + Math.max(...lats)) / 2,
			lon: (Math.min(...lons) + Math.max(...lons)) / 2,
		};
		zoom = fitZoom(points, w, h);
	});
	const c = $derived(project(center.lat, center.lon, zoom));
	const tiles = $derived.by(() => {
		const n = 2 ** zoom;
		const out: { key: string; url: string; left: number; top: number }[] = [];
		for (let x = Math.floor((c.x - w / 2) / 256); x <= Math.floor((c.x + w / 2) / 256); x++)
			for (let y = Math.floor((c.y - h / 2) / 256); y <= Math.floor((c.y + h / 2) / 256); y++) {
				if (y < 0 || y >= n) continue;
				const tx = ((x % n) + n) % n;
				out.push({
					key: `${zoom}/${x}/${y}`,
					url: `https://tile.openstreetmap.org/${zoom}/${tx}/${y}.png`,
					left: x * 256 - (c.x - w / 2),
					top: y * 256 - (c.y - h / 2),
				});
			}
		return out;
	});
	const markers = $derived.by(() => {
		const groups = new Map<string, { x: number; y: number; items: PhotoItem[] }>();
		for (const p of points) {
			const q = project(p.lat, p.lon, zoom);
			const key = `${Math.floor(q.x / 72)}:${Math.floor(q.y / 72)}`;
			const g = groups.get(key) ?? { x: 0, y: 0, items: [] };
			g.items.push(p);
			g.x += (q.x - g.x) / g.items.length;
			g.y += (q.y - g.y) / g.items.length;
			groups.set(key, g);
		}
		return [...groups.entries()].map(([key, g]) => ({
			key,
			left: g.x - (c.x - w / 2),
			top: g.y - (c.y - h / 2),
			items: g.items,
		}));
	});
	let drag: { x: number; y: number; cx: number; cy: number } | null = null;
	function down(e: PointerEvent) {
		if ((e.target as HTMLElement).closest('button')) return;
		drag = { x: e.clientX, y: e.clientY, cx: c.x, cy: c.y };
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!drag) return;
		center = unproject(drag.cx - (e.clientX - drag.x), drag.cy - (e.clientY - drag.y), zoom);
	}
	function setZoom(z: number) {
		zoom = Math.max(2, Math.min(18, z));
	}
	function wheel(e: WheelEvent) {
		e.preventDefault();
		setZoom(zoom + (e.deltaY < 0 ? 1 : -1));
	}
</script>

<div
	class="map"
	bind:clientWidth={w}
	bind:clientHeight={h}
	role="application"
	aria-label={t('photos.mapTitle')}
	onpointerdown={down}
	onpointermove={move}
	onpointerup={() => (drag = null)}
	onwheel={wheel}
>
	{#each tiles as tile (tile.key)}
		<img
			class="tile"
			src={tile.url}
			alt=""
			draggable="false"
			style:left="{tile.left}px"
			style:top="{tile.top}px"
		/>
	{/each}
	{#each markers as m (m.key)}
		<button
			type="button"
			class="marker"
			style:left="{m.left}px"
			style:top="{m.top}px"
			aria-label={t('photos.mapMarker', { count: m.items.length })}
			onclick={() => onopen(m.items)}
		>
			<img src={thumbnailURL(m.items[0].root, m.items[0].path, 160, m.items[0].modified)} alt="" />
			<b>{m.items.length}</b>
		</button>
	{/each}
	<div class="zoom">
		<button type="button" aria-label={t('photos.zoomIn')} onclick={() => setZoom(zoom + 1)}
			><Icon name="plus" size={16} /></button
		>
		<button type="button" aria-label={t('photos.zoomOut')} onclick={() => setZoom(zoom - 1)}
			>−</button
		>
	</div>
	<a class="attr" href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer"
		>© OpenStreetMap-Mitwirkende</a
	>
</div>

<style>
	.map {
		position: relative;
		height: min(62dvh, 620px);
		min-height: 320px;
		border-radius: 14px;
		overflow: hidden;
		border: 1px solid var(--border);
		background: #aad3df;
		touch-action: none;
		cursor: grab;
		user-select: none;
	}
	.tile {
		position: absolute;
		width: 256px;
		height: 256px;
		pointer-events: none;
	}
	.marker {
		position: absolute;
		transform: translate(-50%, -100%);
		border: 0;
		padding: 0;
		background: none;
		cursor: pointer;
		display: flex;
		flex-direction: column;
		align-items: center;
	}
	.marker img {
		width: 54px;
		height: 54px;
		object-fit: cover;
		border-radius: 12px;
		border: 3px solid #fff;
		box-shadow: 0 6px 18px rgb(0 0 0 / 0.45);
	}
	.marker b {
		margin-top: 3px;
		font-size: 12px;
		padding: 0 8px;
		border-radius: 999px;
		background: var(--accent);
		color: var(--on-accent);
	}
	.zoom {
		position: absolute;
		top: 10px;
		right: 10px;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.zoom button {
		width: 34px;
		height: 34px;
		border-radius: 8px;
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text);
		font-size: 18px;
		display: grid;
		place-items: center;
		cursor: pointer;
	}
	.attr {
		position: absolute;
		right: 6px;
		bottom: 4px;
		font-size: 11px;
		padding: 1px 6px;
		border-radius: 6px;
		background: rgb(255 255 255 / 0.8);
		color: #333;
	}
</style>
