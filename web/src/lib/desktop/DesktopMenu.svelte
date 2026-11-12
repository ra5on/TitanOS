<script lang="ts">
	// The context menus of the desktop: of an icon, a widget, the dock, the
	// top bar and the free desktop, with their sub-menus.
	import ContextMenu from '../components/ContextMenu.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import {
		autoArrange,
		dockerAction as runDocker,
		removeIcon as dropIcon,
		removeWidget,
		resizeWidget,
		setWidgetCamera,
		setWidgetDevice,
		toggleWidgetValue,
		type Cell,
		type DeskDialog,
		type DeskMenu,
		type IconsNow,
	} from './desk-ops.ts';
	import { dockPin, dockUnpin } from './Dock.svelte';
	import { GPU_VALUES, isGpuWidget, type GpuValue } from './prefs.ts';
	import { loadWidgetCameras, widgetCameras } from '../nvr/widget.svelte.ts';
	import { openProgram, openTab, type Program } from './programs.svelte.ts';
	import { system } from '../system.svelte.ts';
	import { desktop, requestClose, savePrefs, wm } from './wm.svelte.ts';

	let {
		menu = $bindable(),
		openFolder = $bindable(),
		renaming = $bindable(),
		editWidgets = $bindable(),
		editDock = $bindable(),
		dialog = $bindable(),
		byKey,
		role,
		inDock,
		known,
		icons,
		places,
		iconsNow,
		open,
		newFolder,
	}: {
		menu: DeskMenu | null;
		openFolder: string | null;
		renaming: string | null;
		editWidgets: boolean;
		editDock: boolean;
		dialog: DeskDialog | null;
		byKey: Map<string, Program>;
		role: string;
		inDock: Set<string>;
		known: Set<string>;
		icons: string[];
		places: Map<string, Cell>;
		iconsNow: IconsNow;
		open: (key: string, event?: Event, href?: string) => void;
		newFolder: () => void;
	} = $props();

	const removeIcon = (key: string) => dropIcon(key, iconsNow);

	// PLAN 79-5: the menu of a graphics widget – a check per value and, with
	// several devices of the kind, the device. Stored with the widget.
	const GPU_TITLE = {
		igpu: 'desktop.widget.igpu',
		npu: 'desktop.widget.npu',
		gpu: 'desktop.widget.gpu',
	} as const;
	const GPU_VALUE_TEXT: Record<GpuValue, Parameters<typeof t>[0]> = {
		usage: 'desktop.widget.gpuUsage',
		history: 'desktop.widget.gpuHistory',
		temperature: 'desktop.widget.gpuTemperature',
		memory: 'desktop.widget.gpuMemory',
		power: 'desktop.widget.gpuPower',
		clock: 'desktop.widget.gpuClock',
	};
	const gpuWidget = $derived.by(() => {
		if (menu?.kind !== 'widget' || !menu.key) return null;
		const w = desktop.prefs.widgets.find((x) => x.id === menu?.key);
		if (!w || !isGpuWidget(w.kind)) return null;
		const devices = (system.info?.accelerators ?? []).filter((a) => a.kind === w.kind);
		const chosen = devices.find((a) => a.address === w.device) ?? devices[0];
		return { w, kind: w.kind, devices, chosen };
	});
	const cameraWidget = $derived(
		menu?.kind === 'widget' && menu.key
			? (desktop.prefs.widgets.find((x) => x.id === menu?.key && x.kind === 'camera') ?? null)
			: null,
	);
	$effect(() => {
		if (cameraWidget) void loadWidgetCameras();
	});
	function dockerAction(key: string, action: 'restart' | 'stop' | 'start') {
		menu = null;
		return runDocker(key, action);
	}
</script>

{#if menu}
	<ContextMenu
		label={t('desktop.menu.context')}
		x={menu.x}
		y={menu.y}
		onclose={() => (menu = null)}
	>
		{#if menu.kind === 'icon' && menu.key}
			{@const key = menu.key}
			{@const p = byKey.get(key)}
			{#if key.startsWith('folder:')}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						openFolder = key;
						menu = null;
					}}><span>{t('desktop.ctx.open')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						renaming = key;
						menu = null;
					}}><span>{t('desktop.folder.rename')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					class="danger"
					onclick={() => {
						removeIcon(key);
						menu = null;
					}}><span>{t('desktop.folder.remove')}</span></button
				>
			{:else if p}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						open(key);
						menu = null;
					}}><span>{t('desktop.ctx.open')}</span></button
				>
				{#if p.multi || p.kind === 'system'}<button
						type="button"
						role="menuitem"
						onclick={() => {
							void openProgram(key, { fresh: true });
							menu = null;
						}}><span>{t('desktop.ctx.newWindow')}</span></button
					>{/if}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						void openTab(key, p.href);
						menu = null;
					}}><span>{t('desktop.window.openTab')}</span></button
				>
				{#if p.kind === 'docker' && role !== 'user'}
					<button type="button" role="menuitem" onclick={() => dockerAction(key, 'restart')}
						><span>{t('desktop.ctx.restart')}</span></button
					>
					<button
						type="button"
						role="menuitem"
						onclick={() => dockerAction(key, p.badge?.dot === 'off' ? 'start' : 'stop')}
						><span>{t(p.badge?.dot === 'off' ? 'desktop.ctx.start' : 'desktop.ctx.stop')}</span
						></button
					>
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							open('app:docker', undefined, '/docker/apps/' + encodeURIComponent(key.slice(7)));
							menu = null;
						}}><span>{t('desktop.ctx.showInDocker')}</span></button
					>
				{/if}
				<span class="menu-rule" role="separator"></span>
				{#if inDock.has(key)}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							dockUnpin(key, known);
							menu = null;
						}}><span>{t('desktop.dock.unpin')}</span></button
					>
				{:else}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							dockPin(key, known);
							if (!desktop.prefs.dock.show) savePrefs((d) => (d.dock.show = true));
							menu = null;
						}}><span>{t('desktop.dock.pin')}</span></button
					>
				{/if}
				{#if icons.includes(key)}
					{#if !menu.dock}
						<button
							type="button"
							role="menuitem"
							class="danger"
							onclick={() => {
								removeIcon(key);
								menu = null;
							}}><span>{t('desktop.ctx.removeFromDesktop')}</span></button
						>
					{/if}
				{:else}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							savePrefs((d) => {
								d.icons = [...iconsNow(), key];
								d.customIcons = true;
							});
							menu = null;
						}}><span>{t('desktop.ctx.toDesktop')}</span></button
					>
				{/if}
				{#if wm.windows.some((w) => w.app === key)}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							wm.windows.filter((w) => w.app === key).forEach((w) => requestClose(w.id));
							menu = null;
						}}><span>{t('desktop.dock.closeAll')}</span></button
					>
				{/if}
			{/if}
		{:else if menu.kind === 'bar'}
			{#each [['s', 'desktop.size.s'], ['m', 'desktop.size.m'], ['l', 'desktop.size.l']] as const as [size, label] (size)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={desktop.prefs.barSize === size}
					onclick={() => {
						savePrefs((p) => (p.barSize = size));
						menu = null;
					}}><span>{t('desktop.bar.size')}: {t(label)}</span></button
				>
			{/each}
			<span class="menu-rule" role="separator"></span>
			<button
				type="button"
				role="menuitemcheckbox"
				aria-checked={desktop.prefs.barLoad}
				onclick={() => {
					savePrefs((p) => (p.barLoad = !p.barLoad));
					menu = null;
				}}><span>{t('desktop.bar.load')}</span></button
			>
		{:else if menu.kind === 'widget' && menu.key}
			{@const id = menu.key}
			{#if gpuWidget}
				{@const g = gpuWidget}
				<div class="menu-title">
					{t('desktop.widget.gpuShows', { name: t(GPU_TITLE[g.kind]) })}
				</div>
				{#each GPU_VALUES as value (value)}
					<!-- The menu stays open: several values can be switched one after the other. -->
					<button
						type="button"
						role="menuitemcheckbox"
						data-gpu-toggle={value}
						aria-checked={!(g.w.hide ?? []).includes(value)}
						onclick={() => toggleWidgetValue(id, value)}
						><span>{t(GPU_VALUE_TEXT[value])}</span></button
					>
				{/each}
				{#if g.devices.length > 1}
					<span class="menu-rule" role="separator"></span>
					<div class="menu-title">{t('desktop.widget.gpuDevice')}</div>
					{#each g.devices as d (d.address)}
						<button
							type="button"
							class="wrap"
							role="menuitemradio"
							data-gpu-device={d.address}
							aria-checked={g.chosen?.address === d.address}
							onclick={() => setWidgetDevice(id, d.address)}
							><span>{d.name || d.address}</span></button
						>
					{/each}
				{/if}
				<span class="menu-rule" role="separator"></span>
			{/if}
			{#if cameraWidget && widgetCameras.list.length}
				<!-- PLAN 79-7: which camera the widget shows. -->
				<div class="menu-title">{t('desktop.widget.cameraChoice')}</div>
				{#each widgetCameras.list as c (c.id)}
					<button
						type="button"
						class="wrap"
						role="menuitemradio"
						data-widget-camera={c.id}
						aria-checked={(cameraWidget.camera ?? widgetCameras.list[0]?.id) === c.id}
						onclick={() => setWidgetCamera(id, c.id)}><span>{c.name}</span></button
					>
				{/each}
				<span class="menu-rule" role="separator"></span>
			{/if}
			<button
				type="button"
				role="menuitem"
				onclick={() => (menu = menu && { ...menu, kind: 'widgetSize' })}
				><span>{t('desktop.widgets.size')}</span><span aria-hidden="true">›</span></button
			>
			<button
				type="button"
				role="menuitem"
				class="danger"
				onclick={() => {
					removeWidget(id);
					menu = null;
				}}><span>{t('desktop.widgets.removeOne')}</span></button
			>
			<span class="menu-rule" role="separator"></span>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					editWidgets = true;
					menu = null;
				}}><span>{t('desktop.ctx.widgets')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					dialog = 'gallery';
					menu = null;
				}}><span>{t('desktop.widgets.add')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					savePrefs((p) => (p.widgetsHidden = true));
					editWidgets = false;
					menu = null;
				}}><span>{t('desktop.widgets.hideAll')}</span></button
			>
		{:else if menu.kind === 'widgetSize' && menu.key}
			{@const id = menu.key}
			{#each [['s', 'desktop.size.s'], ['m', 'desktop.size.m'], ['l', 'desktop.size.l']] as const as [size, label] (size)}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						resizeWidget(id, size);
						menu = null;
					}}><span>{t(label)}</span></button
				>
			{/each}
		{:else if menu.kind === 'dock'}
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					editDock = true;
					menu = null;
				}}><span>{t('desktop.dock.edit')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => (menu = menu && { ...menu, kind: 'dockPos' })}
				><span>{t('desktop.dock.position')}</span><span aria-hidden="true">›</span></button
			>
			<button
				type="button"
				role="menuitemcheckbox"
				aria-checked={desktop.prefs.dock.magnify > 0}
				onclick={() => {
					savePrefs((p) => (p.dock.magnify = p.dock.magnify ? 0 : 1));
					menu = null;
				}}><span>{t('desktop.dock.magnify')}</span></button
			>
			<span class="menu-rule" role="separator"></span>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					savePrefs((p) => (p.dock.show = false));
					editDock = false;
					menu = null;
				}}><span>{t('desktop.dock.hide')}</span></button
			>
		{:else if menu.kind === 'dockPos'}
			{#each [['bottom', 'desktop.dock.bottom'], ['left', 'desktop.dock.left'], ['right', 'desktop.dock.right']] as const as [pos, label] (pos)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={desktop.prefs.dock.position === pos}
					onclick={() => {
						savePrefs((p) => (p.dock.position = pos));
						menu = null;
					}}><span>{t(label)}</span></button
				>
			{/each}
		{:else if menu.kind === 'desk'}
			<button
				type="button"
				role="menuitem"
				onclick={() => (menu = menu && { ...menu, kind: 'sort' })}
				><span>{t('desktop.ctx.sort')}</span><span aria-hidden="true">›</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => (menu = menu && { ...menu, kind: 'size' })}
				><span>{t('desktop.ctx.iconSize')}</span><span aria-hidden="true">›</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					autoArrange();
					menu = null;
				}}><span>{t('desktop.ctx.autoArrange')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					newFolder();
					menu = null;
				}}><span>{t('desktop.ctx.newFolder')}</span></button
			>
			<span class="menu-rule" role="separator"></span>
			<button
				type="button"
				role="menuitemcheckbox"
				aria-checked={!desktop.prefs.widgetsHidden}
				onclick={() => {
					savePrefs((p) => (p.widgetsHidden = !p.widgetsHidden));
					menu = null;
				}}><span>{t('desktop.widgets.show')}</span></button
			>
			<button
				type="button"
				role="menuitemcheckbox"
				aria-checked={desktop.prefs.dock.show}
				onclick={() => {
					savePrefs((p) => (p.dock.show = !p.dock.show));
					menu = null;
				}}><span>{t('desktop.dock.show')}</span></button
			>
			<span class="menu-rule" role="separator"></span>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					dialog = 'wallpaper';
					menu = null;
				}}><span>{t('desktop.ctx.wallpaper')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					editWidgets = true;
					if (desktop.prefs.widgetsHidden) savePrefs((p) => (p.widgetsHidden = false));
					menu = null;
				}}><span>{t('desktop.ctx.widgets')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					dialog = 'layout';
					menu = null;
				}}><span>{t('desktop.ctx.layouts')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					dialog = 'settings';
					menu = null;
				}}><span>{t('desktop.ctx.settings')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					open('app:settings', undefined, '/settings/appearance');
					menu = null;
				}}><span>{t('desktop.ctx.appearance')}</span></button
			>
		{:else if menu.kind === 'sort'}
			{#each [['name', 'desktop.sort.name'], ['kind', 'desktop.sort.kind'], ['recent', 'desktop.sort.recent'], ['free', 'desktop.sort.free']] as const as [mode, label] (mode)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={desktop.prefs.sort === mode}
					onclick={() => {
						savePrefs((p) => {
							// Into "free" without own places: the icons stay where they are now.
							if (mode === 'free' && p.sort !== 'free' && !Object.keys(p.iconPos).length)
								p.iconPos = Object.fromEntries([...places].map(([k, at]) => [k, { ...at }]));
							p.sort = mode;
						});
						menu = null;
					}}><span>{t(label)}</span></button
				>
			{/each}
		{:else if menu.kind === 'size'}
			{#each [['s', 'desktop.size.s'], ['m', 'desktop.size.m'], ['l', 'desktop.size.l']] as const as [size, label] (size)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={desktop.prefs.iconSize === size}
					onclick={() => {
						savePrefs((p) => (p.iconSize = size));
						menu = null;
					}}><span>{t(label)}</span></button
				>
			{/each}
		{/if}
	</ContextMenu>
{/if}

<style>
	/* A device's name may be long: it wraps instead of leaving the menu. */
	button.wrap {
		white-space: normal;
		overflow-wrap: anywhere;
		padding-top: var(--space-1-5);
		padding-bottom: var(--space-1-5);
	}
</style>
