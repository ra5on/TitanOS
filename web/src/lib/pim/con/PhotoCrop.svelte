<script lang="ts" module>
	/** The edge of the square picture the crop makes (titand keeps at most 512 px). */
	export const CROP_EDGE = 512;

	/**
	 * The square of the source shown at a zoom and offset: `zoom` 1 shows
	 * the largest square of the picture, the offsets (−1 … 1) move it from
	 * the middle to the edges.
	 */
	export function cropBox(w: number, h: number, zoom: number, x: number, y: number) {
		const side = Math.min(w, h) / Math.max(1, zoom);
		const sx = ((w - side) / 2) * (1 + Math.max(-1, Math.min(1, x)));
		const sy = ((h - side) / 2) * (1 + Math.max(-1, Math.min(1, y)));
		return { sx, sy, side };
	}
</script>

<script lang="ts">
	// 1.13 (owner 18 Nov 2026): a contact's picture is cut square before it
	// goes up – like the iPhone's "Bewegen und skalieren": the picture in a
	// square frame, moved by dragging (or the arrow keys) and zoomed with the
	// slider. The result is a JPEG of 512 × 512 px at most, made in the
	// browser; titand then checks and keeps it as before (≤ 512 px, ≤ 256
	// KiB). A picture the browser cannot read (HEIC) never comes here.
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		/** The picture: an object URL of a file, or a picture of the NAS. */
		src: string;
		onpick: (file: File, preview: string) => void;
		/** The picture as it is, when the browser cannot read it (titand may: HEIC is refused there too). */
		onraw?: () => void;
		onclose: () => void;
	};
	let { src, onpick, onraw, onclose }: Props = $props();

	let img = $state<HTMLImageElement>();
	let natural = $state({ w: 0, h: 0 });
	let failed = $state(false);
	let zoom = $state(1);
	let x = $state(0);
	let y = $state(0);
	let busy = $state(false);

	const VIEW = 260;
	// The picture drawn in the frame: the crop box scaled to the frame.
	const shown = $derived.by(() => {
		if (!natural.w) return null;
		const { sx, sy, side } = cropBox(natural.w, natural.h, zoom, x, y);
		const scale = VIEW / side;
		return { w: natural.w * scale, h: natural.h * scale, left: -sx * scale, top: -sy * scale };
	});

	let dragFrom: { px: number; py: number; x: number; y: number; id: number } | null = null;
	function down(e: PointerEvent) {
		if (!natural.w) return;
		dragFrom = { px: e.clientX, py: e.clientY, x, y, id: e.pointerId };
		(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!dragFrom || e.pointerId !== dragFrom.id || !natural.w) return;
		const { side } = cropBox(natural.w, natural.h, zoom, 0, 0);
		const scale = VIEW / side;
		// Moving the picture right shows more of its left: the offset goes down.
		const rangeX = ((natural.w - side) / 2) * scale;
		const rangeY = ((natural.h - side) / 2) * scale;
		if (rangeX > 0) x = Math.max(-1, Math.min(1, dragFrom.x - (e.clientX - dragFrom.px) / rangeX));
		if (rangeY > 0) y = Math.max(-1, Math.min(1, dragFrom.y - (e.clientY - dragFrom.py) / rangeY));
	}
	function key(e: KeyboardEvent) {
		const step = 0.1;
		if (e.key === 'ArrowLeft') x = Math.max(-1, x - step);
		else if (e.key === 'ArrowRight') x = Math.min(1, x + step);
		else if (e.key === 'ArrowUp') y = Math.max(-1, y - step);
		else if (e.key === 'ArrowDown') y = Math.min(1, y + step);
		else return;
		e.preventDefault();
	}

	async function take() {
		if (!img || !natural.w) return;
		busy = true;
		try {
			const { sx, sy, side } = cropBox(natural.w, natural.h, zoom, x, y);
			const edge = Math.round(Math.min(CROP_EDGE, side));
			const canvas = document.createElement('canvas');
			canvas.width = canvas.height = edge;
			const ctx = canvas.getContext('2d');
			if (!ctx) throw new Error('no canvas');
			ctx.imageSmoothingQuality = 'high';
			ctx.drawImage(img, sx, sy, side, side, 0, 0, edge, edge);
			const blob = await new Promise<Blob | null>((r) => canvas.toBlob(r, 'image/jpeg', 0.9));
			if (!blob) throw new Error('no picture');
			const file = new File([blob], 'kontakt.jpg', { type: 'image/jpeg' });
			onpick(file, URL.createObjectURL(blob));
		} catch {
			failed = true;
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('contacts.crop.title')} locked={busy} {onclose}>
	<p class="lead">{t('contacts.crop.text')}</p>
	<button
		type="button"
		class="frame"
		style:--view="{VIEW}px"
		aria-label={t('contacts.crop.frame')}
		onpointerdown={down}
		onpointermove={move}
		onpointerup={() => (dragFrom = null)}
		onpointercancel={() => (dragFrom = null)}
		onkeydown={key}
		data-crop-frame
	>
		<img
			bind:this={img}
			{src}
			alt=""
			draggable="false"
			style:width={shown ? `${shown.w}px` : undefined}
			style:height={shown ? `${shown.h}px` : undefined}
			style:left={shown ? `${shown.left}px` : undefined}
			style:top={shown ? `${shown.top}px` : undefined}
			onload={() => img && (natural = { w: img.naturalWidth, h: img.naturalHeight })}
			onerror={() => (failed = true)}
		/>
	</button>
	<label class="zoom">
		<Icon name="search" size={15} />
		<span class="sr-only">{t('contacts.crop.zoom')}</span>
		<input type="range" min="1" max="4" step="0.05" bind:value={zoom} />
	</label>
	{#if failed}<p class="notice-error" role="alert">{t('contacts.crop.failed')}</p>{/if}
	<div class="dialog-actions">
		{#if failed && onraw}<button type="button" class="btn" onclick={onraw}
				>{t('contacts.crop.raw')}</button
			>{/if}
		<button type="button" class="btn" disabled={busy} onclick={onclose}>{t('common.cancel')}</button
		>
		<button
			type="button"
			class="btn btn-primary"
			disabled={busy || !natural.w || failed}
			onclick={take}
			data-crop-take><Icon name="check" size={15} />{t('contacts.crop.take')}</button
		>
	</div>
</Dialog>

<style>
	.lead {
		margin: 0 0 var(--space-3);
		overflow-wrap: break-word;
	}
	.frame {
		display: block;
		padding: 0;
		border: 0;
		position: relative;
		width: var(--view);
		height: var(--view);
		max-width: 100%;
		margin: 0 auto var(--space-3);
		overflow: hidden;
		border-radius: 50%;
		background: var(--chip);
		cursor: grab;
		touch-action: none;
	}
	.frame:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.frame img {
		position: absolute;
		max-width: none;
		user-select: none;
		pointer-events: none;
	}
	.zoom {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--muted);
		margin-bottom: var(--space-3);
	}
	.zoom input {
		flex: 1;
		min-height: 44px;
	}
</style>
