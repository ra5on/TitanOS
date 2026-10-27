<script lang="ts">
	import { tick, untrack, onDestroy, type Component } from 'svelte';
	import {
		api,
		ApiError,
		type VM,
		type VMStatus,
		type DockerContainer,
		type DockerStatus,
		type Share,
	} from '#lib/api.ts';
	import { t, errorText, type MessageKey } from '#lib/i18n/index.svelte.ts';

	import { session } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { dockerGroups } from '#lib/docker-layout.ts';
	import { tailnetHost, type TailnetStatus } from '#lib/app-links.ts';
	import { dashboardControl } from './control.svelte.ts';
	import { positionGrid } from './position-grid.svelte.ts';
	import { dropLayout, type Rect } from './positions.ts';
	import { formatBytes } from '#lib/format.ts';
	import { cpuTemperature, temperatureSelection, tileTemperatures } from './temperatures.ts';
	import type { TailnetDevices } from './tailscale.ts';
	import type { NetbirdPeers } from './netbird.ts';
	import Icon from '#lib/components/Icon.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import NavigationGuard from '#lib/operations/NavigationGuard.svelte';
	import {
		TILES,
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
	import {
		tileOptions,
		tileStyle,
		transferStyle,
		STYLE_KEYS,
		type TileOptions,
	} from './options.ts';
	import { flowLayout, columnCount, columnWidth, visibleColumns, legacyPositions } from './flow.ts';
	import GlobalEditor from './GlobalEditor.svelte';
	import { dashboardVisual, visualStyle, type DashboardVisual } from './visual.ts';
	import { dashboardScene } from './scene.svelte.ts';
	import TileEditor, { type EditPart } from './TileEditor.svelte';
	import UsageTile from './tiles/UsageTile.svelte';
	import AcceleratorTile from './tiles/AcceleratorTile.svelte';
	import WorkloadTile from './tiles/WorkloadTile.svelte';
	import CollectionTile from './tiles/CollectionTile.svelte';
	import SystemTile from './tiles/SystemTile.svelte';
	import NetworkTile from './tiles/NetworkTile.svelte';
	import DisksTile from './tiles/DisksTile.svelte';
	import VolumesTile from './tiles/VolumesTile.svelte';
	import TemperaturesTile from './tiles/TemperaturesTile.svelte';
	import AssistantTile from './tiles/AssistantTile.svelte';
	import TailscaleTile from './tiles/TailscaleTile.svelte';
	import NetbirdTile from './tiles/NetbirdTile.svelte';
	const CONTENT: Partial<Record<StaticTileId, Component<{ options?: TileOptions }>>> = {
		volumes: VolumesTile,
		disks: DisksTile,
		temperatures: TemperaturesTile,
		assistant: AssistantTile,
	};
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
	let galleryVisual = $state<DashboardVisual | null>(null),
		globalOpen = $state(false);
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
		confirmRestore = $state(false),
		transferOpen = $state(false),
		transferTargets = $state<TileId[]>([]);
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
			items: mountedLayout,
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
	let refreshSerial = $state(0),
		kvm = $state(false);
	let containers = $state<DockerContainer[]>([]),
		vms = $state<VM[]>([]),
		shares = $state<Share[]>([]),
		tailnet = $state<TailnetStatus | null>(null),
		tailnetDevices = $state<TailnetDevices | null>(null),
		tailnetDevicesError = $state(''),
		netbirdPeers = $state<NetbirdPeers | null>(null),
		netbirdPeersError = $state('');
	let containersReady = $state(false),
		vmsReady = $state(false),
		sharesReady = $state(false),
		dockerError = $state(''),
		vmError = $state(''),
		sharesError = $state('');
	const currentSettings = $derived(tileOptions(options, customize ?? 'cpu'));
	const currentPlacement = $derived(shown.find((p) => p.id === customize));
	const hidden = $derived(addableTiles(shown, { admin, hardware: hardwareKinds() }));
	// Stacks, VMs and single containers in their own groups; entries already on
	// the dashboard are marked, adding them again creates an independent copy.
	type WorkloadGroup = { label: MessageKey; choices: { id: TileId; name: string }[] };
	const workloadGroups = $derived(
		(
			[
				{
					label: 'docker.stack',
					choices: dockerGroups(containers)
						.filter((g) => g.id.startsWith('app:'))
						.map((g) => ({ id: g.id as TileId, name: g.name })),
				},
				{
					label: 'nav.vms',
					choices: vms.map((vm) => ({ id: `vm:${vm.name}` as TileId, name: vmLabel(vm) })),
				},
				{
					label: 'tile.container',
					choices: containers.map((c) => ({
						id: `container:${c.name}` as TileId,
						name: dockerName(c, 'distinct'),
					})),
				},
			] satisfies WorkloadGroup[]
		).filter((group) => group.choices.length),
	);
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
	$effect(() => {
		if (!watched) return;
		void refreshSerial;
		let disposed = false,
			inflight = false;
		async function refresh() {
			if (inflight || document.hidden) return;
			inflight = true;
			const results = await Promise.allSettled([
				api.get<VMStatus>('/vms'),
				api.get<DockerStatus>('/docker'),
				api.get<TailnetStatus>('/plugins/tailscale'),
			]);
			if (!disposed) {
				const [machines, docker, tailscale] = results;
				if (machines.status === 'fulfilled') {
					vms = machines.value.vms;
					kvm = machines.value.kvm;
					vmsReady = true;
					vmError = '';
				} else vmError = fail(machines.reason);
				if (docker.status === 'fulfilled') {
					containers = docker.value.containers;
					containersReady = true;
					dockerError =
						docker.value.settings.enabled && !docker.value.running
							? t('dashboard.engineUnavailable')
							: '';
				} else dockerError = fail(docker.reason);
				if (tailscale.status === 'fulfilled') tailnet = tailscale.value;
				else tailnet = null;
			}
			inflight = false;
		}
		void refresh();
		const timer = setInterval(refresh, 5000);
		return () => {
			disposed = true;
			clearInterval(timer);
		};
	});
	// Tailnet devices only while a Tailscale tile is shown or being edited;
	// the server answers from a short RAM cache.
	const watchesTailnet = $derived(
		admin && (editing || shown.some((p) => sourceId(p.id) === 'tailscale')),
	);
	$effect(() => {
		if (!watchesTailnet) return;
		void refreshSerial;
		let disposed = false,
			inflight = false;
		async function refresh() {
			if (inflight || document.hidden) return;
			inflight = true;
			try {
				const value = await api.get<TailnetDevices>('/plugins/tailscale/devices');
				if (!disposed) {
					tailnetDevices = value;
					tailnetDevicesError = '';
				}
			} catch (e) {
				if (!disposed) tailnetDevicesError = fail(e);
			} finally {
				inflight = false;
			}
		}
		void refresh();
		const timer = setInterval(refresh, 15000);
		return () => {
			disposed = true;
			clearInterval(timer);
		};
	});
	// NetBird peers only while a NetBird tile is shown or being edited;
	// the server answers from a short RAM cache.
	const watchesNetbird = $derived(
		admin && (editing || shown.some((p) => sourceId(p.id) === 'netbird')),
	);
	$effect(() => {
		if (!watchesNetbird) return;
		void refreshSerial;
		let disposed = false,
			inflight = false;
		async function refresh() {
			if (inflight || document.hidden) return;
			inflight = true;
			try {
				const value = await api.get<NetbirdPeers>('/plugins/netbird/peers');
				if (!disposed) {
					netbirdPeers = value;
					netbirdPeersError = '';
				}
			} catch (e) {
				if (!disposed) netbirdPeersError = fail(e);
			} finally {
				inflight = false;
			}
		}
		void refresh();
		const timer = setInterval(refresh, 15000);
		return () => {
			disposed = true;
			clearInterval(timer);
		};
	});
	const watchesShares = $derived(editing || shown.some((p) => sourceId(p.id) === 'shares'));
	$effect(() => {
		if (!watchesShares) return;
		void refreshSerial;
		let disposed = false,
			inflight = false;
		async function refresh() {
			if (inflight || document.hidden) return;
			inflight = true;
			try {
				const value = await api.get<Share[]>('/shares');
				if (!disposed) {
					shares = value;
					sharesReady = true;
					sharesError = '';
				}
			} catch (e) {
				if (!disposed) sharesError = fail(e);
			} finally {
				inflight = false;
			}
		}
		void refresh();
		const timer = setInterval(refresh, 15000);
		return () => {
			disposed = true;
			clearInterval(timer);
		};
	});
	let initializedHardware = false;
	$effect(() => {
		if (initializedHardware || editing || !system.info) return;
		initializedHardware = true;
		if (session.dashboard?.tiles) return;
		const next = untrack(defaults);
		saved = next;
		rebuild(next);
	});
	function fail(e: unknown) {
		return errorText(e instanceof ApiError ? e.code : 'internal');
	}
	function titleOf(id: TileId) {
		const o = tileOptions(options, id);
		if (o.title) return o.title;
		const target = workload(id);
		if (target) {
			const vm = target.kind === 'vm' ? vms.find((v) => v.name === target.name) : undefined;
			const container =
				target.kind === 'container' ? containers.find((v) => v.name === target.name) : undefined;
			return vm ? vmLabel(vm) : container ? dockerName(container, 'distinct') : target.name;
		}
		return t(tileSpec(id).title);
	}
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
		globalOpen = true;
		adding = false;
	}
	// Leaving the editor with unsaved changes asks first.
	function requestCancel() {
		if (dirty) confirmCancel = true;
		else cancel();
	}
	function openAdd() {
		customize = null;
		globalOpen = false;
		adding = true;
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
		globalOpen = false;
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
			globalOpen = false;
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
			error = fail(e);
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
	function transfer() {
		if (!customize) return;
		checkpoint();
		const next = { ...options };
		for (const id of transferTargets)
			next[id] = transferStyle(currentSettings, tileOptions(options, id));
		options = next;
		transferOpen = false;
	}
	function moveTile(id: TileId, to: number) {
		const layout = [...shown],
			from = layout.findIndex((p) => p.id === id);
		if (from < 0 || to < 0 || to >= layout.length || from === to) return;
		checkpoint();
		const [item] = layout.splice(from, 1);
		layout.splice(to, 0, item);
		rebuild(layout.map((p) => ({ ...p, column: undefined, offset: undefined })));
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
		} else moveTile(id, shown.findIndex((p) => p.id === id) + direction);
		await tick();
		document
			.querySelector<HTMLButtonElement>(`.grid-stack-item[gs-id="${CSS.escape(id)}"] .grip`)
			?.focus({ preventScroll: true });
	}
	function pointerMove(event: PointerEvent, id: TileId, resize = false) {
		const current = shown.find((p) => p.id === id)!;
		if (!editing || saving || current.locked || event.button !== 0 || !board) return;
		const handle = event.currentTarget as HTMLElement;
		const start = { x: event.clientX, y: event.clientY },
			r = boardRects[id];
		const cell = (availableWidth + 12) / actualColumns;
		const original = structuredClone($state.snapshot(shown));
		const base = Object.fromEntries(Object.entries(boardRects).map(([id, r]) => [id, { ...r }]));
		const history = snapshot();
		const explicit = actualColumns === columns;
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
			handle.removeEventListener('pointermove', move);
			handle.removeEventListener('pointerup', end);
			handle.removeEventListener('pointercancel', abort);
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
		handle.addEventListener('pointermove', move);
		handle.addEventListener('pointerup', end);
		handle.addEventListener('pointercancel', abort);
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
	function summary(id: TileId) {
		const kind = sourceId(id),
			info = system.info;
		if (kind === 'system') return info ? t('dashboard.online') : '–';
		if (kind === 'docker' || kind.startsWith('app:')) {
			if (!containersReady || dockerError) return '–';
			const cs = kind.startsWith('app:')
				? containers.filter((c) => c.app === kind.slice(4))
				: containers;
			const gs = dockerGroups(cs);
			return t('dashboard.appSummary', {
				count: gs.length,
				running: gs.filter((g) => g.members.some((c) => c.state === 'running')).length,
			});
		}
		if (kind === 'vms')
			return vmsReady && !vmError
				? t('dashboard.runningSummary', {
						running: vms.filter((v) => v.state === 'running').length,
						count: vms.length,
					})
				: '–';
		if (kind === 'cpu') {
			if (!tileOptions(options, id).temperature) return '';
			const temp = cpuTemperature(info?.temperatures, tileOptions(options, id).sensor);
			return temp?.celsius == null ? '' : `${temp.celsius.toFixed(0)} °C`;
		}
		if (kind === 'memory') return info ? formatBytes(info.memory.totalBytes) : '–';
		if (kind === 'volumes')
			return storage.status
				? t('dashboard.mountedSummary', {
						count: storage.status.volumes.filter((v) => v.mounted).length,
					})
				: '–';
		if (kind === 'shares')
			return sharesReady && !sharesError
				? t('dashboard.shareSummary', { count: shares.length })
				: '–';
		if (kind === 'disks')
			return storage.status
				? t('dashboard.deviceSummary', {
						count: storage.status.disks.filter((d) => d.role !== 'stick').length,
					})
				: '–';
		if (['accelerators', 'igpu', 'nvidia', 'npu'].includes(kind))
			return info
				? t('dashboard.deviceSummary', {
						count: info.accelerators.filter(
							(d) =>
								kind === 'accelerators' ||
								(kind === 'npu'
									? d.kind === 'npu'
									: kind === 'nvidia'
										? d.vendor === 'NVIDIA' && d.kind !== 'npu'
										: d.kind !== 'npu' && d.vendor !== 'NVIDIA'),
						).length,
					})
				: '–';
		if (kind === 'netbird') {
			if (!netbirdPeers || netbirdPeersError) return '–';
			if (!netbirdPeers.active) return t('netbird.states.inactive');
			const o = tileOptions(options, id),
				list = netbirdPeers.peers.filter((p) => o.items === null || o.items.includes(p.id));
			return t('netbird.peer.summary', {
				online: list.filter((p) => p.online === true).length,
				count: list.length,
			});
		}
		if (kind === 'tailscale') {
			if (!tailnetDevices || tailnetDevicesError) return '–';
			if (!tailnetDevices.active) return t('tailscale.states.inactive');
			const o = tileOptions(options, id),
				list = tailnetDevices.devices.filter((d) => o.items === null || o.items.includes(d.id));
			return t('tailscale.device.summary', {
				online: list.filter((d) => d.online === true).length,
				count: list.length,
			});
		}
		if (kind === 'temperatures')
			return t('dashboard.sensorSummary', {
				count: tileTemperatures(
					session.dashboard,
					tileOptions(options, id).items,
					info?.temperatures ?? [],
				).length,
			});
		if (kind === 'network') {
			const o = tileOptions(options, id),
				n = o.interface ? info?.networks.find((n) => n.interface === o.interface) : info?.network;
			return n
				? `${n.interface || '–'}${n.speedMbps > 0 ? ' · ' + n.speedMbps + ' Mbit/s' : ''}`
				: '–';
		}
		return '';
	}

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
		else if (globalOpen) globalOpen = false;
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
	const inspectorItems = $derived.by(() => {
		if (!customize) return [];
		switch (sourceId(customize)) {
			case 'docker':
				return dockerGroups(containers).map((g) => ({ id: g.id, name: g.name }));
			case 'vms':
				return vms.map((v) => ({ id: v.name, name: vmLabel(v) }));
			case 'shares':
				return shares.map((s) => ({ id: s.name, name: s.name }));
			case 'volumes':
				return (storage.status?.volumes ?? []).map((v) => ({ id: v.name, name: v.name }));
			case 'netbird':
				return (netbirdPeers?.peers ?? []).map((p) => ({
					id: p.id,
					name: p.self ? `${p.name} · ${t('netbird.peer.self')}` : p.name || p.fqdn,
				}));
			case 'tailscale':
				return (tailnetDevices?.devices ?? []).map((d) => ({
					id: d.id,
					name: d.self ? `${d.name} · ${t('tailscale.device.self')}` : d.name || d.hostName,
				}));
			case 'temperatures':
				return (system.info?.temperatures ?? []).map((s) => ({
					id: s.id,
					name: `${s.label} · ${s.chip}`,
				}));
			case 'disks':
				return (storage.status?.disks ?? [])
					.filter((d) => d.role !== 'stick')
					.map((d) => ({ id: d.identity || d.serial || d.name, name: d.name }));
			default:
				return [];
		}
	});
</script>

<svelte:window onkeydown={keys} />
<NavigationGuard {dirty} />
<div class="dashboard-page" inert={saving} style={visualStyle(effectiveVisual)}>
	{#if editing}<div class="editbar" role="toolbar" aria-label={t('dashboard.editor.toolbar')}>
			<div class="flow-controls">
				<button
					class="btn"
					aria-pressed={adding}
					disabled={saving}
					onclick={() => (adding ? (adding = false) : openAdd())}
					><Icon name="plus" size={16} />{t('dashboard.hidden')}</button
				><button
					class="btn"
					aria-pressed={globalOpen && !customize && !adding}
					onclick={() => {
						globalOpen = true;
						customize = null;
						adding = false;
					}}><Icon name="palette" size={16} />{t('dashboard.visual.all')}</button
				>
				<label
					>{t('dashboard.columns')}<select
						class="field"
						value={columns}
						onchange={(e) => {
							checkpoint();
							columns = columnCount(Number(e.currentTarget.value));
						}}
						>{#each [1, 2, 3, 4] as n}<option value={n}>{n}</option>{/each}</select
					></label
				><label
					>{t('dashboard.minimumWidth')}<input
						class="field"
						type="number"
						min="280"
						max="700"
						step="10"
						value={minimumWidth}
						onchange={(e) => {
							checkpoint();
							minimumWidth = columnWidth(Number(e.currentTarget.value));
						}}
					/></label
				>{#if actualColumns !== columns}<span
						>{t('dashboard.visibleColumns', { count: actualColumns })}</span
					>{/if}
			</div>
			<div class="editor-actions">
				<button
					class="btn icon-btn"
					disabled={!undo.length || saving}
					title={`${t('dashboard.editor.undo')} (Strg+Z)`}
					aria-label={t('dashboard.editor.undo')}
					onclick={() => step('undo')}><Icon name="undo" size={15} /></button
				><button
					class="btn icon-btn"
					disabled={!redo.length || saving}
					title={`${t('dashboard.editor.redo')} (Strg+Y)`}
					aria-label={t('dashboard.editor.redo')}
					onclick={() => step('redo')}><Icon name="redo" size={15} /></button
				><button class="btn" disabled={saving} onclick={() => (confirmRestore = true)}
					>{t('dashboard.restore')}</button
				><button class="btn" disabled={saving} onclick={requestCancel}>{t('common.cancel')}</button
				><button
					class="btn btn-primary"
					disabled={saving}
					title={`${t('dashboard.editor.save')} (Strg+S)`}
					onclick={finish}
					>{saving ? t('dashboard.editor.saving') : t('dashboard.editor.save')}</button
				>
			</div>
		</div>{/if}
	{#if error}<div class="notice-error" role="alert">
			{error}<button class="btn" onclick={finish}>{t('ui.retry')}</button>
		</div>{/if}
	<div
		class="dashboard-workspace"
		class:editing
		class:with-inspector={editing && (!!customize || globalOpen || adding)}
	>
		<div class="area">
			<div class="dashboard-grid" {@attach attachGrid}>
				{#each mountedLayout as tile (tile.id)}
					{@const source = sourceId(tile.id)}{@const target = workload(tile.id)}{@const spec =
						tileSpec(tile.id)}{@const settings = tileOptions(options, tile.id)}
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
							style={tileStyle(settings)}
							aria-label={titleOf(tile.id)}
							onpointerdown={(e) => selectPart(e, tile.id)}
						>
							{#if settings.showTitle || settings.collapsed || editing}<div class="tile-head">
									{#if editing}<button
											disabled={tile.locked}
											class="grip"
											title={t('dashboard.editor.moveHint')}
											aria-label={t('dashboard.move', { tile: titleOf(tile.id) })}
											onkeydown={(e) => keyboardMove(e, tile.id)}
											onpointerdown={(e) => pointerMove(e, tile.id)}
											><Icon name="grip" size={16} /></button
										>{/if}
									{#if settings.showIcon}<span class="tile-symbol"
											><Icon name={settings.icon} size={settings.titleSize + 3} /></span
										>{/if}
									<h2 class="tile-title" data-edit-part="heading">
										{settings.showTitle ? titleOf(tile.id) : ''}
									</h2>
									{#if settings.showSummary}<span class="tile-summary">{summary(tile.id)}</span
										>{/if}
									{#if editing}<button
											class="tile-action"
											title={t(tile.locked ? 'dashboard.unlockPosition' : 'dashboard.lockPosition')}
											aria-label={t('dashboard.lockPosition')}
											aria-pressed={!!tile.locked}
											onclick={() => {
												checkpoint();
												rebuild(
													shown.map((p) => (p.id === tile.id ? { ...p, locked: !p.locked } : p)),
												);
											}}><Icon name={tile.locked ? 'lock' : 'unlock'} size={14} /></button
										><button
											class="tile-action"
											title={t('dashboard.tileOptions')}
											aria-label={`${t('dashboard.tileOptions')}: ${titleOf(tile.id)}`}
											onclick={() => configure(tile.id)}><Icon name="edit" size={14} /></button
										><button
											class="tile-action"
											title={t('dashboard.hide', { tile: titleOf(tile.id) })}
											aria-label={t('dashboard.hide', { tile: titleOf(tile.id) })}
											onclick={() => hide(tile.id)}><Icon name="close" size={16} /></button
										>{:else}<button
											class="tile-action collapse"
											class:collapsed={settings.collapsed}
											disabled={saving}
											title={t(
												settings.collapsed ? 'dashboard.expandTile' : 'dashboard.collapseTile',
												{ name: titleOf(tile.id) },
											)}
											aria-label={t(
												settings.collapsed ? 'dashboard.expandTile' : 'dashboard.collapseTile',
												{ name: titleOf(tile.id) },
											)}
											aria-expanded={!settings.collapsed}
											onclick={() => runtimeOption(tile.id, 'collapsed', !settings.collapsed)}
											><Icon name="chevronDown" size={13} /></button
										>{/if}
								</div>{/if}
							{#if !settings.collapsed || editing}<!-- svelte-ignore a11y_no_noninteractive_tabindex (fixed-height content is keyboard scrollable) -->
								<div
									class="tile-body"
									data-edit-part="labels"
									tabindex={tile.autoHeight === false ? 0 : undefined}
									role="region"
									aria-label={titleOf(tile.id)}
								>
									{#if target?.kind === 'app'}<CollectionTile
											kind="docker"
											{kvm}
											onoption={(key, value) => runtimeOption(tile.id, key, value)}
											disabled={saving}
											options={settings}
											containers={containers.filter((c) => c.app === target.name)}
											ready={containersReady}
											error={dockerError}
											{admin}
											tailscaleHost={tailnetHost(tailnet)}
											{editing}
											onrefresh={() => refreshSerial++}
											onpin={(id) => {
												if (!editing) startEditing();
												add(shown.some((p) => p.id === id) ? duplicateId(id) : id);
											}}
										/>
									{:else if target}<WorkloadTile
											kind={target.kind}
											name={target.name}
											options={settings}
											{editing}
											{kvm}
											tailscaleHost={tailnetHost(tailnet)}
											vm={vms.find((v) => v.name === target.name)}
											container={containers.find((c) => c.name === target.name)}
											onrefresh={() => refreshSerial++}
											ready={target.kind === 'vm' ? vmsReady : containersReady}
											error={target.kind === 'vm' ? vmError : dockerError}
											{admin}
										/>
									{:else if source === 'cpu' || source === 'memory'}<UsageTile
											kind={source}
											options={settings}
											ondetails={() => runtimeOption(tile.id, 'details', !settings.details)}
										/>
									{:else if source === 'network'}<NetworkTile options={settings} />
									{:else if source === 'netbird'}<NetbirdTile
											options={settings}
											value={netbirdPeers}
											error={netbirdPeersError}
										/>
									{:else if source === 'tailscale'}<TailscaleTile
											options={settings}
											value={tailnetDevices}
											error={tailnetDevicesError}
										/>
									{:else if source === 'system'}<SystemTile options={settings} />
									{:else if source === 'igpu' || source === 'npu' || source === 'nvidia' || source === 'accelerators'}<AcceleratorTile
											kind={source}
											options={settings}
										/>
									{:else if source === 'docker' || source === 'vms' || source === 'shares'}<CollectionTile
											kind={source}
											{kvm}
											onoption={(key, value) => runtimeOption(tile.id, key, value)}
											disabled={saving}
											options={settings}
											{containers}
											{vms}
											{shares}
											ready={source === 'docker'
												? containersReady
												: source === 'vms'
													? vmsReady
													: sharesReady}
											error={source === 'docker'
												? dockerError
												: source === 'vms'
													? vmError
													: sharesError}
											{admin}
											tailscaleHost={tailnetHost(tailnet)}
											{editing}
											onrefresh={() => refreshSerial++}
											onpin={(id) => {
												if (!editing) startEditing();
												add(shown.some((p) => p.id === id) ? duplicateId(id) : id);
											}}
										/>
									{:else}{@const Content = CONTENT[source as StaticTileId]}{#if Content}<Content
												options={settings}
											/>{/if}{/if}
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
		{#if editing && adding}<aside class="tile add-tiles" aria-label={t('dashboard.hidden')}>
				<div class="add-head">
					<h2 class="tile-title">{t('dashboard.hidden')}</h2>
					<button
						class="tile-action"
						title={t('common.close')}
						aria-label={t('common.close')}
						onclick={() => (adding = false)}><Icon name="close" size={16} /></button
					>
				</div>
				{#if hidden.length}<div class="tile-choices">
						{#each hidden as id}<button
								class="btn"
								disabled={shown.length >= 100}
								onclick={() => add(id)}><Icon name="plus" size={14} />{t(TILES[id].title)}</button
							>{/each}
					</div>{:else}<p class="muted">{t('dashboard.editor.allAdded')}</p>{/if}
				{#if admin}<label
						>{t('dashboard.workload')}<select class="field" bind:value={selection}
							><option value="">{t('dashboard.pickWorkload')}</option
							>{#each workloadGroups as group}<optgroup label={t(group.label)}
									>{#each group.choices as choice}<option value={choice.id}
											>{choice.name}{shown.some((p) => sourceId(p.id) === choice.id)
												? ` · ${t('dashboard.onDashboard')}`
												: ''}</option
										>{/each}</optgroup
								>{/each}</select
						></label
					><button
						class="btn"
						disabled={!selection || shown.length >= 100}
						onclick={() =>
							add(
								shown.some((p) => p.id === selection)
									? duplicateId(selection as TileId)
									: (selection as TileId),
							)}>{t('dashboard.addWorkload')}</button
					>{/if}
				<p class="muted">{t('dashboard.hiddenHint')}</p>
			</aside>{/if}
		{#if editing && globalOpen && !customize && !adding}<GlobalEditor
				value={visual}
				urls={dashboardScene.urls}
				onbegin={checkpoint}
				onchange={(v) => (visual = v)}
				onpreview={(v) => {
					galleryVisual = v;
					if (!v) pruneImages();
				}}
				onupload={registerImage}
				onclose={() => (globalOpen = false)}
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
				ontransfer={() => {
					transferTargets = [];
					transferOpen = true;
				}}
				onclose={() => (customize = null)}
			/>{/if}
	</div>
	{#if transferOpen}<Dialog
			title={t('dashboard.editor.transfer')}
			onclose={() => (transferOpen = false)}
			><div class="transfer-targets">
				{#each shown.filter((p) => p.id !== customize) as tile}<label
						><input
							type="checkbox"
							checked={transferTargets.includes(tile.id)}
							onchange={() =>
								(transferTargets = transferTargets.includes(tile.id)
									? transferTargets.filter((id) => id !== tile.id)
									: [...transferTargets, tile.id])}
						/>{titleOf(tile.id)}</label
					>{/each}
			</div>
			<div class="dialog-actions">
				<button class="btn" onclick={() => (transferOpen = false)}>{t('common.cancel')}</button
				><button class="btn btn-primary" disabled={!transferTargets.length} onclick={transfer}
					>{t('common.done')}</button
				>
			</div></Dialog
		>{/if}
	{#if confirmCancel}<Dialog
			title={t('dashboard.editor.discardTitle')}
			onclose={() => (confirmCancel = false)}
			><p>{t('dashboard.editor.discardText')}</p>
			<div class="dialog-actions">
				<button class="btn" onclick={() => (confirmCancel = false)}
					>{t('dashboard.editor.keepEditing')}</button
				><button type="button" class="btn btn-warn" onclick={cancel}
					>{t('dashboard.editor.discard')}</button
				>
			</div></Dialog
		>{/if}
	{#if confirmRestore}<Dialog
			title={t('dashboard.restore')}
			onclose={() => (confirmRestore = false)}
			><p>{t('dashboard.editor.restoreText')}</p>
			<div class="dialog-actions">
				<button class="btn" onclick={() => (confirmRestore = false)}>{t('common.cancel')}</button
				><button type="button" class="btn btn-warn" onclick={restoreDefaults}
					>{t('dashboard.restore')}</button
				>
			</div></Dialog
		>{/if}
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
	.editor-actions {
		display: flex;
		justify-content: flex-end;
		flex-wrap: wrap;
		gap: 8px;
	}
	.icon-btn {
		padding-inline: 9px;
	}
	.collapse :global(svg) {
		transform: rotate(180deg);
	}
	.collapse.collapsed :global(svg) {
		transform: none;
	}
	.tile-summary {
		color: var(--muted);
		font-size: 11px;
		min-width: 0;
		overflow-wrap: anywhere;
		text-align: right;
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
	.grip:disabled,
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
	.flow-controls {
		display: flex;
		gap: 14px;
		flex-wrap: wrap;
		align-items: center;
	}
	.flow-controls label {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.flow-controls .field {
		width: 80px;
		min-height: 32px;
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
		border: var(--tile-border, 1px) solid var(--tile-border-color, var(--border));
		border-radius: var(--tile-radius, 12px);
		background: var(--tile-background, var(--surface));
		box-shadow:
			var(--tile-shadow, none),
			0 0 24px
				color-mix(
					in srgb,
					var(--dashboard-glow-color, var(--accent)) calc(var(--tile-glow, 0) * 100%),
					transparent
				);
		backdrop-filter: blur(var(--tile-blur, 0px)) saturate(115%);
		display: flex;
		flex-direction: column;
		gap: 0;
		overflow: hidden;
	}
	.dashboard-tile.selected {
		outline: 2px solid var(--accent);
		outline-offset: -1px;
	}
	.tile-head {
		display: flex;
		align-items: center;
		gap: 8px;
		flex: none;
		min-height: 35px;
		padding: 5px 10px;
		border-bottom: 1px solid var(--border);
		background: color-mix(in srgb, var(--text) 3%, transparent);
	}
	.tile-title {
		flex: 1;
		min-width: 0;
		font-size: var(--tile-title-size, 13px);
		font-weight: var(--tile-title-weight, 500);
		font-family: var(--tile-title-font);
		color: var(--tile-title-color, var(--text));
		text-align: var(--tile-title-align, left);
		line-height: 1.3;
		letter-spacing: 0;
		text-transform: uppercase;
		overflow-wrap: anywhere;
	}
	.tile-symbol {
		color: var(--accent);
	}
	.grip,
	.tile-action {
		border: 0;
		background: transparent;
		color: var(--muted);
		padding: 3px;
		cursor: pointer;
		display: flex;
		align-items: center;
		flex: none;
	}
	.grip {
		cursor: grab;
		touch-action: none;
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
		font-size: var(--tile-label-size, 12px);
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
		font-size: var(--tile-value-size, 36px) !important;
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
		font-size: var(--tile-label-size, 12px);
		color: var(--tile-label-color, var(--muted));
	}
	.tile-body :global(.rates) {
		background: none;
	}
	.tile-body :global(dd) {
		font-size: var(--tile-label-size, 12px);
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
	/* One bar for all editor controls; it stays below the header while scrolling. */
	.editbar {
		position: sticky;
		top: 60px;
		z-index: 15;
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
		border: 1px solid var(--border);
		background: var(--surface);
		border-radius: var(--radius-ctl);
		padding: 8px 12px;
		margin-bottom: 12px;
		font-size: 0.8rem;
		color: var(--muted);
	}
	.dashboard-page:has(.editbar) {
		--dashboard-sticky-top: 132px;
	}
	.add-tiles {
		position: sticky;
		top: var(--dashboard-sticky-top, 76px);
		width: 320px;
		flex: none;
		max-height: calc(100vh - var(--dashboard-sticky-top, 76px) - 19px);
		overflow: auto;
		box-sizing: border-box;
		padding: 18px;
		display: flex;
		flex-direction: column;
		gap: 15px;
		background: var(--surface);
	}
	.add-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}
	.tile-choices {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.add-tiles label {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.add-tiles select {
		width: auto;
		max-width: 100%;
	}
	.add-tiles p {
		font-size: 0.8rem;
	}
	.transfer-targets {
		display: grid;
		gap: 14px;
	}
	.transfer-targets label {
		display: flex;
		align-items: center;
		gap: 8px;
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
		.editbar {
			font-size: 0.75rem;
		}
		.add-tiles {
			position: fixed;
			inset: auto 10px 12px;
			top: auto;
			width: auto;
			max-height: 75vh;
			z-index: 40;
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
