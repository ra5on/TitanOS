<script lang="ts" module>
	// 1.07 (owner 17 Nov 2026: "die Seitenleiste … frei in der Breite
	// variieren"): the width of the agent's side bar on the desktop, per
	// device. The default stays the width of before.
	export const SIDE_DEFAULT = 264;
	export const SIDE_MIN = 200;
	export const SIDE_MAX = 480;
	const KEY = 'titanos.agent.sideWidth';

	export const clampSide = (w: number) =>
		Math.round(Math.min(SIDE_MAX, Math.max(SIDE_MIN, Number.isFinite(w) ? w : SIDE_DEFAULT)));

	/** The width this browser keeps; the default without one. */
	export function readSideWidth(): number {
		try {
			const v = Number(localStorage.getItem(KEY));
			return v ? clampSide(v) : SIDE_DEFAULT;
		} catch {
			return SIDE_DEFAULT;
		}
	}
	export function storeSideWidth(w: number) {
		try {
			if (w === SIDE_DEFAULT) localStorage.removeItem(KEY);
			else localStorage.setItem(KEY, String(w));
		} catch {
			// Not kept in this browser; it still applies now.
		}
	}
</script>

<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';

	let { width = $bindable(SIDE_DEFAULT) }: { width?: number } = $props();

	const set = (w: number) => {
		width = clampSide(w);
		storeSideWidth(width);
	};
	let from = $state<{ x: number; w: number } | null>(null);
	function down(e: PointerEvent) {
		if (e.button !== 0) return;
		e.preventDefault();
		from = { x: e.clientX, w: width };
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (from) set(from.w + e.clientX - from.x);
	}
	function up() {
		from = null;
	}
	function key(e: KeyboardEvent) {
		const step = e.shiftKey ? 48 : 16;
		const next =
			e.key === 'ArrowLeft'
				? width - step
				: e.key === 'ArrowRight'
					? width + step
					: e.key === 'Home'
						? SIDE_MIN
						: e.key === 'End'
							? SIDE_MAX
							: null;
		if (next === null) return;
		e.preventDefault();
		set(next);
	}
</script>

<!-- A separator after ARIA (window splitter): focusable, with its value;
     arrows, Home, End. Svelte's check does not know focusable separators. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<div
	class="resize"
	class:moving={!!from}
	role="separator"
	aria-orientation="vertical"
	aria-label={t('agent.sideResize')}
	aria-valuenow={width}
	aria-valuemin={SIDE_MIN}
	aria-valuemax={SIDE_MAX}
	title={t('agent.sideResizeHint')}
	tabindex="0"
	onpointerdown={down}
	onpointermove={move}
	onpointerup={up}
	onpointercancel={up}
	ondblclick={() => set(SIDE_DEFAULT)}
	onkeydown={key}
></div>

<style>
	.resize {
		position: absolute;
		top: 0;
		bottom: 0;
		/* On the border of the side bar; the layout around is relative. */
		left: calc(var(--ag-side-w, 264px) - 6px);
		width: 12px;
		z-index: 3;
		cursor: col-resize;
		touch-action: none;
	}
	/* A finger needs more room. */
	@media (pointer: coarse) {
		.resize {
			left: calc(var(--ag-side-w, 264px) - 22px);
			width: 44px;
		}
	}
	.resize::after {
		content: '';
		position: absolute;
		top: 0;
		bottom: 0;
		left: 50%;
		width: 2px;
		transform: translateX(-1px);
		background: transparent;
		transition: background 0.15s;
	}
	.resize:hover::after,
	.resize.moving::after,
	.resize:focus-visible::after {
		background: var(--accent);
	}
	.resize:focus-visible {
		outline: none;
	}
</style>
