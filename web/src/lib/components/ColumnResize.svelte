<script lang="ts" module>
	// 1.07 (agent side bar) and 1.12 (T-Mail's columns): a column whose
	// width is dragged at its border, per device. An ARIA window splitter:
	// focusable with its value, arrows (Shift: larger steps), Home and End;
	// a double click goes back to the default.

	export const clampWidth = (w: number, min: number, max: number, fallback: number) =>
		Math.round(Math.min(max, Math.max(min, Number.isFinite(w) ? w : fallback)));

	/** The width this browser keeps under `key`; `fallback` without one. */
	export function readWidth(key: string, min: number, max: number, fallback: number): number {
		try {
			const v = Number(localStorage.getItem(key));
			return v ? clampWidth(v, min, max, fallback) : fallback;
		} catch {
			return fallback;
		}
	}
	export function storeWidth(key: string, w: number, fallback: number) {
		try {
			if (w === fallback) localStorage.removeItem(key);
			else localStorage.setItem(key, String(w));
		} catch {
			// Not kept in this browser; it still applies now.
		}
	}
</script>

<script lang="ts">
	type Props = {
		width: number;
		min: number;
		max: number;
		fallback: number;
		/** localStorage key; the width is kept there. */
		key: string;
		label: string;
		hint: string;
		/** Where the border is (CSS length from the left of the positioned parent). */
		left: string;
	};
	let { width = $bindable(), min, max, fallback, key, label, hint, left }: Props = $props();

	const set = (w: number) => {
		width = clampWidth(w, min, max, fallback);
		storeWidth(key, width, fallback);
	};
	let from = $state<{ x: number; w: number } | null>(null);
	function down(e: PointerEvent) {
		if (e.button !== 0) return;
		e.preventDefault();
		from = { x: e.clientX, w: width };
		(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (from) set(from.w + e.clientX - from.x);
	}
	function up() {
		from = null;
	}
	function keydown(e: KeyboardEvent) {
		const step = e.shiftKey ? 48 : 16;
		const next =
			e.key === 'ArrowLeft'
				? width - step
				: e.key === 'ArrowRight'
					? width + step
					: e.key === 'Home'
						? min
						: e.key === 'End'
							? max
							: null;
		if (next === null) return;
		e.preventDefault();
		set(next);
	}
</script>

<!-- Svelte's check does not know focusable separators. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<div
	class="resize"
	class:moving={!!from}
	style:--resize-left={left}
	role="separator"
	aria-orientation="vertical"
	aria-label={label}
	aria-valuenow={width}
	aria-valuemin={min}
	aria-valuemax={max}
	title={hint}
	tabindex="0"
	onpointerdown={down}
	onpointermove={move}
	onpointerup={up}
	onpointercancel={up}
	ondblclick={() => set(fallback)}
	onkeydown={keydown}
></div>

<style>
	.resize {
		position: absolute;
		top: 0;
		bottom: 0;
		left: calc(var(--resize-left) - 6px);
		width: 12px;
		z-index: 3;
		cursor: col-resize;
		touch-action: none;
	}
	/* A finger needs more room. */
	@media (pointer: coarse) {
		.resize {
			left: calc(var(--resize-left) - 22px);
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
