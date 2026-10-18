<script lang="ts">
	import { dockerName } from '#lib/docker-name.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import 'gridstack/dist/gridstack.min.css';

	import { GridStack } from 'gridstack';
	import { onDestroy, untrack, type Component } from 'svelte';
	import { api, ApiError, type VM, type VMStatus, type DockerContainer, type DockerStatus } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { formatDuration } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { shell } from '#lib/shell.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { COLUMNS, defaultLayout, defaultPlacement, GAP, ROW_HEIGHT, sanitizeLayout, TILE_IDS, TILES, tileSpec, workload, type StaticTileId, type TileId, type TilePlacement } from './tiles.ts';
	import Dialog from '#lib/components/Dialog.svelte';
 import { tileOptions, type TileOptions } from './options.ts';
 import UsageTile from './tiles/UsageTile.svelte';
 import AcceleratorTile from './tiles/AcceleratorTile.svelte';
 import WorkloadTile from './tiles/WorkloadTile.svelte';
	import DisksTile from './tiles/DisksTile.svelte';
	import NetworkTile from './tiles/NetworkTile.svelte';
	import SystemTile from './tiles/SystemTile.svelte';
	import VolumesTile from './tiles/VolumesTile.svelte';
	import TemperaturesTile from './tiles/TemperaturesTile.svelte';

	const CONTENT: Partial<Record<StaticTileId, Component>> = { system: SystemTile, volumes: VolumesTile, disks: DisksTile, temperatures: TemperaturesTile };

	/** Below this width the tiles are stacked and cannot be arranged. */
	const STACK_BELOW = 760;

	// The layout as saved for the account …
	let saved = $state(initialLayout());
	// … and as shown. They differ while editing.
	let shown = $state(initialLayout());
 let mountedLayout = $state(initialLayout());
 let options = $state<Record<string, TileOptions>>(readOptions());
 let savedOptions = $state(readOptions());
 let customize = $state<TileId | null>(null);
 let tileWidth = $state(4);
 let tileHeight = $state(2);
 let tileSettings = $state(tileOptions(null, 'cpu'));

 function initialLayout() {
  const layout = sanitizeLayout(session.dashboard);
  if (Number(session.dashboard?.version ?? 0) >= 3 || !layout.some((tile) => tile.id === 'system')) return layout;
  return [...layout, ...(['cpu', 'memory'] as const).filter((id) => !layout.some((tile) => tile.id === id)).map(defaultPlacement)];
 }
 function readOptions() {
  return Object.fromEntries(initialLayout().map((tile) => [tile.id, tileOptions(session.dashboard?.tileOptions, tile.id)]));
 }
 function configure(id: TileId) {
  const tile = readGrid().find((tile) => tile.id === id);
  if (!tile) return;
  customize = id; tileWidth = tile.w; tileHeight = tile.h; tileSettings = tileOptions(options, id);
 }
 function applyOptions() {
  if (!customize) return;
  const id = customize;
  const layout = sanitizeLayout({ tiles: readGrid().map((tile) => tile.id === id ? { ...tile, w: Number(tileWidth), h: Number(tileHeight) } : tile) });
  options = { ...options, [id]: tileOptions({ [id]: tileSettings }, id) };
  customize = null; rebuild(layout);
 }
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
		const container = target?.kind === 'container' ? containers.find((container) => container.name === target.name) : undefined;
		return vm ? vmLabel(vm) : container ? dockerName(container, 'distinct') : target?.name ?? t(tileSpec(id).title);
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
		if (Number(session.dashboard?.version ?? 0) >= 2) return;
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
  // Attachments track synchronous reads. GridStack owns its DOM and lifecycle;
  // editing/layout updates must not destroy it during a resize or scrollbar change.
  const instance = untrack(() => GridStack.init(
			{
				column: COLUMNS,
				// A row of the grid includes the gap, which gridstack puts around every tile.
				cellHeight: ROW_HEIGHT + GAP,
				margin: GAP / 2,
				animate: false,
                sizeToContent: false,
				staticGrid: !editing,
				handle: '.grip',
				resizable: { handles: 'se' },
				alwaysShowResizeHandle: true,
				columnOpts: { breakpoints: [{ w: STACK_BELOW, c: 1 }] }
			},
			element
		)!);
        grid = instance;
		instance.on('change', () => (shown = readGrid()));
		return () => {
			instance.destroy(false);
			if (grid === instance) grid = undefined;
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
        mountedLayout = layout;
		revision++;
	}

	function startEditing() {
		error = '';
		editing = true;
		grid?.setStatic(false);
	}

	function cancel() {
		editing = false;
        options = savedOptions;
		rebuild(saved);
	}

	async function finish() {
		const layout = readGrid();
		const preferences = { ...session.dashboard, version: 3, tiles: layout, tileOptions: options };
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
        savedOptions = options;
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
			<button type="button" class="btn" onclick={() => { options = {}; rebuild(defaultLayout()); }}>{t('dashboard.restore')}</button>
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
			{#each mountedLayout as tile (tile.id)}
				{@const spec = tileSpec(tile.id)}
				{@const target = workload(tile.id)}
				<div class="grid-stack-item" gs-id={tile.id} gs-x={tile.x} gs-y={tile.y} gs-w={tile.w} gs-h={tile.h} gs-min-w={spec.minW} gs-min-h={spec.minH} gs-auto-position={tile.x === undefined ? 'true' : undefined}>
					<section class="grid-stack-item-content tile" aria-labelledby="tile-{tile.id}">
						<div class="tile-head">
							{#if editing}
								<span class="grip" role="img" aria-label={t('dashboard.move', { tile: titleOf(tile.id) })}><Icon name="grip" /></span>
							{/if}
							<h2 class="tile-title" id="tile-{tile.id}">{#if target}<a href={target.kind === 'vm' ? `/vms?vm=${encodeURIComponent(target.name)}` : '/docker'}>{titleOf(tile.id)}</a>{:else}{titleOf(tile.id)}{/if}</h2>
							{#if editing}
								<button type="button" class="size" aria-label={`${t('dashboard.tileOptions')}: ${titleOf(tile.id)}`} onclick={() => configure(tile.id)}><Icon name="edit" size={14} /></button>
								<button type="button" class="hide" aria-label={t('dashboard.hide', { tile: titleOf(tile.id) })} onclick={() => rebuild(readGrid().filter((other) => other.id !== tile.id))}>
									<Icon name="close" size={16} />
								</button>
							{/if}
						</div>
						<div class="tile-body">
							{#if target}
								<WorkloadTile kind={target.kind} name={target.name} vm={vms.find((vm) => vm.name === target.name)} container={containers.find((container) => container.name === target.name)} ready={workloadsReady} error={workloadError} {admin} />
       {:else if tile.id === 'cpu' || tile.id === 'memory'}
        <UsageTile kind={tile.id} options={tileOptions(options, tile.id)} />
       {:else if tile.id === 'network'}
        <NetworkTile options={tileOptions(options, tile.id)} />
       {:else if tile.id === 'igpu' || tile.id === 'npu' || tile.id === 'nvidia'}
        <AcceleratorTile kind={tile.id} options={tileOptions(options, tile.id)} />
       {:else}
        {@const Content = CONTENT[tile.id as StaticTileId]}
        {#if Content}<Content />{/if}
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
						{#each containers.filter((container) => !shown.some((tile) => tile.id === `container:${container.name}`)) as container (container.name)}<option value="container:{container.name}">{dockerName(container, 'distinct')}</option>{/each}
					</optgroup>
				</select>
				<button class="btn" disabled={!selection} onclick={addWorkload}>{t('dashboard.addWorkload')}</button>
			</div>
			{#if workloadError}<p class="notice-error" role="alert">{workloadError}</p>{/if}
		{/if}

		<span class="muted note">{t('dashboard.hiddenHint')}</span>
	</section>
{/if}

{#if customize}
 <Dialog title={`${titleOf(customize)} · ${t('dashboard.tileOptions')}`} onclose={() => (customize = null)}>
  <form class="tile-options" onsubmit={(event) => { event.preventDefault(); applyOptions(); }}>
   <div class="dimensions">
    <label>{t('dashboard.width')}<input class="field" type="number" min={tileSpec(customize).minW} max={COLUMNS} step="1" bind:value={tileWidth} required /></label>
    <label>{t('dashboard.height')}<input class="field" type="number" min={tileSpec(customize).minH} max="40" step="1" bind:value={tileHeight} required /></label>
   </div>
   {#if ['cpu', 'memory', 'igpu', 'npu', 'nvidia', 'network'].includes(customize)}<label><input type="checkbox" bind:checked={tileSettings.details} /> {t('dashboard.details')}</label>{/if}
   {#if ['cpu', 'memory', 'igpu', 'npu', 'nvidia'].includes(customize)}<label><input type="checkbox" bind:checked={tileSettings.history} /> {t('dashboard.history')}</label>{/if}
   {#if customize === 'cpu'}<label><input type="checkbox" bind:checked={tileSettings.temperature} /> {t('dashboard.cpuTemperature')}</label>{/if}
   {#if customize === 'network'}
    <label>{t('dashboard.interface')}<select class="field" bind:value={tileSettings.interface}>
     <option value="">{t('dashboard.defaultInterface')}</option>
     {#if tileSettings.interface && !system.info?.networks?.some((n) => n.interface === tileSettings.interface)}<option value={tileSettings.interface}>{tileSettings.interface}</option>{/if}
     {#each system.info?.networks ?? [] as network (network.interface)}<option value={network.interface}>{network.interface}</option>{/each}
    </select></label>
    <label><input type="checkbox" bind:checked={tileSettings.ipv4} /> {t('network.ipv4')}</label>
    <label><input type="checkbox" bind:checked={tileSettings.ipv6} /> {t('network.ipv6')}</label>
    <label><input type="checkbox" bind:checked={tileSettings.link} /> {t('dashboard.link')}</label>
    <label><input type="checkbox" bind:checked={tileSettings.rates} /> {t('dashboard.rates')}</label>
   {/if}
   <div class="dialog-actions"><button class="btn" type="button" onclick={() => (customize = null)}>{t('common.cancel')}</button><button class="btn btn-primary" type="submit">{t('common.done')}</button></div>
  </form>
 </Dialog>
{/if}

<style>
 .tile-options { display: flex; flex-direction: column; gap: 14px; }
 .dimensions { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
 .tile-title { min-width: 0; overflow-wrap: anywhere; }
 .tile-title a { color: inherit; text-decoration: none; }
 .size { background: var(--chip); border: 0; border-radius: var(--radius-ctl); padding: 6px; cursor: pointer; }
 .tile-head { flex: none; }
 .tile-body > :global(*) { flex-shrink: 0; }

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
		font-size: var(--text-page);
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
		gap: 10px;
		padding: 14px;
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
		gap: 8px;
		overflow-y: auto;
        scrollbar-gutter: stable;
	}

	/* While arranging, tiles are solid: the column guides stay behind them. */
	.editing .tile {
		background: var(--surface);
		border-color: var(--outline-border);
		-webkit-backdrop-filter: none;
		backdrop-filter: none;
	}
	.editing .tile-head {
		margin: -14px -14px 0;
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
