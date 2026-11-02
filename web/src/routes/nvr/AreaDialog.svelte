<script lang="ts">
	// Zones and masks of the motion check, drawn on a current picture of the
	// camera: drag to draw a rectangle. Zones: only movement there counts.
	// Masks: movement there never counts (street, trees, neighbours).
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { untrack } from 'svelte';
	import { MAX_AREAS, normArea, type Area, type Camera } from './nvr.ts';

	type Props = { camera: Camera; onclose: () => void; onsaved: () => void };
	let { camera, onclose, onsaved }: Props = $props();

	const initial = untrack(() => camera);
	let zones = $state<Area[]>($state.snapshot(initial.zones ?? []));
	let masks = $state<Area[]>($state.snapshot(initial.masks ?? []));
	let kind = $state<'zone' | 'mask'>('mask');
	let drawing = $state<{ x0: number; y0: number; x1: number; y1: number } | null>(null);
	let picture = $state(true);
	let error = $state('');
	let saving = $state(false);
	let box = $state<HTMLDivElement | null>(null);

	function point(e: PointerEvent) {
		const r = box!.getBoundingClientRect();
		return {
			x: Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)),
			y: Math.min(1, Math.max(0, (e.clientY - r.top) / r.height)),
		};
	}
	function down(e: PointerEvent) {
		if (zones.length + masks.length >= MAX_AREAS * 2) return;
		const p = point(e);
		drawing = { x0: p.x, y0: p.y, x1: p.x, y1: p.y };
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!drawing) return;
		const p = point(e);
		drawing.x1 = p.x;
		drawing.y1 = p.y;
	}
	function up() {
		if (!drawing) return;
		const a = normArea(drawing);
		drawing = null;
		if (!a) return;
		const list = kind === 'zone' ? zones : masks;
		if (list.length >= MAX_AREAS) {
			error = t('nvr.areasFull');
			return;
		}
		list.push(a);
	}

	async function save() {
		saving = true;
		error = '';
		try {
			// The camera as stored, with only the areas changed.
			// The camera as stored (without the status fields of the page),
			// with only the areas changed.
			const {
				status: _status,
				watch: _watch,
				segments: _segments,
				bytes: _bytes,
				latest: _latest,
				dayBytes: _dayBytes,
				...stored
			} = $state.snapshot(initial);
			await api.put(`/nvr/cameras/${initial.id}`, {
				...stored,
				zones: $state.snapshot(zones),
				masks: $state.snapshot(masks),
			});
			onsaved();
		} catch (e) {
			error = apiErrorText(e);
			saving = false;
		}
	}
	const pct = (v: number) => `${(v * 100).toFixed(2)}%`;
	const live = $derived(drawing ? normArea(drawing) : null);
</script>

<Dialog title={t('nvr.areasTitle', { name: initial.name })} locked={saving} {onclose}>
	<div class="body">
		<p class="muted small">{t('nvr.areasIntro')}</p>
		<div class="kinds" role="radiogroup" aria-label={t('nvr.areasKind')}>
			<label class="check"
				><input type="radio" bind:group={kind} value="mask" /><span>{t('nvr.areaMask')}</span
				></label
			>
			<label class="check"
				><input type="radio" bind:group={kind} value="zone" /><span>{t('nvr.areaZone')}</span
				></label
			>
		</div>
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="canvas"
			bind:this={box}
			onpointerdown={down}
			onpointermove={move}
			onpointerup={up}
			onpointercancel={() => (drawing = null)}
		>
			{#if picture}
				<img
					src={`/api/v1/nvr/cameras/${initial.id}/snapshot`}
					alt=""
					draggable="false"
					onerror={() => (picture = false)}
				/>
			{:else}
				<p class="nopic muted small">{t('nvr.areasNoPicture')}</p>
			{/if}
			{#each zones as a, i (i)}
				<span
					class="area zone"
					style:left={pct(a.x)}
					style:top={pct(a.y)}
					style:width={pct(a.w)}
					style:height={pct(a.h)}
				></span>
			{/each}
			{#each masks as a, i (i)}
				<span
					class="area mask"
					style:left={pct(a.x)}
					style:top={pct(a.y)}
					style:width={pct(a.w)}
					style:height={pct(a.h)}
				></span>
			{/each}
			{#if live}
				<span
					class="area {kind}"
					style:left={pct(live.x)}
					style:top={pct(live.y)}
					style:width={pct(live.w)}
					style:height={pct(live.h)}
				></span>
			{/if}
		</div>
		<div class="lists">
			{#each zones as _, i (i)}
				<button type="button" class="btn" onclick={() => zones.splice(i, 1)}
					>{t('nvr.areaZone')} {i + 1} ✕</button
				>
			{/each}
			{#each masks as _, i (i)}
				<button type="button" class="btn" onclick={() => masks.splice(i, 1)}
					>{t('nvr.areaMask')} {i + 1} ✕</button
				>
			{/each}
		</div>
		{#if error}<div class="notice-error" role="alert">{error}</div>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={saving}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" onclick={save} disabled={saving}
				>{t('common.save')}</button
			>
		</div>
	</div>
</Dialog>

<style>
	.body {
		display: grid;
		gap: 12px;
		min-width: min(640px, 100%);
	}
	.kinds {
		display: flex;
		gap: 16px;
	}
	.check {
		display: flex;
		gap: 6px;
		align-items: center;
	}
	.canvas {
		position: relative;
		aspect-ratio: 16 / 9;
		background: #000;
		border-radius: 8px;
		overflow: hidden;
		cursor: crosshair;
		touch-action: none;
		user-select: none;
	}
	.canvas img {
		width: 100%;
		height: 100%;
		object-fit: fill;
		display: block;
		pointer-events: none;
	}
	.nopic {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		margin: 0;
	}
	.area {
		position: absolute;
		pointer-events: none;
		border: 2px solid;
	}
	.area.zone {
		border-color: #3ddc84;
		background: rgb(61 220 132 / 0.2);
	}
	.area.mask {
		border-color: #ff5c5c;
		background: rgb(0 0 0 / 0.6);
	}
	.lists {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
</style>
