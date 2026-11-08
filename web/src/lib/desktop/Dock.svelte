<script lang="ts" module>
	import { dockPinned } from './prefs.ts';
	import { desktop as store, savePrefs as save } from './wm.svelte.ts';

	/** Keeps a program in the dock (at a place, or at the end). */
	export function dockPin(key: string, known: Set<string>, at?: number) {
		const list = dockPinned(store.prefs, known).filter((k) => k !== key);
		list.splice(at ?? list.length, 0, key);
		save((p) => {
			p.dock.pinned = list;
			p.dock.customPinned = true;
		});
	}
	export function dockUnpin(key: string, known: Set<string>) {
		const list = dockPinned(store.prefs, known).filter((k) => k !== key);
		save((p) => {
			p.dock.pinned = list;
			p.dock.customPinned = true;
		});
	}
</script>

<script lang="ts">
	// PLAN 62-2: the dock – kept programs and open windows at an edge of the
	// desktop. Programs are pinned, moved and removed by dragging; a right click
	// on the dock (not on an icon) opens its own menu with the edit mode for
	// size, opacity, animation and position.
	import { t } from '../i18n/index.svelte.ts';
	import Icon from '../components/Icon.svelte';
	import DesktopIcon from './DesktopIcon.svelte';
	import type { Dock as DockPrefs, DockPosition } from './prefs.ts';
	import type { Program } from './programs.svelte.ts';
	import { desktop, focus, minimize, savePrefs, wm } from './wm.svelte.ts';

	let {
		all,
		edit = false,
		onopen,
		onmenu = () => {},
		ondone = () => {},
		onlauncher,
		launcher = false,
		preview = null,
	}: {
		all: Program[];
		edit?: boolean;
		onopen?: (key: string, event: Event) => void;
		/** Right click: on an icon (key) or on the dock itself (null). */
		onmenu?: (event: MouseEvent, key: string | null) => void;
		ondone?: () => void;
		/** The menu button at the start: all programs (PLAN 69). */
		onlauncher?: () => void;
		/** The menu is open (the button shows it). */
		launcher?: boolean;
		/**
		 * Shows these settings instead of the desktop's, as a still picture
		 * (Settings › Appearance › Dock, PLAN 64): no clicks, drags or menus.
		 */
		preview?: DockPrefs | null;
	} = $props();

	const byKey = $derived(new Map(all.map((p) => [p.key, p])));
	const dock = $derived(preview ?? desktop.prefs.dock);
	const known = $derived(new Set(all.map((p) => p.key)));
	const pinned = $derived(dockPinned({ ...desktop.prefs, dock }, known));
	// Open programs that are not kept come after a line, like on other desktops.
	const running = $derived(
		preview ? [] : [...new Set(wm.windows.map((w) => w.app))].filter((a) => !pinned.includes(a)),
	);
	const vertical = $derived(dock.position !== 'bottom');
	const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;

	let bar = $state<HTMLElement>();
	let scale = $state<Record<string, number>>({});
	let dropAt = $state<number | null>(null);

	// Growing under the pointer: each icon by its distance to the pointer.
	function hover(event: PointerEvent) {
		if (!bar || !dock.magnify || reduced || edit || event.pointerType === 'touch') return;
		const k = dock.magnify === 2 ? 0.75 : 0.38;
		const reach = dock.size * 2.4;
		const next: Record<string, number> = {};
		for (const el of bar.querySelectorAll<HTMLElement>('[data-key]')) {
			const r = el.getBoundingClientRect();
			const centre = vertical ? r.top + r.height / 2 : r.left + r.width / 2;
			const d = Math.abs((vertical ? event.clientY : event.clientX) - centre);
			const f = Math.max(0, 1 - d / reach);
			next[el.dataset.key ?? ''] = 1 + k * Math.sin((f * Math.PI) / 2) ** 2;
		}
		scale = next;
	}

	function windowsOf(key: string) {
		return wm.windows.filter((w) => w.app === key);
	}
	function click(event: MouseEvent, key: string) {
		if (edit || preview) return;
		const list = windowsOf(key);
		if (!list.length) return onopen?.(key, event);
		const front = list.find((w) => w.id === wm.focused && !w.minimized);
		if (front) list.forEach((w) => minimize(w.id));
		else [...list].sort((a, b) => a.z - b.z).forEach((w) => focus(w.id));
	}

	const pin = (key: string, at?: number) => dockPin(key, known, at);
	const unpin = (key: string) => dockUnpin(key, known);

	function indexAt(event: DragEvent): number {
		if (!bar) return pinned.length;
		const items = [...bar.querySelectorAll<HTMLElement>('[data-pinned]')];
		const at = items.findIndex((el) => {
			const r = el.getBoundingClientRect();
			return vertical ? event.clientY < r.top + r.height / 2 : event.clientX < r.left + r.width / 2;
		});
		return at < 0 ? items.length : at;
	}
	function dragOver(event: DragEvent) {
		if (preview) return;
		if (!event.dataTransfer?.types.includes('text/x-titanos-program')) return;
		event.preventDefault();
		event.stopPropagation();
		dropAt = indexAt(event);
	}
	function drop(event: DragEvent) {
		const key = event.dataTransfer?.getData('text/x-titanos-program');
		const at = indexAt(event);
		dropAt = null;
		if (!key || key.startsWith('folder:') || !byKey.has(key)) return;
		event.preventDefault();
		event.stopPropagation();
		const from = pinned.indexOf(key);
		pin(key, from >= 0 && from < at ? at - 1 : at);
	}

	function set<K extends keyof typeof dock>(key: K, value: (typeof dock)[K]) {
		savePrefs((p) => (p.dock[key] = value));
	}
	function reset() {
		savePrefs((p) => {
			p.dock = {
				...p.dock,
				pinned: [],
				customPinned: false,
				size: 52,
				opacity: 90,
				magnify: 1,
			};
		});
	}
	// PLAN 65-5: the arrow keys move along the dock (Tab leaves it).
	function arrows(event: KeyboardEvent) {
		const keys =
			dock.position === 'bottom' ? ['ArrowLeft', 'ArrowRight'] : ['ArrowUp', 'ArrowDown'];
		if (![...keys, 'Home', 'End'].includes(event.key)) return;
		const items = [...(bar?.querySelectorAll<HTMLElement>('button.it') ?? [])];
		const at = items.indexOf(document.activeElement as HTMLElement);
		if (at < 0) return;
		event.preventDefault();
		const next =
			event.key === 'Home'
				? 0
				: event.key === 'End'
					? items.length - 1
					: (at + (event.key === keys[1] ? 1 : items.length - 1)) % items.length;
		items[next]?.focus();
	}
	const positions = [
		['bottom', 'desktop.dock.bottom'],
		['left', 'desktop.dock.left'],
		['right', 'desktop.dock.right'],
	] as const satisfies readonly (readonly [DockPosition, string])[];
</script>

{#snippet item(key: string, kept: boolean, index: number)}
	{@const p = byKey.get(key)}
	{#if p}
		{@const s = scale[key] ?? 1}
		{@const open = windowsOf(key).length > 0}
		<div
			class="slot"
			class:gap={dropAt === index && kept}
			data-key={key}
			data-pinned={kept ? '' : undefined}
			style:--s={s}
		>
			<button
				type="button"
				class="it"
				class:wiggle={edit && !reduced}
				draggable={!preview}
				tabindex={preview ? -1 : undefined}
				ondragstart={(e) => e.dataTransfer?.setData('text/x-titanos-program', key)}
				onclick={(e) => click(e, key)}
				oncontextmenu={(e) => {
					e.preventDefault();
					e.stopPropagation();
					if (!preview) onmenu(e, key);
				}}
				aria-label={p.label}
			>
				<DesktopIcon
					glyph={p.glyph}
					gradient={p.gradient}
					image={p.image}
					name={p.label}
					size={Math.round(dock.size * s)}
				/>
				{#if p.badge?.count}<span class="badge">{p.badge.count}</span>{/if}
			</button>
			{#if !edit}<span class="tip" role="tooltip">{p.label}</span>{/if}
			{#if edit && kept}
				<button
					type="button"
					class="x"
					onclick={() => unpin(key)}
					aria-label={t('desktop.dock.unpinNamed', { name: p.label })}
					><Icon name="close" size={12} /></button
				>
			{/if}
			<span class="dot" class:on={open} aria-hidden="true"></span>
		</div>
	{/if}
{/snippet}

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<nav
	class="dock {dock.position}"
	class:plain={!dock.border}
	class:edit
	class:preview={!!preview}
	aria-hidden={preview ? 'true' : undefined}
	bind:this={bar}
	style:--size="{dock.size}px"
	style:--plate="{dock.opacity}%"
	aria-label={t('desktop.dock.title')}
	onpointermove={hover}
	onpointerleave={() => (scale = {})}
	ondragover={dragOver}
	ondragleave={(e) => {
		if (e.target === bar) dropAt = null;
	}}
	ondrop={drop}
	onkeydown={arrows}
	oncontextmenu={(e) => {
		e.preventDefault();
		if (!preview) onmenu(e, null);
	}}
>
	{#if onlauncher || preview}
		<div class="slot menu-slot">
			<button
				type="button"
				class="it menu-btn"
				class:on={launcher}
				data-opens="launcher"
				tabindex={preview ? -1 : undefined}
				onclick={() => onlauncher?.()}
				oncontextmenu={(e) => {
					e.preventDefault();
					e.stopPropagation();
				}}
				aria-label={t('desktop.dock.menu')}
				aria-expanded={preview ? undefined : launcher}
				><Icon name="grid" size={Math.round(dock.size * 0.46)} /></button
			>
			{#if !edit && !launcher}<span class="tip" role="tooltip">{t('desktop.dock.menu')}</span>{/if}
			<span class="dot" aria-hidden="true"></span>
		</div>
		<span class="line" aria-hidden="true"></span>
	{/if}
	{#each pinned as key, i (key)}{@render item(key, true, i)}{/each}
	{#if dropAt === pinned.length}<span class="drop-end" aria-hidden="true"></span>{/if}
	{#if running.length}<span class="line" aria-hidden="true"></span>{/if}
	{#each running as key, i (key)}{@render item(key, false, pinned.length + 1 + i)}{/each}
	{#if !pinned.length && !running.length}<span class="empty">{t('desktop.dock.empty')}</span>{/if}
</nav>

{#if edit}
	<div
		class="dock-edit glass-panel {dock.position}"
		role="group"
		aria-label={t('desktop.dock.edit')}
	>
		<b>{t('desktop.dock.edit')}</b>
		<p class="hint">{t('desktop.dock.editHint')}</p>
		<label>
			<span>{t('desktop.dock.size')} <em>{dock.size} px</em></span>
			<input
				type="range"
				min="36"
				max="80"
				step="2"
				value={dock.size}
				oninput={(e) => set('size', Number(e.currentTarget.value))}
			/>
		</label>
		<label>
			<span>{t('desktop.dock.opacity')} <em>{dock.opacity} %</em></span>
			<input
				type="range"
				min="30"
				max="100"
				step="5"
				value={dock.opacity}
				oninput={(e) => set('opacity', Number(e.currentTarget.value))}
			/>
		</label>
		<div class="row">
			<span>{t('desktop.dock.magnify')}</span>
			<div class="seg" role="radiogroup" aria-label={t('desktop.dock.magnify')}>
				{#each [[0, 'desktop.dock.magnifyOff'], [1, 'desktop.dock.magnifyLight'], [2, 'desktop.dock.magnifyStrong']] as const as [v, label] (v)}
					<button
						type="button"
						role="radio"
						aria-checked={dock.magnify === v}
						class:on={dock.magnify === v}
						onclick={() => set('magnify', v)}>{t(label)}</button
					>
				{/each}
			</div>
		</div>
		<div class="row">
			<span>{t('desktop.dock.position')}</span>
			<div class="seg" role="radiogroup" aria-label={t('desktop.dock.position')}>
				{#each positions as [v, label] (v)}
					<button
						type="button"
						role="radio"
						aria-checked={dock.position === v}
						class:on={dock.position === v}
						onclick={() => set('position', v)}>{t(label)}</button
					>
				{/each}
			</div>
		</div>
		<div class="actions">
			<button type="button" class="btn" onclick={reset}>{t('desktop.dock.reset')}</button>
			<button type="button" class="btn btn-primary" onclick={ondone}
				>{t('desktop.widgets.done')}</button
			>
		</div>
	</div>
{/if}

<style>
	.dock {
		position: absolute;
		z-index: 8900;
		display: flex;
		align-items: flex-end;
		gap: var(--space-1-5);
		padding: 9px;
		border-radius: calc((var(--size) * 0.36 + 9px) * min(var(--radius-scale, 1), 1.4));
		background: color-mix(in srgb, var(--surface) var(--plate), transparent);
		/* The global "Glaseffekt" (Settings › Appearance, PLAN 64). */
		backdrop-filter: blur(var(--desk-blur, 20px)) saturate(1.3);
		-webkit-backdrop-filter: blur(var(--desk-blur, 20px)) saturate(1.3);
		border: 1px solid color-mix(in srgb, #fff 14%, transparent);
		box-shadow: 0 16px 40px rgb(0 0 0 / 0.4);
		max-width: calc(100% - 24px);
		max-height: calc(100% - 24px);
	}
	.dock.bottom {
		left: 50%;
		bottom: 10px;
		transform: translateX(-50%);
	}
	.dock.left,
	.dock.right {
		top: 50%;
		transform: translateY(-50%);
		flex-direction: column;
		align-items: flex-start;
	}
	.dock.left {
		left: 10px;
	}
	.dock.right {
		right: 10px;
		align-items: flex-end;
	}
	/* In the settings' preview the dock sits in its frame and only shows itself. */
	.dock.preview {
		pointer-events: none;
	}
	.dock.edit {
		outline: 2px dashed color-mix(in srgb, var(--accent) 70%, transparent);
		outline-offset: 3px;
	}
	.slot {
		position: relative;
		display: flex;
		flex-direction: column;
		align-items: center;
		flex: none;
		width: calc(var(--size) * var(--s));
		transition: width 0.08s linear;
	}
	.left .slot,
	.right .slot {
		flex-direction: row;
		width: auto;
		height: calc(var(--size) * var(--s));
	}
	.right .slot {
		flex-direction: row-reverse;
	}
	.slot.gap {
		margin-left: calc(var(--size) * 0.6);
	}
	.left .slot.gap,
	.right .slot.gap {
		margin-left: 0;
		margin-top: calc(var(--size) * 0.6);
	}
	.it {
		position: relative;
		display: grid;
		place-items: center;
		padding: 0;
		border: 0;
		background: none;
		cursor: pointer;
		line-height: 0;
	}
	.it:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 3px;
		border-radius: 12px;
	}
	.wiggle {
		animation: wiggle 0.3s ease-in-out infinite alternate;
	}
	.slot:nth-child(2n) .wiggle {
		animation-delay: 0.15s;
	}
	@keyframes wiggle {
		from {
			transform: rotate(-2.5deg);
		}
		to {
			transform: rotate(2.5deg);
		}
	}
	.menu-btn {
		width: var(--size);
		height: var(--size);
		border-radius: calc(var(--size) * 0.24 * min(var(--radius-scale, 1), 1.4));
		background: color-mix(in srgb, var(--text) 12%, transparent);
		color: var(--text);
		box-shadow: inset 0 0 0 1px color-mix(in srgb, #fff 16%, transparent);
	}
	.menu-btn:hover,
	.menu-btn.on {
		background: color-mix(in srgb, var(--accent) 30%, transparent);
	}
	/* PLAN 73: the "open" dot lies in the plate's padding, so the frame is the
	   same distance from the icons on every side (owner's photo of 8.11.2026). */
	.dot {
		position: absolute;
		left: 50%;
		bottom: -6px;
		width: 4px;
		height: 4px;
		margin-left: -2px;
		border-radius: 50%;
		background: transparent;
		pointer-events: none;
	}
	.left .dot {
		left: -6px;
		top: 50%;
		bottom: auto;
		margin: -2px 0 0;
	}
	.right .dot {
		left: auto;
		right: -6px;
		top: 50%;
		bottom: auto;
		margin: -2px 0 0;
	}
	/* "Umrandung: Ohne" – the icons alone on the desktop. */
	.dock.plain {
		background: none;
		border-color: transparent;
		box-shadow: none;
		backdrop-filter: none;
		-webkit-backdrop-filter: none;
	}
	.dot.on {
		background: var(--text);
	}
	.tip {
		position: absolute;
		bottom: calc(100% + 10px);
		left: 50%;
		transform: translateX(-50%);
		padding: var(--space-1) var(--space-2-5);
		border-radius: 8px;
		background: var(--surface);
		border: 1px solid var(--border);
		color: var(--text);
		font-size: var(--text-sm);
		font-weight: 600;
		white-space: nowrap;
		pointer-events: none;
		opacity: 0;
		transition: opacity 0.1s;
		box-shadow: 0 6px 18px rgb(0 0 0 / 0.35);
	}
	.left .tip,
	.right .tip {
		bottom: auto;
		top: 50%;
		left: calc(100% + 12px);
		transform: translateY(-50%);
	}
	.right .tip {
		left: auto;
		right: calc(100% + 12px);
	}
	.slot:hover .tip,
	.it:focus-visible + .tip {
		opacity: 1;
	}
	.badge {
		position: absolute;
		top: -4px;
		right: -6px;
		min-width: 20px;
		height: 20px;
		padding: 0 5px;
		border-radius: 10px;
		background: #d93a3f;
		color: #fff;
		font-size: 11px;
		font-weight: 700;
		line-height: 20px;
	}
	/* While editing the × sits at the top: the badge moves down out of its way. */
	.edit .badge {
		top: auto;
		bottom: -4px;
	}
	.x {
		position: absolute;
		top: -8px;
		left: -8px;
		width: 22px;
		height: 22px;
		display: grid;
		place-items: center;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: var(--surface);
		color: var(--text);
		box-shadow: 0 0 0 1px var(--border);
		cursor: pointer;
		z-index: 2;
	}
	.line {
		flex: none;
		align-self: stretch;
		width: 1px;
		margin: var(--space-1-5) var(--space-1);
		background: color-mix(in srgb, var(--text) 25%, transparent);
	}
	.left .line,
	.right .line {
		width: auto;
		height: 1px;
		margin: var(--space-1) var(--space-1-5);
	}
	.drop-end {
		flex: none;
		width: calc(var(--size) * 0.6);
		height: var(--size);
	}
	.empty {
		padding: var(--space-2-5) 14px;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.dock-edit {
		position: absolute;
		z-index: 8950;
		width: min(360px, calc(100% - 24px));
		padding: 14px var(--space-4);
		border-radius: 16px;
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		background: color-mix(in srgb, var(--surface) 96%, transparent);
	}
	.dock-edit.bottom {
		left: 50%;
		bottom: calc(var(--dock-room, 90px) + 14px);
		transform: translateX(-50%);
	}
	.dock-edit.left {
		left: calc(var(--dock-room, 90px) + 14px);
		top: 50%;
		transform: translateY(-50%);
	}
	.dock-edit.right {
		right: calc(var(--dock-room, 90px) + 14px);
		top: 50%;
		transform: translateY(-50%);
	}
	.hint {
		margin: 0;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.dock-edit label,
	.row {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-sm);
	}
	.dock-edit label span {
		display: flex;
		justify-content: space-between;
	}
	em {
		font-style: normal;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.seg {
		display: flex;
		gap: var(--space-1);
		padding: 3px;
		border-radius: 10px;
		background: var(--chip);
	}
	.seg button {
		flex: 1;
		padding: var(--space-1-5) var(--space-2);
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.seg button.on {
		background: var(--accent);
		color: var(--accent-contrast, #fff);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
	}
	@media (prefers-reduced-motion: reduce) {
		.slot,
		.tip {
			transition: none;
		}
	}
</style>
