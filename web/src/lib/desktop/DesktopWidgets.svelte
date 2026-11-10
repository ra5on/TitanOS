<script lang="ts">
	// The widgets on the desktop and the bar shown while they are edited.
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import {
		CELL,
		menuAt,
		removeWidget,
		resetWidgets,
		widgetLeft,
		type DeskDialog,
		type DeskMenu,
	} from './desk-ops.ts';
	import type { Widget } from './prefs.ts';
	import WidgetView from './WidgetView.svelte';
	import { desktop, savePrefs, wm } from './wm.svelte.ts';

	let {
		menu = $bindable(),
		editWidgets = $bindable(),
		dialog = $bindable(),
		columns,
		open,
	}: {
		menu: DeskMenu | null;
		editWidgets: boolean;
		dialog: DeskDialog | null;
		/** The width of the desktop in cells of the widget grid. */
		columns: number;
		open: (key: string, event?: Event, href?: string) => void;
	} = $props();

	function startWidget(event: PointerEvent, w: Widget, mode: 'move' | 'size') {
		if (!editWidgets || event.button !== 0) return;
		event.preventDefault();
		const start = {
			px: event.clientX,
			py: event.clientY,
			x: w.x < 0 ? columns + w.x : w.x,
			y: w.y,
			w: w.w,
			h: w.h,
		};
		const move = (e: PointerEvent) => {
			const dx = Math.round((e.clientX - start.px) / CELL);
			const dy = Math.round((e.clientY - start.py) / CELL);
			if (mode === 'move') {
				w.x = Math.max(0, Math.min(columns - w.w, start.x + dx));
				w.y = Math.max(0, start.y + dy);
			} else {
				w.w = Math.max(6, Math.min(columns - start.x, start.w + dx));
				w.h = Math.max(3, start.h + dy);
				w.x = start.x;
			}
		};
		const up = () => {
			window.removeEventListener('pointermove', move);
			window.removeEventListener('pointerup', up);
			// Near the right edge it stays anchored there when the window grows.
			if (w.x + w.w >= columns - 1)
				w.x = -w.w - (columns - w.x - w.w > 0 ? columns - w.x - w.w : 0);
			savePrefs((p) => (p.customWidgets = true));
		};
		window.addEventListener('pointermove', move);
		window.addEventListener('pointerup', up);
	}
</script>

{#each desktop.prefs.widgetsHidden ? [] : desktop.prefs.widgets as w (w.id)}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="widget glass-panel"
		class:edit={editWidgets}
		style:left="{widgetLeft(w, columns)}px"
		style:top="{w.y * CELL}px"
		style:width="{w.w * CELL - 10}px"
		style:height="{w.h * CELL - 10}px"
		onpointerdown={(e) => startWidget(e, w, 'move')}
		oncontextmenu={(e) => {
			e.preventDefault();
			e.stopPropagation();
			menu = { ...menuAt(e), kind: 'widget', key: w.id };
		}}
	>
		<WidgetView kind={w.kind} onopen={(key, href) => open(key, undefined, href)} />
		{#if editWidgets}
			<button
				type="button"
				class="w-remove"
				onclick={() => removeWidget(w.id)}
				aria-label={t('desktop.widgets.remove')}><Icon name="close" size={14} /></button
			>
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<span class="w-size" onpointerdown={(e) => (e.stopPropagation(), startWidget(e, w, 'size'))}
			></span>
		{/if}
	</div>
{/each}
{#if editWidgets}
	<!-- Clear of the dock (PLAN 69: the dock covered the hint). -->
	<div
		class="edit-bar glass-panel"
		style:bottom="{wm.inset.b + 18}px"
		style:left="calc(50% + {(wm.inset.l - wm.inset.r) / 2}px)"
		style:max-width="calc(100% - {wm.inset.l + wm.inset.r + 40}px)"
	>
		<span>{t('desktop.widgets.editHint')}</span>
		<button type="button" class="btn" onclick={() => (dialog = 'gallery')}
			><Icon name="plus" size={15} />{t('desktop.widgets.add')}</button
		>
		<button type="button" class="btn" onclick={resetWidgets}>{t('desktop.widgets.reset')}</button>
		<button type="button" class="btn btn-primary" onclick={() => (editWidgets = false)}
			>{t('desktop.widgets.done')}</button
		>
	</div>
{/if}

<style>
	.widget {
		position: absolute;
		border-radius: 18px;
		padding: 14px var(--space-4);
		overflow: auto;
		scrollbar-width: thin;
		z-index: 1;
	}
	.widget.edit {
		outline: 2px dashed color-mix(in srgb, var(--accent) 70%, transparent);
		cursor: move;
		user-select: none;
	}
	.w-remove {
		position: absolute;
		top: 6px;
		right: 6px;
		width: 26px;
		height: 26px;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 50%;
		background: var(--surface);
		color: var(--text);
		cursor: pointer;
	}
	.w-size {
		position: absolute;
		right: 0;
		bottom: 0;
		width: 18px;
		height: 18px;
		border-radius: 4px 0 14px 0;
		background: var(--accent);
		cursor: nwse-resize;
	}
	.edit-bar {
		position: absolute;
		left: 50%;
		bottom: 18px;
		transform: translateX(-50%);
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2-5);
		padding: var(--space-2-5) 14px;
		border-radius: 14px;
		z-index: 8000;
		max-width: calc(100% - 40px);
	}
	.edit-bar .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
</style>
