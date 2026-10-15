<script lang="ts">
	import { vmLabel } from '#lib/vm-name.ts';
	import 'gridstack/dist/gridstack.min.css';

	import { GridStack } from 'gridstack';
	import { onDestroy, type Component } from 'svelte';
	import { api, ApiError, type VM, type VMStatus, type DockerContainer, type DockerStatus } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { formatDuration } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { shell } from '#lib/shell.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { COLUMNS, defaultLayout, defaultPlacement, GAP, ROW_HEIGHT, sanitizeLayout, TILE_IDS, TILES, tileSpec, workload, type StaticTileId, type TileId, type TilePlacement } from './tiles.ts';
	import WorkloadTile from './tiles/WorkloadTile.svelte';
	import IGPUTile from './tiles/IGPUTile.svelte';
	import NPUTile from './tiles/NPUTile.svelte';
	import NVIDIATile from './tiles/NVIDIATile.svelte';
	import DisksTile from './tiles/DisksTile.svelte';
	import NetworkTile from './tiles/NetworkTile.svelte';
	import SystemTile from './tiles/SystemTile.svelte';
	import VolumesTile from './tiles/VolumesTile.svelte';
	import TemperaturesTile from './tiles/TemperaturesTile.svelte';

	const CONTENT: Record<StaticTileId, Component> = { system: SystemTile, volumes: VolumesTile, network: NetworkTile, disks: DisksTile, igpu: IGPUTile, npu: NPUTile, nvidia: NVIDIATile, temperatures: TemperaturesTile };

	/** Below this width the tiles are stacked and cannot be arranged. */
	const STACK_BELOW = 760;

	// The layout as saved for the account …
	let saved = $state(sanitizeLayout(session.dashboard));
	// … and as shown. They differ while editing.
	let shown = $state(sanitizeLayout(session.dashboard));
	// Counts up whenever the grid has to be built from `shown` again.
	let revision = $state(0);
	let editing = $state(false);
	let saving = $state(false);
	let error = $state('');

	const admin = $derived(session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin');
	let vms = $state<VM[]>([]);
	let containers = $state<DockerContainer[]>([]);
	let workloadsReady = $state(false);
	let workloadError = $state('');
	let selection = $state('');
	const titleOf = (id: TileId) => {
		const target = workload(id);
		const vm = target?.kind === 'vm' ? vms.find((vm) => vm.name === target.name) : undefined;
		return vm ? vmLabel(vm) : target?.name ?? t(tileSpec(id).title);
	};
	const watchWorkloads = $derived(admin && (editing || shown.some((tile) => workload(tile.id))));
	$effect(() => {
		if (!watchWorkloads) return;
		let disposed = false;
		let fetching = false;
		async function refresh() {
			if (fetching || document.hidden) return;
			fetching = true;
			try {
				const [machines, docker] = await Promise.all([api.get<VMStatus>('/vms'), api.get<DockerStatus>('/docker')]);
				if (!disposed) { vms = machines.vms; containers = docker.containers; workloadsReady = true; workloadError = ''; }
			} catch (err) {
				if (!disposed) workloadError = errorText(err instanceof ApiError ? err.code : 'internal');
			} finally { fetching = false; }
		}
		refresh();
		const timer = setInterval(refresh, 5000);
		return () => { disposed = true; clearInterval(timer); };
	});
	// Version 1 did not show detected accelerators by default. Add them once;
	// version 2 retains a user's later choice to hide them.
	let hardwareMigrated = false;
	$effect(() => {
		if (hardwareMigrated || editing || !system.info?.accelerators.length) return;
		hardwareMigrated = true;
		if ((session.dashboard as { version?: number } | null)?.version === 2) return;
		const ids = new Set<StaticTileId>(system.info.accelerators.map((device) => device.kind === 'npu' ? 'npu' : device.vendor === 'NVIDIA' ? 'nvidia' : 'igpu'));
		const layout = [...shown, ...[...ids].filter((id) => !shown.some((tile) => tile.id === id)).map(defaultPlacement)];
		saved = layout;
		rebuild(layout);
	});
	function addWorkload() {
		const id = selection as TileId;
		if (workload(id) && !shown.some((tile) => tile.id === id)) rebuild([...readGrid(), defaultPlacement(id)]);
		selection = '';
	}

	let grid: GridStack | undefined;

	const hidden = $derived(TILE_IDS.filter((id) => !shown.some((tile) => tile.id === id)));

	const subtitle = $derived.by(() => {
		const info = system.info;
		if (!info) return '';
		const parts = [t('dashboard.uptime', { uptime: formatDuration(info.uptimeSeconds) }), `TitanOS ${info.version}`];
		if (info.image?.slot) parts.push(t('dashboard.slot', { slot: info.image.slot.toUpperCase() }));
		return parts.join(' · ');
	});

	$effect(() => {
		shell.statusNote = editing ? t('dashboard.unsaved') : '';
	});
	onDestroy(() => (shell.statusNote = ''));

	// gridstack moves and resizes the tiles itself. Svelte renders them once
	// per revision and then leaves the grid alone.
	function attachGrid(element: HTMLElement) {
		grid = GridStack.init(
			{
				column: COLUMNS,
				// A row of the grid includes the gap, which gridstack puts around every tile.
				cellHeight: ROW_HEIGHT + GAP,
				margin: GAP / 2,
				animate: false,
				staticGrid: !editing,
				handle: '.grip',
				resizable: { handles: 'se' },
				alwaysShowResizeHandle: true,
				columnOpts: { breakpoints: [{ w: STACK_BELOW, c: 1 }] }
			},
			element
		)!;
		grid.on('change', () => (shown = readGrid()));
		return () => {
			grid?.destroy(false);
			grid = undefined;
		};
	}

	function readGrid(): TilePlacement[] {
		if (!grid) return shown;
		return grid.getGridItems().flatMap((element) => {
			const node = element.gridstackNode;
			if (!node?.id) return [];
			return [{ id: node.id as TileId, x: node.x ?? 0, y: node.y ?? 0, w: node.w ?? 1, h: node.h ?? 1 }];
		});
	}

	function rebuild(layout: TilePlacement[]) {
		shown = layout;
		revision++;
	}

	function startEditing() {
		error = '';
		editing = true;
		grid?.setStatic(false);
	}

	function cancel() {
		editing = false;
		rebuild(saved);
	}

	async function finish() {
		const layout = readGrid();
		const preferences = { ...session.dashboard, version: 2, tiles: layout };
		saving = true;
		error = '';
		try {
			await api.put('/preferences/dashboard', preferences);
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
			saving = false;
			return;
		}
		saving = false;
		saved = layout;
		session.dashboard = preferences;
		editing = false;
		rebuild(layout);
	}
</script>

<div class="head">
	<div class="title">
		<h1>{t('dashboard.title')}</h1>
		{#if subtitle}<p class="muted">{subtitle}</p>{/if}
	</div>
	{#if !editing}
		<button type="button" class="btn edit" onclick={startEditing}><Icon name="edit" size={16} />{t('dashboard.edit')}</button>
	{/if}
</div>

{#if editing}
	<div class="editbar">
		<div class="hint">
			<span class="hint-title">{t('dashboard.editTitle')}</span>
			<span class="muted">{t('dashboard.editHint')}</span>
		</div>
		<div class="actions">
			<button type="button" class="btn" onclick={() => rebuild(defaultLayout())}>{t('dashboard.restore')}</button>
			<button type="button" class="btn" onclick={cancel} disabled={saving}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" onclick={finish} disabled={saving}>{t('common.done')}</button>
		</div>
	</div>
	{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
{/if}

<div class="area" class:editing>
	{#if editing}
		<div class="columns" aria-hidden="true">
			{#each { length: COLUMNS } as _, index (index)}<span></span>{/each}
		</div>
	{/if}

	{#key revision}
		<div class="grid-stack" {@attach attachGrid}>
			{#each shown as tile (tile.id)}
				{@const spec = tileSpec(tile.id)}
				{@const target = workload(tile.id)}
				<div class="grid-stack-item" gs-id={tile.id} gs-x={tile.x} gs-y={tile.y} gs-w={tile.w} gs-h={tile.h} gs-min-w={spec.minW} gs-min-h={spec.minH} gs-auto-position={tile.x === undefined ? 'true' : undefined}>
					<section class="grid-stack-item-content tile" aria-labelledby="tile-{tile.id}">
						<div class="tile-head">
							{#if editing}
								<span class="grip" role="img" aria-label={t('dashboard.move', { tile: titleOf(tile.id) })}><Icon name="grip" /></span>
							{/if}
							<h2 class="tile-title" id="tile-{tile.id}">{titleOf(tile.id)}</h2>
							{#if editing}
								<span class="size mono">{tile.w} × {tile.h}</span>
								<button type="button" class="hide" aria-label={t('dashboard.hide', { tile: titleOf(tile.id) })} onclick={() => rebuild(readGrid().filter((other) => other.id !== tile.id))}>
									<Icon name="close" size={16} />
								</button>
							{/if}
						</div>
						<div class="tile-body">
							{#if target}
								<WorkloadTile kind={target.kind} name={target.name} vm={vms.find((vm) => vm.name === target.name)} container={containers.find((container) => container.name === target.name)} ready={workloadsReady} error={workloadError} {admin} />
							{:else}
								{@const Content = CONTENT[tile.id as StaticTileId]}
								<Content />
							{/if}
						</div>
					</section>
				</div>
			{/each}
		</div>
	{/key}
</div>

{#if editing}
	<section class="tile hidden" aria-labelledby="hidden-title">
		<h2 class="tile-title" id="hidden-title">{t('dashboard.hidden')}</h2>
		{#each hidden as id (id)}
			<button type="button" class="btn" aria-label={t('dashboard.show', { tile: t(TILES[id].title) })} onclick={() => rebuild([...readGrid(), defaultPlacement(id)])}>
				<Icon name="plus" size={16} />{t(TILES[id].title)}
			</button>
		{:else}
			<span>{t('dashboard.allVisible')}</span>
		{/each}
		{#if admin}
			<div class="workload-picker">
				<label for="workload">{t('dashboard.workload')}</label>
				<select id="workload" class="field" bind:value={selection}>
					<option value="">{t('dashboard.pickWorkload')}</option>
					<optgroup label={t('tile.vm')}>
						{#each vms.filter((vm) => !shown.some((tile) => tile.id === `vm:${vm.name}`)) as vm (vm.name)}<option value="vm:{vm.name}">{vmLabel(vm)}</option>{/each}
					</optgroup>
					<optgroup label={t('tile.container')}>
						{#each containers.filter((container) => !shown.some((tile) => tile.id === `container:${container.name}`)) as container (container.name)}<option value="container:{container.name}">{container.name}</option>{/each}
					</optgroup>
				</select>
				<button class="btn" disabled={!selection} onclick={addWorkload}>{t('dashboard.addWorkload')}</button>
			</div>
			{#if workloadError}<p class="notice-error" role="alert">{workloadError}</p>{/if}
		{/if}

		<span class="muted note">{t('dashboard.hiddenHint')}</span>
	</section>
{/if}

<style>
	.workload-picker { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; width: 100%; }
	.workload-picker select { width: auto; flex: 1; min-width: 180px; }
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	.title p {
		font-size: 0.875rem;
	}

	.editbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 12px 16px;
		background: color-mix(in srgb, var(--accent) 10%, var(--surface));
		border: 1px solid color-mix(in srgb, var(--accent) 35%, var(--border));
		border-radius: var(--radius-tile);
	}
	.hint {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		font-size: 0.8125rem;
	}
	.hint-title {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}

	.area {
		position: relative;
		/* gridstack puts half a gap around every tile; pull the grid back to the page edge. */
		margin: calc(var(--dash-gap) / -2);
		--dash-gap: 16px;
	}
	.columns {
		position: absolute;
		inset: calc(var(--dash-gap) / 2);
		display: grid;
		grid-template-columns: repeat(12, minmax(0, 1fr));
		gap: var(--dash-gap);
		pointer-events: none;
	}
	.columns span {
		background: color-mix(in srgb, var(--accent) 5%, transparent);
		border: 1px dashed color-mix(in srgb, var(--accent) 22%, transparent);
		border-radius: var(--radius-ctl);
	}
	.grid-stack {
		min-height: 120px;
	}

	.tile {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: var(--tile-pad);
		overflow: hidden;
	}
	.tile-head {
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 20px;
	}
	.tile-head h2 {
		flex: 1 1 auto;
	}
	.tile-body {
		flex: 1 1 auto;
		min-height: 0;
		display: flex;
		flex-direction: column;
		gap: 16px;
		overflow-y: auto;
	}

	/* While arranging, tiles are solid: the column guides stay behind them. */
	.editing .tile {
		background: var(--surface);
		border-color: var(--outline-border);
		-webkit-backdrop-filter: none;
		backdrop-filter: none;
	}
	.editing .tile-head {
		margin: calc(var(--tile-pad) * -1) calc(var(--tile-pad) * -1) 0;
		padding: 6px 8px 6px 4px;
		background: var(--chip);
		border-bottom: 1px solid var(--border);
	}
	.editing .tile-body {
		opacity: 0.85;
	}
	.grip,
	.hide {
		width: 36px;
		height: 36px;
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
	}
	.grip {
		cursor: grab;
		touch-action: none;
	}
	.hide {
		cursor: pointer;
	}
	.hide:hover {
		background: var(--bg);
		color: var(--text);
	}
	.size {
		font-size: 0.75rem;
		color: var(--muted);
	}

	/* The corner handle and the drop placeholder come from gridstack. */
	.area :global(.ui-resizable-se) {
		right: 10px;
		bottom: 10px;
		width: 18px;
		height: 18px;
		background: none;
		border-right: 2px solid var(--muted);
		border-bottom: 2px solid var(--muted);
		border-bottom-right-radius: 4px;
		transform: none;
		cursor: nwse-resize;
	}
	.area:not(.editing) :global(.ui-resizable-handle) {
		display: none;
	}
	.area :global(.grid-stack-placeholder > .placeholder-content) {
		background: color-mix(in srgb, var(--accent) 6%, transparent);
		border: 2px dashed var(--accent);
		border-radius: var(--radius-tile);
	}
	.area :global(.ui-draggable-dragging > .tile),
	.area :global(.ui-resizable-resizing > .tile) {
		border-color: var(--accent);
		box-shadow: var(--shadow-menu);
	}

	.hidden {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 12px;
		padding: 16px 18px;
		border-style: dashed;
		font-size: 0.875rem;
	}
	.note {
		flex: 1 1 260px;
		font-size: 0.8125rem;
	}

	@media (max-width: 760px) {
		.edit {
			display: none;
		}
	}
</style>
