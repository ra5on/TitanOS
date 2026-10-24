<script lang="ts">
	import { tick, untrack, type Component } from 'svelte';
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
	import { formatDuration } from '#lib/format.ts';
	import { session } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { dockerGroups } from '#lib/docker-layout.ts';
	import { tailnetHost, type TailnetStatus } from '#lib/app-links.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import NavigationGuard from '#lib/operations/NavigationGuard.svelte';
	import {
		TILE_IDS,
		TILES,
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
			.filter((p) => admin || !['docker', 'vms'].includes(sourceId(p.id)));
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
	let editing = $state(false),
		saving = $state(false),
		error = $state(''),
		revision = $state(0),
		customize = $state<TileId | null>(null),
		part = $state<EditPart>('tile'),
		selection = $state('');
	let undo = $state<string[]>([]),
		redo = $state<string[]>([]),
		transferOpen = $state(false),
		transferTargets = $state<TileId[]>([]);
	let columns = $state(columnCount(session.dashboard?.columns)),
		savedColumns = $state(columnCount(session.dashboard?.columns));
	let minimumWidth = $state(columnWidth(session.dashboard?.minimumWidth)),
		savedMinimumWidth = $state(columnWidth(session.dashboard?.minimumWidth));
	let availableWidth = $state(1200);
	const actualColumns = $derived(visibleColumns(availableWidth, columns, minimumWidth));
	let moving = $state<TileId | null>(null);
	function attachGrid(element: HTMLElement) {
		let frame = 0;
		const observer = new ResizeObserver((entries) => {
			const width = Math.floor(entries[0].contentRect.width);
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => {
				if (availableWidth !== width) availableWidth = width;
			});
		});
		observer.observe(element);
		return () => {
			observer.disconnect();
			cancelAnimationFrame(frame);
		};
	}
	let refreshSerial = $state(0);
	let containers = $state<DockerContainer[]>([]),
		vms = $state<VM[]>([]),
		shares = $state<Share[]>([]),
		tailnet = $state<TailnetStatus | null>(null);
	let containersReady = $state(false),
		vmsReady = $state(false),
		sharesReady = $state(false),
		dockerError = $state(''),
		vmError = $state(''),
		sharesError = $state('');
	const currentSettings = $derived(tileOptions(options, customize ?? 'cpu'));
	const currentPlacement = $derived(shown.find((p) => p.id === customize));
	const hidden = $derived(
		TILE_IDS.filter(
			(id) =>
				!shown.some((p) => sourceId(p.id) === id) && (admin || !['docker', 'vms'].includes(id)),
		),
	);
	const dirty = $derived(
		editing &&
			(JSON.stringify(shown) !== JSON.stringify(saved) ||
				JSON.stringify(options) !== JSON.stringify(savedOptions) ||
				columns !== savedColumns ||
				minimumWidth !== savedMinimumWidth),
	);
	const subtitle = $derived(
		system.info
			? `${system.info.hostname} · ${t('dashboard.uptime', { uptime: formatDuration(system.info.uptimeSeconds) })} · TitanOS ${system.info.version}`
			: '',
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
	const watchesShares = $derived(editing || shown.some((p) => sourceId(p.id) === 'shares'));
	$effect(() => {
		if (!watchesShares) return;
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
		revision++;
	}
	function restore(value: string) {
		const state = JSON.parse(value);
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
	}
	function cancel() {
		columns = savedColumns;
		minimumWidth = savedMinimumWidth;
		editing = false;
		customize = null;
		options = structuredClone($state.snapshot(savedOptions));
		rebuild(structuredClone($state.snapshot(saved)));
	}
	async function finish() {
		saving = true;
		error = '';
		const layout = legacyPositions(readGrid(), columns);
		const allOptions = Object.fromEntries(layout.map((p) => [p.id, tileOptions(options, p.id)]));
		const preferences = {
			...session.dashboard,
			version: 5,
			columns,
			minimumWidth,
			tiles: layout,
			tileOptions: allOptions,
		};
		try {
			await api.put('/preferences/dashboard', preferences);
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
	}
	function changeOption(key: keyof TileOptions, value: unknown) {
		if (!customize) return;
		checkpoint();
		options = {
			...options,
			[customize]: tileOptions({ [customize]: { ...currentSettings, [key]: value } }, customize),
		};
	}
	function size(w: number, h: number, autoHeight?: boolean) {
		if (!customize) return;
		checkpoint();
		rebuild(
			sanitizeLayout({
				tiles: readGrid().map((p) =>
					p.id === customize
						? {
								...p,
								span: Math.max(1, Math.min(4, w)),
								h,
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
		rebuild([...readGrid(), { ...currentPlacement!, id, x: undefined, y: undefined }]);
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
		customize = id;
		part = 'tile';
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
		rebuild(layout);
	}
	async function keyboardMove(e: KeyboardEvent, id: TileId) {
		if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return;
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
		if (!editing || event.button !== 0) return;
		const handle = event.currentTarget as HTMLElement;
		handle.setPointerCapture(event.pointerId);
		const x = event.clientX,
			y = event.clientY,
			current = shown.find((p) => p.id === id)!;
		let target = id;
		moving = id;
		const move = (e: PointerEvent) => {
			if (resize) return;
			const hit = document
				.elementsFromPoint(e.clientX, e.clientY)
				.map((el) => el.closest<HTMLElement>('[gs-id]'))
				.find(Boolean);
			if (hit) target = hit.getAttribute('gs-id') as TileId;
		};
		const baseHeight = current.autoHeight === false ? current.h : heightRows(current);
		const end = (e: PointerEvent) => {
			handle.removeEventListener('pointermove', move);
			handle.removeEventListener('pointerup', end);
			handle.removeEventListener('pointercancel', cancelMove);
			moving = null;
			if (resize) {
				checkpoint();
				const dx = Math.round((e.clientX - x) / (availableWidth / actualColumns));
				const span = dx
					? Math.max(1, Math.min(4, Math.min(current.span ?? 1, actualColumns) + dx))
					: (current.span ?? 1);
				const h = Math.max(2, Math.min(40, baseHeight + Math.round((e.clientY - y) / 40)));
				rebuild(
					shown.map((p) =>
						p.id === id
							? { ...p, span, h, autoHeight: Math.abs(e.clientY - y) > 20 ? false : p.autoHeight }
							: p,
					),
				);
			} else if (Math.abs(e.clientX - x) + Math.abs(e.clientY - y) > 5)
				moveTile(
					id,
					shown.findIndex((p) => p.id === target),
				);
		};
		const cancelMove = () => {
			moving = null;
			handle.removeEventListener('pointermove', move);
			handle.removeEventListener('pointerup', end);
			handle.removeEventListener('pointercancel', cancelMove);
		};
		handle.addEventListener('pointermove', move);
		handle.addEventListener('pointerup', end);
		handle.addEventListener('pointercancel', cancelMove);
	}

	function keys(e: KeyboardEvent) {
		if (
			!editing ||
			!(e.ctrlKey || e.metaKey) ||
			e.key.toLowerCase() !== 'z' ||
			(e.target instanceof HTMLElement && e.target.closest('input,textarea,select'))
		)
			return;
		e.preventDefault();
		step(e.shiftKey ? 'redo' : 'undo');
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
<div class="dashboard-page">
	<PageHead title={t('dashboard.title')}>
		{#if subtitle}<p class="muted">{subtitle}</p>{/if}
		{#snippet actions()}{#if editing}<button
					class="btn"
					disabled={!undo.length || saving}
					aria-label={t('dashboard.editor.undo')}
					onclick={() => step('undo')}><Icon name="restart" size={15} /></button
				><button
					class="btn"
					disabled={!redo.length || saving}
					aria-label={t('dashboard.editor.redo')}
					onclick={() => step('redo')}><Icon name="restart" size={15} /></button
				><button class="btn" disabled={saving} onclick={cancel}>{t('common.cancel')}</button><button
					class="btn btn-primary"
					disabled={saving}
					onclick={finish}>{t('dashboard.editor.save')}</button
				>{:else}<button class="btn" onclick={startEditing}
					><Icon name="edit" size={16} />{t('dashboard.edit')}</button
				>{/if}{/snippet}
	</PageHead>
	{#if error}<div class="notice-error" role="alert">
			{error}<button class="btn" onclick={finish}>{t('ui.retry')}</button>
		</div>{/if}
	{#if editing}<div class="editbar">
			<div class="flow-controls">
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
				><span>{t('dashboard.visibleColumns', { count: actualColumns })}</span>
			</div>
			<button
				class="btn"
				onclick={() => {
					checkpoint();
					options = {};
					columns = 2;
					minimumWidth = 350;
					rebuild(defaults());
					customize = null;
				}}>{t('dashboard.restore')}</button
			>
		</div>{/if}
	<div class="dashboard-workspace" class:editing class:with-inspector={editing && !!customize}>
		<div class="area">
			{#key revision}<div
					class="dashboard-grid"
					style:grid-template-columns={`repeat(${actualColumns},minmax(0,1fr))`}
					{@attach attachGrid}
				>
					{#each mountedLayout as tile (tile.id)}
						{@const source = sourceId(tile.id)}{@const target = workload(tile.id)}{@const spec =
							tileSpec(tile.id)}{@const settings = tileOptions(options, tile.id)}
						<div
							class="grid-stack-item"
							style:grid-column={`span ${Math.min(tile.span ?? 1, actualColumns)}`}
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
								style:min-height={tile.autoHeight === false ? `${tile.h * 40}px` : undefined}
								style:height={tile.autoHeight === false ? `${tile.h * 40}px` : undefined}
								{@attach (element) => interceptLinks(element, tile.id)}
								class:selected={editing && customize === tile.id}
								style={tileStyle(settings)}
								aria-label={titleOf(tile.id)}
								onpointerdown={(e) => selectPart(e, tile.id)}
							>
								{#if settings.showTitle || editing}<div class="tile-head">
										{#if editing}<button
												class="grip"
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
										<button
											class="tile-action"
											aria-label={`${t('dashboard.tileOptions')}: ${titleOf(tile.id)}`}
											onclick={() => {
												if (!editing) startEditing();
												configure(tile.id);
											}}><Icon name="edit" size={14} /></button
										>{#if editing}<button
												class="tile-action"
												aria-label={t('dashboard.hide', { tile: titleOf(tile.id) })}
												onclick={() => hide(tile.id)}><Icon name="close" size={16} /></button
											>{/if}
									</div>{/if}
								<div class="tile-body" data-edit-part="labels">
									{#if target?.kind === 'app'}<CollectionTile
											kind="docker"
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
										/>
									{:else if source === 'network'}<NetworkTile options={settings} />
									{:else if source === 'system'}<SystemTile options={settings} />
									{:else if source === 'igpu' || source === 'npu' || source === 'nvidia' || source === 'accelerators'}<AcceleratorTile
											kind={source}
											options={settings}
										/>
									{:else if source === 'docker' || source === 'vms' || source === 'shares'}<CollectionTile
											kind={source}
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
								</div>
								{#if editing}<button
										class="resize-handle"
										aria-label={t('dashboard.resize', { tile: titleOf(tile.id) })}
										onpointerdown={(e) => pointerMove(e, tile.id, true)}
										><Icon name="resize" size={13} /></button
									>{/if}
							</section>
						</div>
					{/each}
				</div>{/key}
			{#if editing}<section class="tile add-tiles">
					<h2 class="tile-title">{t('dashboard.hidden')}</h2>
					<div class="tile-choices">
						{#each hidden as id}<button
								class="btn"
								disabled={shown.length >= 100}
								onclick={() => add(id)}><Icon name="plus" size={14} />{t(TILES[id].title)}</button
							>{/each}
					</div>
					{#if admin}<label
							>{t('dashboard.workload')}<select class="field" bind:value={selection}
								><option value="">{t('dashboard.pickWorkload')}</option
								>{#each dockerGroups(containers).filter((g) => g.id.startsWith('app:')) as g}<option
										value={g.id}>{g.name} · {t('docker.stack')}</option
									>{/each}{#each vms as vm}<option value="vm:{vm.name}">{vmLabel(vm)}</option
									>{/each}{#each containers as c}<option value="container:{c.name}"
										>{dockerName(c, 'distinct')}</option
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
				</section>{/if}
		</div>
		{#if editing && customize && currentPlacement}<TileEditor
				id={customize}
				title={titleOf(customize)}
				settings={currentSettings}
				placement={currentPlacement}
				onauto={(value) => {
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
	.dashboard-page {
		background: var(--bg);
		box-shadow: 0 0 0 24px var(--bg);
	}
	.dashboard-grid {
		display: grid;
		gap: 12px;
		align-items: start;
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
		padding: 0;
		border: var(--tile-border, 1px) solid var(--tile-border-color, var(--border));
		border-radius: var(--tile-radius, 12px);
		background: var(--tile-background, var(--surface));
		box-shadow: var(--tile-shadow, none);
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
		overflow-x: hidden;
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
	.editbar {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
		border: 1px solid var(--border);
		background: var(--surface);
		border-radius: var(--radius-ctl);
		padding: 10px 14px;
		font-size: 0.8rem;
		color: var(--muted);
	}
	.add-tiles {
		padding: 20px;
		margin: 18px 8px 8px;
		display: flex;
		flex-direction: column;
		gap: 15px;
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
		.dashboard-tile {
			max-width: 100%;
		}
		.tile-body :global(.reading) {
			max-width: 100%;
			overflow-wrap: anywhere;
		}
	}
	@media (prefers-reduced-transparency: reduce) {
		.dashboard-tile {
			background: var(--surface);
		}
	}
</style>
