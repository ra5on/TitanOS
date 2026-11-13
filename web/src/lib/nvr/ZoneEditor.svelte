<script lang="ts">
	// PLAN 88: zones and masks drawn over the camera's still picture, with
	// mouse, touch or keyboard. A new area is drawn point by point (a tap or
	// click each, "Fertig" or a tap on the first point closes it); a chosen
	// area shows its points: drag one to move it, "+" in the middle of an
	// edge adds one, a chosen point can be removed (also with Entf), arrow
	// keys move it. Points are relative to the picture (0 to 1). The page
	// only scrolls past the picture while nothing is being drawn or moved.
	import { apiPath } from '../cluster-node.ts';
	import Icon from '../components/Icon.svelte';
	import { t, type MessageKey } from '../i18n/index.svelte.ts';
	import {
		AREA_KINDS,
		areasOf,
		centre,
		closable,
		MAX_POINTS,
		MIN_POINTS,
		nudge,
		point,
		roomFor,
		withMasks,
		zoneId,
		type AreaKind,
	} from './areas.ts';
	import type { AreaPoint, CameraMasks, CameraZone } from './types.ts';

	let {
		camera,
		zones = $bindable(),
		masks = $bindable(),
		onzonegone,
	}: {
		/** The camera's ID; the picture comes from it. */
		camera: string;
		zones: CameraZone[];
		masks: CameraMasks;
		/** A zone was removed (messages limited to it must forget it). */
		onzonegone?: (id: string, name: string) => void;
	} = $props();
	const uid = $props.id();

	type Sel = { kind: AreaKind; index: number };
	let selected = $state<Sel | null>(null);
	let pointAt = $state<number | null>(null);
	let drawing = $state<{ kind: AreaKind; points: AreaPoint[] } | null>(null);
	let stage = $state<HTMLDivElement>();
	let width = $state(0);
	let height = $state(0);
	let picture = $state<'loading' | 'ok' | 'none'>('loading');
	let aspect = $state(16 / 9);
	let stamp = $state(0);
	const src = $derived(
		apiPath(
			`/nvr/cameras/${encodeURIComponent(camera)}/snapshot.jpg?height=720${stamp ? `&t=${stamp}` : ''}`,
		),
	);

	const kindName = (kind: AreaKind) => t(`nvr.areas.kind.${kind}` as MessageKey);
	function labelOf(kind: AreaKind, index: number): string {
		if (kind === 'zone') return zones[index]?.name || t('nvr.areas.unnamed');
		return t(`nvr.areas.maskName.${kind}` as MessageKey, { n: index + 1 });
	}
	const all = $derived(
		AREA_KINDS.flatMap((kind) =>
			areasOf(kind, zones, masks).map((area, index) => ({
				kind,
				index,
				points: area.points,
				label: labelOf(kind, index),
			})),
		),
	);
	const same = (a: Sel | null, b: Sel | null) =>
		!!a && !!b && a.kind === b.kind && a.index === b.index;
	const current = $derived(selected ? all.find((a) => same(a, selected)) : undefined);
	const editing = $derived(!!drawing || !!selected);

	function pointsOf(sel: Sel): AreaPoint[] {
		return areasOf(sel.kind, zones, masks)[sel.index]?.points ?? [];
	}
	function setPoints(sel: Sel, points: AreaPoint[]) {
		if (sel.kind === 'zone') zones = zones.map((z, i) => (i === sel.index ? { ...z, points } : z));
		else
			masks = withMasks(
				masks,
				sel.kind,
				masks[sel.kind].map((m, i) => (i === sel.index ? { ...m, points } : m)),
			);
	}

	/** Where a pointer is in the picture, 0 to 1. */
	function at(e: PointerEvent | MouseEvent): AreaPoint {
		const r = stage!.getBoundingClientRect();
		return point((e.clientX - r.left) / r.width, (e.clientY - r.top) / r.height);
	}

	// ---- Drawing ----
	function begin(kind: AreaKind) {
		selected = null;
		pointAt = null;
		drawing = { kind, points: [] };
	}
	function nextZoneName(): string {
		for (let n = zones.length + 1; ; n++) {
			const name = t('nvr.areas.zoneDefault', { n });
			if (!zones.some((z) => z.name.toLowerCase() === name.toLowerCase())) return name;
		}
	}
	function finish() {
		if (!drawing || !closable(drawing.points)) return;
		const { kind, points } = drawing;
		if (kind === 'zone') {
			const name = nextZoneName();
			zones = [
				...zones,
				{
					id: zoneId(
						name,
						zones.map((z) => z.id ?? ''),
					),
					name,
					points,
				},
			];
			selected = { kind, index: zones.length - 1 };
		} else {
			masks = withMasks(masks, kind, [...masks[kind], { points }]);
			selected = { kind, index: masks[kind].length - 1 };
		}
		drawing = null;
		pointAt = null;
	}

	// ---- Pointer: add, choose, drag ----
	let drag: { sel: Sel; point: number; id: number } | null = null;
	function stageDown(e: PointerEvent) {
		if (e.button !== 0 || drag || !drawing) return;
		e.preventDefault();
		if (drawing.points.length < MAX_POINTS) drawing.points = [...drawing.points, at(e)];
	}
	/**
	 * A tap or click chooses the area under it (or none). Not on pointerdown:
	 * a finger that starts on an area to scroll the page must not choose it
	 * (a chosen area stops the page from scrolling over the picture).
	 */
	let dragged = false;
	function stageClick(e: MouseEvent) {
		if (drawing || dragged || (e.target as Element).closest('.handle')) {
			dragged = false;
			return;
		}
		const hit = (e.target as Element).closest('[data-area]');
		if (hit) {
			const [kind, index] = hit.getAttribute('data-area')!.split(':');
			selected = { kind: kind as AreaKind, index: Number(index) };
		} else selected = null;
		pointAt = null;
	}
	function grab(e: PointerEvent, sel: Sel, index: number) {
		if (e.button !== 0) return;
		e.preventDefault();
		e.stopPropagation();
		selected = sel;
		pointAt = index;
		drag = { sel, point: index, id: e.pointerId };
		stage?.setPointerCapture(e.pointerId);
	}
	/** Puts a point in the middle of the edge after point `after`; false when full. */
	function insertMiddle(sel: Sel, after: number): boolean {
		const points = pointsOf(sel);
		if (points.length >= MAX_POINTS) return false;
		const a = points[after];
		const b = points[(after + 1) % points.length];
		setPoints(sel, [
			...points.slice(0, after + 1),
			point((a[0] + b[0]) / 2, (a[1] + b[1]) / 2),
			...points.slice(after + 1),
		]);
		return true;
	}
	function grabMiddle(e: PointerEvent, sel: Sel, after: number) {
		if (e.button !== 0) return;
		if (insertMiddle(sel, after)) grab(e, sel, after + 1);
	}
	function stageMove(e: PointerEvent) {
		if (!drag || e.pointerId !== drag.id) return;
		const points = [...pointsOf(drag.sel)];
		points[drag.point] = at(e);
		setPoints(drag.sel, points);
	}
	function stageUp(e: PointerEvent) {
		if (drag && e.pointerId === drag.id) {
			drag = null;
			// The click that ends a drag chooses nothing.
			dragged = true;
			setTimeout(() => (dragged = false), 0);
			if (stage?.hasPointerCapture(e.pointerId)) stage.releasePointerCapture(e.pointerId);
		}
	}

	// ---- Keyboard ----
	function handleKey(e: KeyboardEvent, sel: Sel, index: number) {
		if (e.key === 'Delete' || e.key === 'Backspace') {
			e.preventDefault();
			removePoint(sel, index);
			return;
		}
		const moved = nudge(pointsOf(sel)[index], e.key, e.shiftKey ? 0.05 : 0.01);
		if (!moved) return;
		e.preventDefault();
		const points = [...pointsOf(sel)];
		points[index] = moved;
		setPoints(sel, points);
	}
	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && drawing) {
			e.preventDefault();
			drawing = null;
		}
	}

	// ---- Remove ----
	function removePoint(sel: Sel, index: number) {
		const points = pointsOf(sel);
		if (points.length <= MIN_POINTS) return;
		setPoints(
			sel,
			points.filter((_, i) => i !== index),
		);
		pointAt = null;
	}
	function removeArea(sel: Sel) {
		if (sel.kind === 'zone') {
			const gone = zones[sel.index];
			zones = zones.filter((_, i) => i !== sel.index);
			if (gone?.id) onzonegone?.(gone.id, gone.name);
		} else
			masks = withMasks(
				masks,
				sel.kind,
				masks[sel.kind].filter((_, i) => i !== sel.index),
			);
		selected = null;
		pointAt = null;
	}
	function rename(index: number, name: string) {
		zones = zones.map((z, i) => (i === index ? { ...z, name } : z));
	}
	const nameTaken = (index: number) => {
		const name = zones[index]?.name.trim().toLowerCase() ?? '';
		return name !== '' && zones.some((z, i) => i !== index && z.name.trim().toLowerCase() === name);
	};

	const svgPoints = (points: AreaPoint[]) => points.map(([x, y]) => `${x},${y}`).join(' ');
	/**
	 * The edges that get a "+" in their middle: where its 44 px box covers no
	 * point's box and no "+" before it (a finger must hit what it means).
	 */
	const middles = $derived.by(() => {
		if (!current || drawing || current.points.length >= MAX_POINTS) return [];
		const px = (p: AreaPoint) => [p[0] * width, p[1] * height];
		const taken = current.points.map(px);
		const out: { after: number; at: AreaPoint }[] = [];
		current.points.forEach((p, i) => {
			const q = current.points[(i + 1) % current.points.length];
			const at: AreaPoint = [(p[0] + q[0]) / 2, (p[1] + q[1]) / 2];
			const [x, y] = px(at);
			if (taken.some(([tx, ty]) => Math.max(Math.abs(tx - x), Math.abs(ty - y)) < 46)) return;
			taken.push([x, y]);
			out.push({ after: i, at });
		});
		return out;
	});
</script>

<svelte:window onkeydown={onKeydown} />

<!-- PLAN 87: drawing at the left edge is no "Zurück". -->
<div class="areas" id="{uid}-areas" data-no-edge-swipe>
	<div class="tools" role="toolbar" aria-label={t('nvr.areas.tools')}>
		<span class="muted">{t('nvr.areas.new')}</span>
		{#each AREA_KINDS as kind (kind)}
			<button
				type="button"
				class="btn"
				disabled={!!drawing || picture !== 'ok' || !roomFor(kind, zones, masks)}
				onclick={() => begin(kind)}><Icon name="plus" size={15} />{kindName(kind)}</button
			>
		{/each}
		<button
			type="button"
			class="btn"
			disabled={!!drawing}
			onclick={() => ((picture = 'loading'), (stamp = Date.now()))}
			><Icon name="refresh" size={15} />{t('nvr.areas.reload')}</button
		>
	</div>

	{#if drawing}
		<div class="drawing" role="status">
			<p>
				{t('nvr.areas.drawing', { kind: kindName(drawing.kind) })}
				{t('nvr.areas.pointsCount', { count: drawing.points.length })}
			</p>
			<div class="drawing-actions">
				<button type="button" class="btn" onclick={() => (drawing = null)}
					>{t('common.cancel')}</button
				>
				<button
					type="button"
					class="btn btn-primary"
					disabled={!closable(drawing.points)}
					onclick={finish}><Icon name="check" size={15} />{t('nvr.areas.done')}</button
				>
			</div>
		</div>
	{/if}

	{#if picture === 'none'}
		<p class="notice-warn note" role="status">{t('nvr.areas.noPicture')}</p>
	{/if}
	<!-- The keyboard chooses areas in the list below and moves points with the
	     arrow keys on their handles; the picture itself is for pointers. -->
	<!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events -->
	<div
		class="stage"
		class:hidden={picture === 'none'}
		class:editing
		class:drawing={!!drawing}
		bind:this={stage}
		bind:clientWidth={width}
		bind:clientHeight={height}
		style:aspect-ratio={aspect}
		onpointerdown={stageDown}
		onpointermove={stageMove}
		onpointerup={stageUp}
		onpointercancel={stageUp}
		onclick={stageClick}
	>
		<img
			class="still"
			{src}
			alt={t('nvr.areas.picture')}
			draggable="false"
			onload={(e) => {
				const img = e.currentTarget as HTMLImageElement;
				if (img.naturalWidth && img.naturalHeight) aspect = img.naturalWidth / img.naturalHeight;
				picture = 'ok';
			}}
			onerror={() => (picture = 'none')}
		/>
		{#if picture === 'ok'}
			<svg class="shapes" viewBox="0 0 1 1" preserveAspectRatio="none" aria-hidden="true">
				{#each all as area (`${area.kind}:${area.index}`)}
					<polygon
						class="shape {area.kind}"
						class:sel={same(area, selected)}
						data-area="{area.kind}:{area.index}"
						points={svgPoints(area.points)}
						vector-effect="non-scaling-stroke"
					/>
				{/each}
				{#if drawing && drawing.points.length > 0}
					<polyline
						class="draft {drawing.kind}"
						points={svgPoints(drawing.points)}
						vector-effect="non-scaling-stroke"
					/>
				{/if}
			</svg>
			{#each all as area (`label-${area.kind}:${area.index}`)}
				{@const [cx, cy] = centre(area.points)}
				<!-- The names stand in the list below as well (for screen readers there). -->
				<span
					aria-hidden="true"
					class="label {area.kind}"
					class:sel={same(area, selected)}
					style:left="{Math.min(88, Math.max(12, cx * 100))}%"
					style:top="{Math.min(92, Math.max(8, cy * 100))}%">{area.label}</span
				>
			{/each}
			{#if current && !drawing}
				{@const sel = { kind: current.kind, index: current.index }}
				{#each middles as { after, at } (after)}
					<button
						type="button"
						class="handle middle"
						style:left="{at[0] * 100}%"
						style:top="{at[1] * 100}%"
						aria-label={t('nvr.areas.addPoint', {
							a: after + 1,
							b: ((after + 1) % current.points.length) + 1,
						})}
						onpointerdown={(e) => grabMiddle(e, sel, after)}
						onclick={(e) => {
							// A keyboard press (no pointer): the point goes in the middle.
							if (e.detail === 0 && insertMiddle(sel, after)) pointAt = after + 1;
						}}><span class="dot" aria-hidden="true">+</span></button
					>
				{/each}
				{#each current.points as p, i (i)}
					<button
						type="button"
						class="handle"
						class:on={pointAt === i}
						style:left="{p[0] * 100}%"
						style:top="{p[1] * 100}%"
						aria-label={t('nvr.areas.point', { n: i + 1, area: current.label })}
						aria-pressed={pointAt === i}
						onpointerdown={(e) => grab(e, sel, i)}
						onfocus={() => (pointAt = i)}
						onkeydown={(e) => handleKey(e, sel, i)}><span class="dot"></span></button
					>
				{/each}
			{/if}
			{#if drawing}
				{#each drawing.points as p, i (i)}
					<!-- Only the first point catches a tap (to close the area); a tap
					     near another point sets a new one. -->
					<button
						type="button"
						class="handle draft-point"
						class:first={i === 0}
						class:closes={i === 0 && closable(drawing.points)}
						tabindex={i === 0 && closable(drawing.points) ? 0 : -1}
						style:left="{p[0] * 100}%"
						style:top="{p[1] * 100}%"
						aria-label={i === 0 && closable(drawing.points)
							? t('nvr.areas.closeHere')
							: t('nvr.areas.point', { n: i + 1, area: kindName(drawing.kind) })}
						onpointerdown={(e) => e.stopPropagation()}
						onclick={() => {
							if (i === 0 && drawing && closable(drawing.points)) finish();
						}}><span class="dot"></span></button
					>
				{/each}
			{/if}
		{/if}
	</div>
	{#if picture === 'ok'}<p class="muted note">{t('nvr.areas.editHint')}</p>{/if}

	{#if all.length > 0}
		<ul class="list" aria-label={t('nvr.areas.list')}>
			{#each all as area (`item-${area.kind}:${area.index}`)}
				<li>
					<button
						type="button"
						class="item"
						class:sel={same(area, selected)}
						aria-pressed={same(area, selected)}
						onclick={() => {
							drawing = null;
							selected = same(area, selected) ? null : { kind: area.kind, index: area.index };
							pointAt = null;
						}}
					>
						<span class="swatch {area.kind}" aria-hidden="true"></span>
						<span class="item-text"
							><b>{area.label}</b>
							<small
								>{kindName(area.kind)} · {t('nvr.areas.pointsCount', {
									count: area.points.length,
								})}</small
							></span
						>
					</button>
				</li>
			{/each}
		</ul>
	{/if}

	{#if current}
		{@const sel = { kind: current.kind, index: current.index }}
		<div class="panel">
			{#if current.kind === 'zone'}
				<label class="name" for="{uid}-zone-name"
					><span>{t('nvr.areas.name')}</span>
					<input
						id="{uid}-zone-name"
						class="field"
						type="text"
						maxlength="40"
						value={zones[current.index]?.name ?? ''}
						aria-invalid={nameTaken(current.index) || !zones[current.index]?.name.trim()
							? 'true'
							: undefined}
						oninput={(e) => rename(current.index, e.currentTarget.value)}
					/></label
				>
				{#if nameTaken(current.index)}<p class="bad" role="alert">{t('nvr.areas.nameTaken')}</p>
				{:else if !zones[current.index]?.name.trim()}<p class="bad" role="alert">
						{t('nvr.areas.nameEmpty')}
					</p>{/if}
			{:else}
				<p class="muted">{t(`nvr.areas.kindHint.${current.kind}` as MessageKey)}</p>
			{/if}
			<div class="panel-actions">
				<button
					type="button"
					class="btn"
					disabled={pointAt === null || current.points.length <= MIN_POINTS}
					onclick={() => pointAt !== null && removePoint(sel, pointAt)}
					>{t('nvr.areas.removePoint')}</button
				>
				<button type="button" class="btn btn-danger-outline" onclick={() => removeArea(sel)}
					><Icon name="trash" size={15} />{t('nvr.areas.remove')}</button
				>
			</div>
		</div>
	{/if}
</div>

<style>
	.areas {
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		min-width: 0;
	}
	.tools,
	.drawing-actions,
	.panel-actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.tools .btn,
	.drawing-actions .btn,
	.panel-actions .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.drawing {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--accent);
		border-radius: 10px;
	}
	.drawing p,
	.note,
	.panel p {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.stage {
		position: relative;
		width: 100%;
		max-width: 960px;
		border-radius: 10px;
		overflow: hidden;
		background: #0b0f14;
		user-select: none;
		-webkit-user-select: none;
		touch-action: pan-y pinch-zoom;
	}
	/* While something is drawn or chosen, a finger draws and drags instead of scrolling. */
	.stage.editing {
		touch-action: none;
	}
	.stage.drawing {
		cursor: crosshair;
	}
	.stage.hidden {
		display: none;
	}
	.still {
		display: block;
		width: 100%;
		height: 100%;
		object-fit: fill;
		pointer-events: none;
	}
	.shapes {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		overflow: visible;
	}
	.shape {
		stroke-width: 2;
		cursor: pointer;
	}
	.shape.zone {
		fill: color-mix(in srgb, var(--accent) 24%, transparent);
		stroke: var(--accent);
	}
	.shape.motion {
		fill: rgba(0, 0, 0, 0.5);
		stroke: #fff;
		stroke-dasharray: 6 4;
	}
	.shape.objects {
		fill: rgba(0, 0, 0, 0.5);
		stroke: #fff;
		stroke-dasharray: 2 4;
	}
	.shape.sel {
		stroke-width: 3.5;
	}
	.draft {
		fill: none;
		stroke: #fff;
		stroke-width: 2.5;
	}
	.label {
		position: absolute;
		transform: translate(-50%, -50%);
		max-width: 40%;
		padding: 2px 7px;
		border-radius: 6px;
		background: rgba(0, 0, 0, 0.72);
		color: #fff;
		font-size: var(--text-sm);
		line-height: 1.3;
		text-align: center;
		overflow-wrap: anywhere;
		pointer-events: none;
	}
	.label.zone {
		border: 1.5px solid var(--accent);
	}
	.label.motion,
	.label.objects {
		border: 1.5px dashed #fff;
	}
	.handle {
		position: absolute;
		width: 44px;
		height: 44px;
		transform: translate(-50%, -50%);
		display: grid;
		place-items: center;
		padding: 0;
		border: 0;
		background: transparent;
		cursor: grab;
		touch-action: none;
	}
	.handle .dot {
		width: 16px;
		height: 16px;
		border-radius: 50%;
		background: #fff;
		border: 2.5px solid var(--accent);
		box-shadow: 0 0 0 1.5px rgba(0, 0, 0, 0.6);
	}
	.handle.on .dot {
		width: 22px;
		height: 22px;
		background: var(--accent);
		border-color: #fff;
	}
	.handle:focus-visible {
		outline: none;
	}
	.handle:focus-visible .dot {
		outline: 3px solid var(--accent);
		outline-offset: 3px;
	}
	.handle.middle .dot {
		width: 18px;
		height: 18px;
		display: grid;
		place-items: center;
		font-size: 14px;
		font-weight: 700;
		line-height: 1;
		color: var(--accent);
		background: rgba(255, 255, 255, 0.85);
		border-width: 1.5px;
	}
	.handle.draft-point {
		pointer-events: none;
	}
	.handle.draft-point.closes {
		pointer-events: auto;
	}
	.handle.draft-point .dot {
		width: 12px;
		height: 12px;
		border-color: #fff;
		background: var(--accent);
	}
	.handle.draft-point.first .dot {
		width: 18px;
		height: 18px;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 14rem), 1fr));
		gap: var(--space-2);
	}
	.item {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		width: 100%;
		min-height: 44px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.item.sel {
		border-color: var(--accent);
		box-shadow: inset 0 0 0 1px var(--accent);
	}
	.item-text {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.item-text small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.swatch {
		flex: none;
		width: 18px;
		height: 14px;
		border-radius: 3px;
	}
	.swatch.zone {
		background: color-mix(in srgb, var(--accent) 35%, transparent);
		border: 2px solid var(--accent);
	}
	.swatch.motion {
		background: #333;
		border: 2px dashed var(--muted);
	}
	.swatch.objects {
		background: #333;
		border: 2px dotted var(--muted);
	}
	.panel {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-2-5) var(--space-3);
		border: 1px solid var(--border);
		border-radius: 10px;
	}
	.name {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.name .field {
		width: min(16rem, 100%);
	}
	.bad {
		color: var(--error-text);
	}
</style>
