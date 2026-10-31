<script lang="ts">
	import TemplateDialog from './TemplateDialog.svelte';
	import { templateLayout, type DashboardTemplate } from './tiles.ts';
	import { tick, untrack, onDestroy } from 'svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	import { session } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { dashboardControl } from './control.svelte.ts';
	import { positionGrid } from './position-grid.svelte.ts';
	import { columnOrder, dropLayout, type Rect } from './positions.ts';
	import { temperatureSelection } from './temperatures.ts';
	import { dashboardData } from './dashboard-data.svelte.ts';
	import {
		inspectorItems as itemsOf,
		tileSummary,
		tileTitle,
		workloadGroups as groupsOf,
	} from './tile-info.ts';
	import Icon from '#lib/components/Icon.svelte';
	import NavigationGuard from '#lib/operations/NavigationGuard.svelte';
	import EditorToolbar from './EditorToolbar.svelte';
	import AddTiles from './AddTiles.svelte';
	import EditorConfirm from './EditorConfirm.svelte';
	import TileHead from './TileHead.svelte';
	import { vmOsIcon } from '#lib/vm-os.ts';
	import {
		ADMIN_TILES,
		addableTiles,
		defaultPlacement,
		sanitizeLayout,
		tileSpec,
		workload,
		sourceId,
		duplicateId,
		type TileId,
		type StaticTileId,
		type TilePlacement,
	} from './tiles.ts';
	import { tileOptions, tileStyle, STYLE_KEYS, type TileOptions } from './options.ts';
	import { flowLayout, columnCount, columnWidth, visibleColumns, legacyPositions } from './flow.ts';
	import { dashboardVisual, type DashboardVisual } from './visual.ts';
	import { dashboardScene } from './scene.svelte.ts';
	import TileEditor, { type EditPart } from './TileEditor.svelte';
	import TileContent from './TileContent.svelte';
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const hardwareKinds = () => [
		...new Set<StaticTileId>(
			(system.info?.accelerators ?? []).map((d) =>
				d.kind === 'npu' ? 'npu' : d.vendor === 'NVIDIA' ? 'nvidia' : 'igpu',
			),
		),
	];
	function defaults(): TilePlacement[] {
		const order: StaticTileId[] = [
			'system',
			'docker',
			'cpu',
			'vms',
			'memory',
			'volumes',
			...(hardwareKinds().length ? ['accelerators' as StaticTileId] : []),
			'shares',
			'network',
			'temperatures',
		];
		return order
			.map((id) => ({ id, w: 6, h: 2, span: 1, autoHeight: true }))
			.filter((p) => admin || !ADMIN_TILES.includes(sourceId(p.id) as StaticTileId));
	}
	function initialLayout() {
		if (!session.dashboard?.tiles) return defaults();
		const layout = flowLayout(session.dashboard);
		if (Number(session.dashboard?.version ?? 0) < 3 && layout.some((t) => t.id === 'system'))
			return [
				...layout,
				...(['cpu', 'memory'] as const)
					.filter((id) => !layout.some((t) => t.id === id))
					.map(defaultPlacement),
			];
		return layout;
	}
	let shown = $state(initialLayout()),
		mountedLayout = $state(initialLayout()),
		saved = $state(initialLayout());
	let options = $state<Record<string, TileOptions>>(
		untrack(() =>
			Object.fromEntries(
				shown.map((p) => [p.id, tileOptions(session.dashboard?.tileOptions, p.id)]),
			),
		),
	);
	let savedOptions = $state(untrack(() => structuredClone($state.snapshot(options))));
	let visual = $state(dashboardVisual(session.dashboard?.visual));
	let savedVisual = $state(dashboardVisual(session.dashboard?.visual));
	let galleryVisual = $state<DashboardVisual | null>(null);
	const effectiveVisual = $derived(galleryVisual ?? visual);
	const pendingImages = new Map<string, File>();
	$effect(() => {
		dashboardScene.preview = effectiveVisual;
	});
	onDestroy(() => {
		dashboardScene.preview = null;
		clearImages();
	});
	function clearImages() {
		pendingImages.clear();
		for (const url of Object.values(dashboardScene.urls)) URL.revokeObjectURL(url);
		dashboardScene.urls = {};
	}
	function pruneImages() {
		const retained = new Set(visual.images.map((image) => image.id));
		for (const snapshot of [...undo, ...redo]) {
			for (const image of dashboardVisual(JSON.parse(snapshot).visual).images)
				retained.add(image.id);
		}
		const urls = { ...dashboardScene.urls };
		for (const [id, url] of Object.entries(urls)) {
			if (retained.has(id)) continue;
			URL.revokeObjectURL(url);
			pendingImages.delete(id);
			delete urls[id];
		}
		dashboardScene.urls = urls;
	}
	function registerImage(id: string, file: File, url: string) {
		const previous = dashboardScene.urls[id];
		if (previous) URL.revokeObjectURL(previous);
		pendingImages.set(id, file);
		dashboardScene.urls = { ...dashboardScene.urls, [id]: url };
	}

	let editing = $state(false),
		saving = $state(false),
		error = $state(''),
		customize = $state<TileId | null>(null),
		part = $state<EditPart>('tile'),
		selection = $state('');
	let undo = $state<string[]>([]),
		redo = $state<string[]>([]),
		adding = $state(false),
		confirmCancel = $state(false),
		confirmRestore = $state(false);
	let columns = $state(columnCount(session.dashboard?.columns)),
		savedColumns = $state(columnCount(session.dashboard?.columns));
	let minimumWidth = $state(columnWidth(session.dashboard?.minimumWidth)),
		savedMinimumWidth = $state(columnWidth(session.dashboard?.minimumWidth));
	let availableWidth = $state(1200);
	const actualColumns = $derived(visibleColumns(availableWidth, columns, minimumWidth));
	let moving = $state<TileId | null>(null);
	let cancelPointer: (() => void) | null = null;
	onDestroy(() => cancelPointer?.());
	let board: HTMLElement | undefined;
	let boardRects: Record<string, Rect> = {};
	function attachGrid(element: HTMLElement) {
		board = element;
		const cleanup = positionGrid(element, () => ({
			items: actualColumns === columns ? mountedLayout : columnOrder(mountedLayout, columns),
			columns: actualColumns,
			explicit: actualColumns === columns,
			onlayout: (rects, width) => {
				boardRects = rects;
				if (availableWidth !== width) availableWidth = width;
			},
		}));
		return () => {
			cleanup();
			board = undefined;
		};
	}
	$effect(() => {
		dashboardControl.edit = startEditing;
		return () => {
			dashboardControl.edit = null;
			dashboardControl.editing = false;
		};
	});
	$effect(() => {
		dashboardControl.editing = editing;
	});
	let ghost = $state<{ left: number; top: number; width: number; height: number } | null>(null);
	const currentSettings = $derived(tileOptions(options, customize ?? 'cpu'));
	const currentPlacement = $derived(shown.find((p) => p.id === customize));
	const hidden = $derived(addableTiles(shown, { admin, hardware: hardwareKinds() }));
	const dirty = $derived(
		editing &&
			(JSON.stringify(shown) !== JSON.stringify(saved) ||
				JSON.stringify(options) !== JSON.stringify(savedOptions) ||
				JSON.stringify(visual) !== JSON.stringify(savedVisual) ||
				columns !== savedColumns ||
				minimumWidth !== savedMinimumWidth),
	);
	const watched = $derived(
		admin &&
			(editing || shown.some((p) => workload(p.id) || ['docker', 'vms'].includes(sourceId(p.id)))),
	);
	const watchesTailnet = $derived(
		admin && (editing || shown.some((p) => sourceId(p.id) === 'tailscale')),
	);
	const watchesNetbird = $derived(
		admin && (editing || shown.some((p) => sourceId(p.id) === 'netbird')),
	);
	const watchesShares = $derived(editing || shown.some((p) => sourceId(p.id) === 'shares'));
	const data = dashboardData({
		workloads: () => watched,
		tailnet: () => watchesTailnet,
		netbird: () => watchesNetbird,
		shares: () => watchesShares,
	});
	const workloadGroups = $derived(groupsOf(data));
	let initializedHardware = false;
	$effect(() => {
		if (initializedHardware || editing || !system.info) return;
		initializedHardware = true;
		if (session.dashboard?.tiles) return;
		const next = untrack(defaults);
		saved = next;
		rebuild(next);
	});
	const titleOf = (id: TileId) => tileTitle(id, { options, data });
	const osIconOf = (id: TileId) => {
		const target = workload(id);
		const vm = target?.kind === 'vm' ? data.vms.find((v) => v.name === target.name) : undefined;
		return vm ? vmOsIcon(vm) : 'monitor';
	};
	function readGrid(): TilePlacement[] {
		return shown;
	}

	function snapshot() {
		return JSON.stringify({
			tiles: readGrid(),
			options: $state.snapshot(options),
			visual: $state.snapshot(visual),
			columns,
			minimumWidth,
		});
	}
	function checkpoint() {
		if (!editing) return;
		const value = snapshot();
		if (undo.at(-1) !== value) undo = [...undo.slice(-39), value];
		redo = [];
	}
	function rebuild(layout: TilePlacement[]) {
		shown = layout;
		mountedLayout = layout;
	}
	function restore(value: string) {
		const state = JSON.parse(value);
		visual = dashboardVisual(state.visual);
		galleryVisual = null;
		columns = columnCount(state.columns);
		minimumWidth = columnWidth(state.minimumWidth);
		options = Object.fromEntries(
			sanitizeLayout(state).map((p) => [p.id, tileOptions(state.options, p.id)]),
		);
		rebuild(sanitizeLayout(state));
		if (customize && !state.tiles.some((p: TilePlacement) => p.id === customize)) customize = null;
	}
	function step(direction: 'undo' | 'redo') {
		const from = direction === 'undo' ? undo : redo;
		const value = from.at(-1);
		if (!value) return;
		const current = snapshot();
		if (direction === 'undo') {
			undo = undo.slice(0, -1);
			redo = [...redo, current];
		} else {
			redo = redo.slice(0, -1);
			undo = [...undo, current];
		}
		restore(value);
	}

	function startEditing() {
		error = '';
		undo = [];
		redo = [];
		shown = readGrid();
		editing = true;
		adding = false;
	}
	// Leaving the editor with unsaved changes asks first.
	function requestCancel() {
		if (dirty) confirmCancel = true;
		else cancel();
	}
	function openAdd() {
		customize = null;
		adding = true;
	}
	let choosingTemplate = $state(false);
	function useTemplate(template: DashboardTemplate | 'default') {
		checkpoint();
		rebuild(template === 'default' ? defaults() : templateLayout(template, admin));
		customize = null;
		choosingTemplate = false;
	}
	function restoreDefaults() {
		checkpoint();
		options = {};
		visual = dashboardVisual({ images: visual.images });
		columns = 2;
		minimumWidth = 350;
		rebuild(defaults());
		customize = null;
		confirmRestore = false;
	}
	function cancel() {
		confirmCancel = false;
		adding = false;
		visual = structuredClone($state.snapshot(savedVisual));
		galleryVisual = null;
		columns = savedColumns;
		minimumWidth = savedMinimumWidth;
		editing = false;
		customize = null;
		undo = [];
		redo = [];
		clearImages();
		options = structuredClone($state.snapshot(savedOptions));
		rebuild(structuredClone($state.snapshot(saved)));
	}
	async function finish() {
		if (saving) return;
		saving = true;
		error = '';
		const layout = legacyPositions(readGrid(), columns);
		const allOptions = Object.fromEntries(layout.map((p) => [p.id, tileOptions(options, p.id)]));
		const preferences = {
			...session.dashboard,
			version: 6,
			columns,
			minimumWidth,
			tiles: layout,
			tileOptions: allOptions,
			visual: $state.snapshot(visual),
		};
		try {
			const uploads = visual.images.filter((image) => pendingImages.has(image.id));
			if (uploads.length) {
				const form = new FormData();
				form.append('preferences', JSON.stringify(preferences));
				for (const image of uploads) form.append('image', pendingImages.get(image.id)!, image.id);
				await api.put('/preferences/dashboard', form);
			} else await api.put('/preferences/dashboard', preferences);
			savedVisual = structuredClone($state.snapshot(visual));
			clearImages();
			undo = [];
			redo = [];
			galleryVisual = null;
			adding = false;
			session.dashboard = preferences;
			saved = structuredClone(layout);
			savedColumns = columns;
			savedMinimumWidth = minimumWidth;
			savedOptions = structuredClone(allOptions);
			options = allOptions;
			editing = false;
			customize = null;
			rebuild(layout);
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			saving = false;
		}
	}
	function configure(id: TileId, p: EditPart = 'tile') {
		customize = id;
		part = p;
		adding = false;
	}
	function changeOption(key: keyof TileOptions, value: unknown) {
		if (!customize) return;
		checkpoint();
		options = {
			...options,
			[customize]: tileOptions({ [customize]: { ...currentSettings, [key]: value } }, customize),
		};
	}
	function size(w: number, h: number, autoHeight?: boolean, pixels?: number) {
		if (!customize || currentPlacement?.locked) return;
		checkpoint();
		rebuild(
			sanitizeLayout({
				tiles: readGrid().map((p) =>
					p.id === customize
						? {
								...p,
								span: Math.max(1, Math.min(4, w)),
								h,
								height: pixels ?? (h === p.h && autoHeight === undefined ? p.height : h * 40),
								...(autoHeight === undefined ? {} : { autoHeight }),
							}
						: p,
				),
			}),
		);
	}
	function heightRows(p: TilePlacement) {
		const height = document
			.querySelector<HTMLElement>(`[gs-id="${CSS.escape(p.id)}"] .dashboard-tile`)
			?.getBoundingClientRect().height;
		return height ? Math.max(2, Math.min(40, Math.ceil(height / 40))) : p.h;
	}

	function duplicate() {
		if (!customize || shown.length >= 100) return;
		checkpoint();
		const id = duplicateId(customize);
		options = { ...options, [id]: structuredClone($state.snapshot(currentSettings)) };
		rebuild([
			...readGrid(),
			{
				...currentPlacement!,
				id,
				x: undefined,
				y: undefined,
				column: undefined,
				offset: undefined,
				locked: false,
			},
		]);
		customize = id;
	}
	function hide(id: TileId) {
		checkpoint();
		rebuild(readGrid().filter((p) => p.id !== id));
		if (customize === id) customize = null;
	}
	function add(id: TileId) {
		if (shown.length >= 100) return;
		checkpoint();
		options = { ...options, [id]: tileOptions(options, id) };
		rebuild([...readGrid(), { ...defaultPlacement(id), span: 1, autoHeight: true }]);
		selection = '';
		// The new tile opens in the inspector; "Add a tile" reopens the list.
		customize = id;
		part = 'tile';
		adding = false;
		void tick().then(() =>
			document
				.querySelector(`.grid-stack-item[gs-id="${CSS.escape(id)}"]`)
				?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }),
		);
	}
	function resetTile() {
		if (!customize) return;
		checkpoint();
		const fresh = tileOptions(null, customize),
			next = { ...currentSettings };
		for (const key of STYLE_KEYS) (next as unknown as Record<string, unknown>)[key] = fresh[key];
		options = { ...options, [customize]: next };
	}
	// One place up or down in the order shown on screen.
	function moveTile(id: TileId, direction: number) {
		const narrow = actualColumns !== columns;
		const layout = narrow ? columnOrder(shown, columns) : [...shown],
			from = layout.findIndex((p) => p.id === id),
			to = from + direction;
		if (from < 0 || to < 0 || to >= layout.length) return;
		checkpoint();
		// Narrow: the tile joins the wished column of the tile it passes.
		const passed = layout[to];
		const [item] = layout.splice(from, 1);
		layout.splice(to, 0, narrow ? { ...item, column: passed.column, offset: 0 } : item);
		rebuild(narrow ? layout : layout.map((p) => ({ ...p, column: undefined, offset: undefined })));
	}
	async function keyboardMove(e: KeyboardEvent, id: TileId) {
		if (
			shown.find((p) => p.id === id)?.locked ||
			!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)
		)
			return;
		e.preventDefault();
		const direction = e.key === 'ArrowLeft' || e.key === 'ArrowUp' ? -1 : 1;
		if (e.shiftKey) {
			customize = id;
			const p = shown.find((p) => p.id === id)!;
			size(
				Math.min(p.span ?? 1, actualColumns) +
					(e.key === 'ArrowLeft' || e.key === 'ArrowRight' ? direction : 0),
				Math.max(
					2,
					(p.autoHeight === false ? p.h : heightRows(p)) +
						(e.key === 'ArrowUp' || e.key === 'ArrowDown' ? direction : 0),
				),
				e.key === 'ArrowUp' || e.key === 'ArrowDown' ? false : undefined,
			);
		} else moveTile(id, direction);
		await tick();
		document
			.querySelector<HTMLButtonElement>(`.grid-stack-item[gs-id="${CSS.escape(id)}"] .grip`)
			?.focus({ preventScroll: true });
	}
	// The whole head moves the tile, not only the small grip; its other buttons keep their action.
	function headMove(event: PointerEvent, id: TileId) {
		const target = event.target as HTMLElement;
		if (!editing || target.closest('button:not(.grip)')) return;
		pointerMove(event, id);
	}
	function pointerMove(event: PointerEvent, id: TileId, resize = false) {
		const current = shown.find((p) => p.id === id)!;
		if (!editing || saving || current.locked || event.button !== 0 || !board) return;
		// No text selection or native drag while the pointer holds the tile.
		event.preventDefault();
		const handle = event.currentTarget as HTMLElement;
		const start = { x: event.clientX, y: event.clientY },
			r = boardRects[id];
		const cell = (availableWidth + 12) / actualColumns;
		const explicit = actualColumns === columns;
		const original = explicit
			? structuredClone($state.snapshot(shown))
			: columnOrder(structuredClone($state.snapshot(shown)), columns);
		const base = Object.fromEntries(Object.entries(boardRects).map(([id, r]) => [id, { ...r }]));
		const history = snapshot();
		let changed = false,
			preview = '';
		handle.setPointerCapture(event.pointerId);
		moving = id;
		const dropped = (g: NonNullable<typeof ghost>) =>
			dropLayout(
				original,
				id,
				base,
				{ column: Math.round(g.left / cell), top: g.top },
				actualColumns,
				explicit,
			);
		function move(e: PointerEvent) {
			const dx = e.clientX - start.x,
				dy = e.clientY - start.y;
			if (Math.abs(dx) + Math.abs(dy) < 5 && !changed) return;
			changed = true;
			const span = resize
				? Math.max(1, Math.min(actualColumns, r.span + Math.round(dx / cell)))
				: r.span;
			const column = resize
				? Math.min(r.column, actualColumns - span)
				: Math.max(0, Math.min(actualColumns - span, Math.round(r.column + dx / cell)));
			const top = Math.max(0, Math.round((r.top + dy) / 8) * 8);
			const height = resize
				? Math.max(80, Math.min(1600, Math.round((r.height + dy) / 8) * 8))
				: r.height;
			ghost = { left: column * cell, top: resize ? r.top : top, width: span * cell - 12, height };
			if (!resize) {
				// Live preview: the other tiles make room while dragging.
				const layout = dropped(ghost);
				const key = JSON.stringify(layout.map((p) => [p.id, p.column, p.offset]));
				if (key !== preview) {
					preview = key;
					rebuild(layout);
				}
			}
			if (resize) {
				rebuild(
					original.map((p) =>
						p.id === id
							? {
									...p,
									span: Math.round(dx / cell) === 0 ? current.span : span,
									h: Math.max(2, Math.ceil(height / 40)),
									height,
									autoHeight: Math.abs(dy) > 4 ? false : p.autoHeight,
								}
							: p,
					),
				);
			}
		}
		function cleanup() {
			window.removeEventListener('pointermove', onMove);
			window.removeEventListener('pointerup', onUp);
			window.removeEventListener('pointercancel', onCancel);
			window.removeEventListener('blur', abort);
			document.removeEventListener('keydown', escapeMove);
			if (handle.hasPointerCapture(event.pointerId)) handle.releasePointerCapture(event.pointerId);
			moving = null;
			ghost = null;
			cancelPointer = null;
		}
		function end() {
			if (changed && ghost) {
				if (!resize) rebuild(dropped(ghost));
				if (JSON.stringify(shown) !== JSON.stringify(original)) {
					undo = [...undo.slice(-39), history];
					redo = [];
				}
			}
			cleanup();
		}
		function abort() {
			if (changed) rebuild(original);
			cleanup();
		}
		// On the window, not the handle: the live preview reorders the tiles, which moves the
		// dragged tile's node and takes the pointer capture away from its handle.
		const onMove = (e: PointerEvent) => e.pointerId === event.pointerId && move(e);
		const onUp = (e: PointerEvent) => e.pointerId === event.pointerId && end();
		const onCancel = (e: PointerEvent) => e.pointerId === event.pointerId && abort();
		window.addEventListener('pointermove', onMove);
		window.addEventListener('pointerup', onUp);
		window.addEventListener('pointercancel', onCancel);
		// Switching windows mid-drag loses the pointerup; never stay half-dragged.
		window.addEventListener('blur', abort);
		cancelPointer = abort;
		function escapeMove(e: KeyboardEvent) {
			if (e.key === 'Escape') {
				e.preventDefault();
				abort();
			}
		}
		document.addEventListener('keydown', escapeMove);
	}
	async function runtimeOption(id: TileId, key: keyof TileOptions, value: unknown) {
		if (saving) return;
		checkpoint();
		options = {
			...options,
			[id]: tileOptions({ [id]: { ...tileOptions(options, id), [key]: value } }, id),
		};
		if (!editing) await finish();
	}
	const summary = (id: TileId) =>
		tileSummary(id, {
			options,
			data,
			info: system.info,
			storage: storage.status,
			dashboard: session.dashboard,
		});

	// Editor shortcuts: Ctrl/Cmd+Z undo, Ctrl/Cmd+Shift+Z or +Y redo, Ctrl/Cmd+S
	// save, Escape closes the side panel and then leaves the editor.
	function keys(e: KeyboardEvent) {
		if (!editing || saving || e.defaultPrevented || moving || confirmCancel || confirmRestore)
			return;
		const target = e.target instanceof HTMLElement ? e.target : null;
		if (target?.closest('[role="dialog"],dialog,[role="menu"]')) return;
		const key = e.key.toLowerCase();
		if (e.ctrlKey || e.metaKey) {
			if (key === 's') {
				e.preventDefault();
				void finish();
				return;
			}
			if (target?.closest('input,textarea,select')) return;
			if (key === 'z' || key === 'y') {
				e.preventDefault();
				step(e.shiftKey || key === 'y' ? 'redo' : 'undo');
			}
			return;
		}
		if (key !== 'escape' || target?.closest('input,textarea,select')) return;
		e.preventDefault();
		if (customize) customize = null;
		else if (adding) adding = false;
		else requestCancel();
	}
	function interceptLinks(element: HTMLElement, id: TileId) {
		const click = (event: MouseEvent) => {
			if (!editing || !(event.target instanceof Element) || !event.target.closest('a,button'))
				return;
			if (event.target.closest('.tile-head,.resize-handle')) return;
			event.stopImmediatePropagation();
			event.preventDefault();
			const selected = event.target.closest<HTMLElement>('[data-edit-part]');
			configure(id, (selected?.dataset.editPart as EditPart) || 'labels');
		};
		element.addEventListener('click', click, true);
		return () => element.removeEventListener('click', click, true);
	}

	function selectPart(e: PointerEvent, id: TileId) {
		if (!editing) return;
		const target = e.target as HTMLElement;
		if (target.closest('button,a,input,select')) return;
		const element = target.closest<HTMLElement>('[data-edit-part]');
		configure(id, (element?.dataset.editPart as EditPart) || 'tile');
	}
	const inspectorItems = $derived(
		customize ? itemsOf(customize, { data, info: system.info, storage: storage.status }) : [],
	);
</script>

<svelte:window onkeydown={keys} />
<NavigationGuard {dirty} />
<div class="dashboard-page" inert={saving}>
	{#if editing}<EditorToolbar
			{adding}
			{saving}
			{columns}
			{minimumWidth}
			{actualColumns}
			canUndo={undo.length > 0}
			canRedo={redo.length > 0}
			ontoggleadd={() => (adding ? (adding = false) : openAdd())}
			oncolumns={(value) => {
				checkpoint();
				columns = columnCount(value);
			}}
			onminimumwidth={(value) => {
				checkpoint();
				minimumWidth = columnWidth(value);
			}}
			onstep={step}
			ontemplate={() => (choosingTemplate = true)}
			onrestore={() => (confirmRestore = true)}
			oncancel={requestCancel}
			onsave={finish}
		/>{/if}
	{#if error}<div class="notice-error" role="alert">
			{error}<button class="btn" onclick={finish}>{t('ui.retry')}</button>
		</div>{/if}
	<div
		class="dashboard-workspace"
		class:editing
		class:with-inspector={editing && (!!customize || adding)}
	>
		<div class="area">
			<div class="dashboard-grid" {@attach attachGrid}>
				{#each mountedLayout as tile (tile.id)}
					{@const spec = tileSpec(tile.id)}{@const settings = tileOptions(options, tile.id)}
					<div
						class="grid-stack-item"
						data-position={tile.id}
						class:moving={moving === tile.id}
						gs-id={tile.id}
						gs-x={tile.x}
						gs-y={tile.y}
						gs-w={tile.w}
						gs-h={tile.h}
						gs-min-w={spec.minW}
						gs-min-h={spec.minH}
						gs-auto-position={tile.x === undefined ? 'true' : undefined}
					>
						<section
							class="tile dashboard-tile"
							style:min-height={tile.autoHeight === false && (!settings.collapsed || editing)
								? `${tile.height ?? tile.h * 40}px`
								: undefined}
							style:height={tile.autoHeight === false && (!settings.collapsed || editing)
								? `${tile.height ?? tile.h * 40}px`
								: undefined}
							{@attach (element) => interceptLinks(element, tile.id)}
							class:selected={editing && customize === tile.id}
							style={tileStyle(settings, tile.id)}
							aria-label={titleOf(tile.id)}
							onpointerdown={(e) => selectPart(e, tile.id)}
						>
							{#if settings.showTitle || settings.collapsed || editing}<TileHead
									title={titleOf(tile.id)}
									summary={summary(tile.id)}
									osIcon={osIconOf(tile.id)}
									{settings}
									{editing}
									locked={!!tile.locked}
									{saving}
									onpointerdown={(e) => headMove(e, tile.id)}
									onkeymove={(e) => keyboardMove(e, tile.id)}
									onlock={() => {
										checkpoint();
										rebuild(shown.map((p) => (p.id === tile.id ? { ...p, locked: !p.locked } : p)));
									}}
									onconfigure={() => configure(tile.id)}
									onhide={() => hide(tile.id)}
									oncollapse={() => runtimeOption(tile.id, 'collapsed', !settings.collapsed)}
								/>{/if}
							{#if !settings.collapsed || editing}<!-- svelte-ignore a11y_no_noninteractive_tabindex (fixed-height content is keyboard scrollable) -->
								<div
									class="tile-body"
									data-edit-part="labels"
									tabindex={tile.autoHeight === false ? 0 : undefined}
									role="region"
									aria-label={titleOf(tile.id)}
								>
									<TileContent
										id={tile.id}
										{settings}
										{data}
										{admin}
										{editing}
										{saving}
										onoption={(key, value) => runtimeOption(tile.id, key, value)}
										onpin={(id) => {
											if (!editing) startEditing();
											add(shown.some((p) => p.id === id) ? duplicateId(id) : id);
										}}
									/>
								</div>{/if}
							{#if editing}<button
									disabled={tile.locked}
									class="resize-handle"
									title={t('dashboard.editor.resizeHint')}
									aria-label={t('dashboard.resize', { tile: titleOf(tile.id) })}
									onpointerdown={(e) => pointerMove(e, tile.id, true)}
									><Icon name="resize" size={13} /></button
								>{/if}
						</section>
					</div>
				{/each}
				{#if editing && ghost}<div
						class="placement-placeholder"
						style:left={`${ghost.left}px`}
						style:top={`${ghost.top}px`}
						style:width={`${ghost.width}px`}
						style:height={`${ghost.height}px`}
					></div>{/if}
			</div>
		</div>
		{#if editing && adding}<AddTiles
				{hidden}
				{shown}
				{admin}
				{workloadGroups}
				bind:selection
				onadd={add}
				onclose={() => (adding = false)}
			/>{/if}
		{#if editing && customize && currentPlacement}<TileEditor
				id={customize}
				title={titleOf(customize)}
				settings={currentSettings}
				placement={currentPlacement}
				onauto={(value) => {
					if (currentPlacement?.locked) return;
					checkpoint();
					const current = document
						.querySelector<HTMLElement>(`[gs-id="${CSS.escape(customize!)}"] .dashboard-tile`)
						?.getBoundingClientRect().height;
					rebuild(
						shown.map((p) =>
							p.id === customize
								? {
										...p,
										autoHeight: value,
										height:
											!value && current
												? Math.max(80, Math.min(1600, Math.round(current / 8) * 8))
												: p.height,
										h: !value && current ? Math.max(2, Math.min(40, Math.ceil(current / 40))) : p.h,
									}
								: p,
						),
					);
				}}
				{part}
				networks={system.info?.networks?.map((n) => n.interface) ?? []}
				devices={system.info?.accelerators ?? []}
				items={inspectorItems}
				defaultItems={sourceId(customize) === 'temperatures'
					? temperatureSelection(session.dashboard).map((choice) => choice.id)
					: undefined}
				sensors={sourceId(customize) === 'cpu'
					? (system.info?.temperatures ?? []).map((s) => ({
							id: s.id,
							name: `${s.label} · ${s.chip}`,
						}))
					: []}
				onchange={changeOption}
				onsize={size}
				onduplicate={duplicate}
				onreset={resetTile}
				onclose={() => (customize = null)}
			/>{/if}
	</div>
	{#if confirmCancel}<EditorConfirm
			title={t('dashboard.editor.discardTitle')}
			text={t('dashboard.editor.discardText')}
			keep={t('dashboard.editor.keepEditing')}
			confirm={t('dashboard.editor.discard')}
			onconfirm={cancel}
			onclose={() => (confirmCancel = false)}
		/>{/if}
	{#if choosingTemplate}<TemplateDialog
			onpick={useTemplate}
			onclose={() => (choosingTemplate = false)}
		/>{/if}
	{#if confirmRestore}<EditorConfirm
			title={t('dashboard.restore')}
			text={t('dashboard.editor.restoreText')}
			keep={t('common.cancel')}
			confirm={t('dashboard.restore')}
			onconfirm={restoreDefaults}
			onclose={() => (confirmRestore = false)}
		/>{/if}
</div>

<style>
	.dashboard-workspace {
		display: flex;
		align-items: flex-start;
		gap: 22px;
		min-width: 0;
	}
	.area {
		flex: 1;
		min-width: 0;
	}
	.editing .dashboard-grid {
		background-image:
			linear-gradient(var(--border) 1px, transparent 1px),
			linear-gradient(90deg, var(--border) 1px, transparent 1px);
		background-size: 8px 8px;
		/* Dragging tiles must not select their text. */
		user-select: none;
	}
	.placement-placeholder {
		position: absolute;
		z-index: 3;
		pointer-events: none;
		border: 2px dashed var(--accent);
		background: color-mix(in srgb, var(--accent) 12%, transparent);
		border-radius: 4px;
	}
	.resize-handle:disabled {
		cursor: default;
		opacity: 0.35;
	}
	.dashboard-page {
		background: transparent;
		box-shadow: none;
	}
	.dashboard-grid {
		position: relative;
		min-width: 0;
	}
	.grid-stack-item {
		min-width: 0;
		position: relative;
	}
	.resize-handle {
		position: absolute;
		right: 2px;
		bottom: 2px;
		border: 0;
		background: transparent;
		color: var(--muted);
		cursor: nwse-resize;
		touch-action: none;
		padding: 3px;
	}
	.moving {
		outline: 2px dashed var(--accent);
	}
	.dashboard-tile {
		container-type: inline-size;
		padding: 0;
		/* The same surface as every tile in the UI (Settings › Appearance). */
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-tile);
		background: var(--panel-bg);
		box-shadow: var(--panel-shadow), var(--panel-glow, 0 0 0 transparent);
		-webkit-backdrop-filter: var(--panel-backdrop);
		backdrop-filter: var(--panel-backdrop);
		display: flex;
		flex-direction: column;
		gap: 0;
		overflow: hidden;
	}
	.dashboard-tile.selected {
		outline: 2px solid var(--accent);
		outline-offset: -1px;
	}
	/* Selected text in the editor would be dragged natively by the browser instead of the tile. */
	.editing .dashboard-grid {
		user-select: none;
	}
	.tile-body {
		min-height: 0;
		min-width: 0;
		flex: 1;
		overflow-y: auto;
		overflow-x: auto;
		scrollbar-gutter: auto;
		padding: var(--tile-padding, 12px);
		display: flex;
		flex-direction: column;
		gap: var(--tile-gap, 10px);
		font-size: var(--tile-label-size, var(--text-sm));
		font-family: var(--tile-label-font);
		font-weight: var(--tile-label-weight, 400);
		color: var(--tile-label-color, var(--muted));
		text-align: var(--tile-label-align, left);
	}
	.tile-body > :global(*) {
		flex-shrink: 0;
	}
	.tile-body :global(.value),
	.tile-body :global(.usage strong) {
		font-size: var(--tile-value-size, 2.25rem) !important;
		color: var(--tile-value-color, var(--text));
		font-family: var(--tile-value-font);
		font-weight: var(--tile-value-weight);
	}
	.tile-body :global(dt),
	.tile-body :global(.label),
	.tile-body :global(.small),
	.tile-body :global(.detail),
	.tile-body :global(.muted),
	.tile-body :global(.what),
	.tile-body :global(.state) {
		font-size: var(--tile-label-size, var(--text-sm));
		color: var(--tile-label-color, var(--muted));
	}
	.tile-body :global(.rates) {
		background: none;
	}
	.tile-body :global(dd) {
		font-size: var(--tile-label-size, var(--text-sm));
		color: var(--tile-value-color, var(--text));
		font-family: var(--tile-value-font);
		font-weight: var(--tile-value-weight);
	}
	.tile-body :global([data-edit-part='value']:not(.reading)) {
		font-size: var(--tile-value-size);
		font-family: var(--tile-value-font);
		font-weight: var(--tile-value-weight);
		color: var(--tile-value-color);
		text-align: var(--tile-align);
	}
	.editing .tile-body :global([data-edit-part]):hover {
		outline: 1px dashed var(--accent);
		outline-offset: 3px;
		cursor: pointer;
	}
	/* The editor bar of EditorToolbar.svelte pushes the sticky side panels down. */
	.dashboard-page:has(:global(.editbar)) {
		--dashboard-sticky-top: 132px;
	}
	@supports not (backdrop-filter: blur(1px)) {
		.dashboard-tile {
			background: var(--surface);
		}
	}
	@media (prefers-reduced-transparency: reduce) {
		.dashboard-tile {
			background: var(--surface);
			backdrop-filter: none;
			box-shadow: none;
		}
	}
	@media (max-width: 760px) {
		.dashboard-workspace {
			display: block;
		}
		.with-inspector .area {
			padding-bottom: 75vh;
		}
		.dashboard-tile {
			container-type: inline-size;
			max-width: 100%;
		}
		.tile-body :global(.reading) {
			max-width: 100%;
			overflow-wrap: anywhere;
		}
	}
	@media (prefers-reduced-transparency: reduce) {
		.dashboard-tile {
			container-type: inline-size;
			background: var(--surface);
		}
	}
</style>
