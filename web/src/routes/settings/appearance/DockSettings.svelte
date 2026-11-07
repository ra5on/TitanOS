<script lang="ts">
	// PLAN 64/67: the dock's settings, the tab "Dock" of Systemsteuerung ›
	// Darstellung – shown or not, place, size, plate, growing under the
	// pointer and the kept programs (order by dragging or the arrows, remove,
	// add). The preview shows the dock as it will look; every change counts at
	// once, like on the dock itself.
	import { onMount } from 'svelte';
	import Segments from '#lib/components/Segments.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import DesktopIcon from '#lib/desktop/DesktopIcon.svelte';
	import Dock from '#lib/desktop/Dock.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { resolvePalette } from '#lib/theme/themes.ts';
	import { programs } from '#lib/desktop/programs.svelte.ts';
	import { watchSources } from '#lib/desktop/sources.svelte.ts';
	import { changeDock, connectDock, dockLink, moveItem } from '#lib/desktop/dock-link.svelte.ts';
	import {
		defaultDock,
		dockPinned,
		sanitizeDesktop,
		type DockPosition,
	} from '#lib/desktop/prefs.ts';
	import { desktop } from '#lib/desktop/wm.svelte.ts';
	import { wallpaperCss } from '#lib/desktop/wallpapers.ts';

	onMount(() => {
		const stopDock = connectDock();
		const stopSources = watchSources();
		return () => {
			stopDock();
			stopSources();
		};
	});

	const all = $derived(programs());
	const known = $derived(new Set(all.map((p) => p.key)));
	const byKey = $derived(new Map(all.map((p) => [p.key, p])));
	const dock = $derived(dockLink.dock);
	const pinned = $derived(dock ? dockPinned({ ...desktop.prefs, dock }, known) : []);
	const addable = $derived(
		all.filter((p) => !pinned.includes(p.key)).sort((a, b) => a.label.localeCompare(b.label)),
	);
	let adding = $state('');

	// The preview sits on the account's wallpaper (a built-in one in its place for an own picture).
	const light = $derived(!resolvePalette(appearance.current, appearance.systemLight).dark);
	const wallpaper = $derived.by(() => {
		const wp = sanitizeDesktop(session.desktop).wallpaper;
		return wp.source === 'custom'
			? `url("/api/v1/background?v=${wp.version}") center / cover`
			: `${wallpaperCss(wp.id, light)} center / cover`;
	});
	const previewHeight = $derived(
		dock && dock.position !== 'bottom'
			? Math.min(560, Math.max(180, pinned.length * (dock.size + 8) + 48))
			: 170,
	);

	const positions = $derived<{ value: DockPosition; label: string }[]>([
		{ value: 'bottom', label: t('desktop.dock.bottom') },
		{ value: 'left', label: t('desktop.dock.left') },
		{ value: 'right', label: t('desktop.dock.right') },
	]);
	const magnifyOptions = $derived<{ value: '0' | '1' | '2'; label: string }[]>([
		{ value: '0', label: t('desktop.dock.magnifyOff') },
		{ value: '1', label: t('desktop.dock.magnifyLight') },
		{ value: '2', label: t('desktop.dock.magnifyStrong') },
	]);

	function setPinned(list: string[]) {
		changeDock((d) => (d.pinned = list), known);
	}
	function move(index: number, by: number) {
		setPinned(moveItem(pinned, index, index + by));
	}
	function remove(key: string) {
		setPinned(pinned.filter((k) => k !== key));
	}
	function add() {
		if (!adding || pinned.includes(adding)) return;
		setPinned([...pinned, adding]);
		adding = '';
	}
	function reset() {
		changeDock((d) => Object.assign(d, defaultDock()));
	}

	// Dragging inside the list: the entry goes where it is dropped.
	let dragFrom = $state<number | null>(null);
	let dragOver = $state<number | null>(null);
	function drop(index: number) {
		if (dragFrom !== null && dragFrom !== index) setPinned(moveItem(pinned, dragFrom, index));
		dragFrom = null;
		dragOver = null;
	}
</script>

<div class="dock-tab">
	{#if !dock}
		<p class="muted" role="status">{t('common.loading')}</p>
	{:else}
		<section class="tile ui-card" aria-labelledby="h-dock-preview">
			<div class="card-head"><h2 id="h-dock-preview">{t('look.dock.preview')}</h2></div>
			<div
				class="stage"
				style:background={wallpaper}
				style:height="{previewHeight}px"
				data-testid="dock-preview"
			>
				{#if dock.show}
					<Dock {all} preview={dock} />
				{:else}
					<p class="hidden-note">{t('look.dock.hiddenNote')}</p>
				{/if}
			</div>
		</section>

		<section class="tile ui-card" aria-labelledby="h-dock-look">
			<div class="card-head"><h2 id="h-dock-look">{t('look.dock.look')}</h2></div>
			<div class="set-row">
				<span class="set-label" id="dock-show-label">{t('look.dock.show')}</span>
				<label class="check set-control end">
					<input
						type="checkbox"
						role="switch"
						class="switch"
						aria-labelledby="dock-show-label"
						checked={dock.show}
						onchange={(e) => {
							const on = e.currentTarget.checked;
							changeDock((d) => (d.show = on));
						}}
					/><span class="muted">{dock.show ? t('look.on') : t('look.off')}</span>
				</label>
			</div>
			<div class="set-row">
				<span class="set-label">{t('desktop.dock.position')}</span>
				<span class="set-control"
					><Segments
						label={t('desktop.dock.position')}
						options={positions}
						value={dock.position}
						onchange={(v) => changeDock((d) => (d.position = v))}
					/></span
				>
			</div>
			<div class="set-row">
				<label class="set-label" for="dock-size">{t('desktop.dock.size')}</label>
				<span class="range">
					<input
						id="dock-size"
						type="range"
						min="36"
						max="80"
						step="2"
						value={dock.size}
						oninput={(e) => {
							const v = Number(e.currentTarget.value);
							changeDock((d) => (d.size = v));
						}}
					/><span class="mono value">{dock.size} px</span>
				</span>
			</div>
			<div class="set-row">
				<label class="set-label" for="dock-opacity">{t('desktop.dock.opacity')}</label>
				<span class="range">
					<input
						id="dock-opacity"
						type="range"
						min="30"
						max="100"
						step="5"
						value={dock.opacity}
						oninput={(e) => {
							const v = Number(e.currentTarget.value);
							changeDock((d) => (d.opacity = v));
						}}
					/><span class="mono value">{dock.opacity} %</span>
				</span>
			</div>
			<div class="set-row">
				<span class="set-label">{t('desktop.dock.magnify')}</span>
				<span class="set-control"
					><Segments
						label={t('desktop.dock.magnify')}
						options={magnifyOptions}
						value={String(dock.magnify) as '0' | '1' | '2'}
						onchange={(v) => changeDock((d) => (d.magnify = Number(v) as 0 | 1 | 2))}
					/></span
				>
			</div>
			<div class="card-foot">
				<span class="grow">{t('look.dock.intro')}</span>
				<button type="button" class="btn" onclick={reset}>{t('desktop.dock.reset')}</button>
			</div>
		</section>

		<section class="tile ui-card" aria-labelledby="h-dock-apps">
			<div class="card-head">
				<h2 id="h-dock-apps">{t('look.dock.apps')}</h2>
				<span class="card-meta">{t('look.dock.appsHint')}</span>
			</div>
			<div class="card-body">
				{#if pinned.length}
					<ol class="pins" data-testid="dock-pins">
						{#each pinned as key, i (key)}
							{@const p = byKey.get(key)}
							<li
								draggable="true"
								class:over={dragOver === i && dragFrom !== i}
								data-key={key}
								ondragstart={(e) => {
									dragFrom = i;
									e.dataTransfer?.setData('text/plain', key);
								}}
								ondragover={(e) => {
									e.preventDefault();
									dragOver = i;
								}}
								ondragleave={() => (dragOver = dragOver === i ? null : dragOver)}
								ondrop={(e) => {
									e.preventDefault();
									drop(i);
								}}
								ondragend={() => {
									dragFrom = null;
									dragOver = null;
								}}
							>
								<span class="grip" aria-hidden="true"><Icon name="more" size={16} /></span>
								{#if p}<DesktopIcon
										glyph={p.glyph}
										gradient={p.gradient}
										image={p.image}
										name={p.label}
										size={30}
									/>{/if}
								<span class="label">{p?.label ?? key}</span>
								<button
									type="button"
									class="btn small icon"
									disabled={i === 0}
									aria-label={t('look.dock.up', { name: p?.label ?? key })}
									title={t('look.dock.up', { name: p?.label ?? key })}
									onclick={() => move(i, -1)}><Icon name="arrowUp" size={14} /></button
								>
								<button
									type="button"
									class="btn small icon"
									disabled={i === pinned.length - 1}
									aria-label={t('look.dock.down', { name: p?.label ?? key })}
									title={t('look.dock.down', { name: p?.label ?? key })}
									onclick={() => move(i, 1)}><Icon name="arrowDown" size={14} /></button
								>
								<button
									type="button"
									class="btn small icon"
									aria-label={t('desktop.dock.unpinNamed', { name: p?.label ?? key })}
									title={t('desktop.dock.unpinNamed', { name: p?.label ?? key })}
									onclick={() => remove(key)}><Icon name="close" size={14} /></button
								>
							</li>
						{/each}
					</ol>
				{:else}
					<p class="muted">{t('desktop.dock.empty')}</p>
				{/if}
				{#if addable.length}
					<div class="add">
						<label for="dock-add" class="name">{t('look.dock.add')}</label>
						<select id="dock-add" class="field" bind:value={adding}>
							<option value="">{t('look.dock.choose')}</option>
							{#each addable as p (p.key)}<option value={p.key}>{p.label}</option>{/each}
						</select>
						<button type="button" class="btn" disabled={!adding} onclick={add}
							>{t('look.dock.addButton')}</button
						>
					</div>
				{/if}
				<p class="muted small">{t('look.dock.rightClickHint')}</p>
			</div>
		</section>
	{/if}
</div>

<style>
	.dock-tab {
		display: flex;
		flex-direction: column;
		gap: var(--gap, 16px);
	}
	.stage {
		margin: 16px;
		position: relative;
		/* The dock's own z-index stays inside the preview. */
		isolation: isolate;
		overflow: hidden;
		border-radius: var(--radius-ctl);
		border: 1px solid var(--border);
		filter: none;
	}
	.hidden-note {
		position: absolute;
		inset: 0;
		margin: 0;
		display: grid;
		place-items: center;
		color: #fff;
		text-shadow: 0 1px 3px rgb(0 0 0 / 0.7);
		font-size: var(--text-md);
	}
	.range {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-width: 0;
	}
	.range input {
		flex: 1;
		min-width: 0;
	}
	.value {
		min-width: 56px;
		text-align: right;
		font-size: var(--text-base);
	}
	.check {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-md);
	}
	.pins {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.pins li {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: 6px var(--space-2);
		border-radius: var(--radius-ctl);
		background: var(--chip);
		border: 1px solid transparent;
		cursor: grab;
	}
	.pins li.over {
		border-color: var(--accent);
	}
	.grip {
		color: var(--muted);
		display: grid;
	}
	.label {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: var(--text-md);
	}
	.add {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.add select {
		flex: 1;
		min-width: 160px;
	}
	.small {
		font-size: var(--text-sm);
	}
</style>
