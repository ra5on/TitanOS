// PLAN 61: moving a window by its title bar, resizing it at every edge and
// corner, and docking it with Alt + arrows.
import { focus, saveWindows, setSnap, snapAt, clearOfDock, wm, type Win } from './wm.svelte.ts';

const MIN_W = 360;
const MIN_H = 240;
type Rect = { x: number; y: number; w: number; h: number };

export class WindowDrag {
	/** While the window is moved or resized. */
	dragging = $state(false);
	/** How far the window is moved right now (a transform until the pointer lets go). */
	shift = $state({ x: 0, y: 0 });
	readonly #win: () => Win;
	readonly #rect: () => Rect;
	get win() {
		return this.#win();
	}
	get rect() {
		return this.#rect();
	}
	constructor(win: () => Win, rect: () => Rect) {
		this.#win = win;
		this.#rect = rect;
	}

	// Moving: from the title bar; a snapped window comes loose under the pointer.
	startMove = (event: PointerEvent) => {
		if (event.button !== 0 || (event.target as HTMLElement).closest('button, a, input')) return;
		focus(this.win.id);
		const start = { px: event.clientX, py: event.clientY, ...this.rect };
		if (this.win.snap) {
			const ratio = (event.clientX - this.rect.x) / this.rect.w;
			this.win.w = Math.min(this.win.w, wm.area.w);
			start.x = event.clientX - ratio * this.win.w;
			start.w = this.win.w;
			start.h = this.win.h;
		}
		const bounds = (event.currentTarget as HTMLElement)
			.closest('.desk-area')
			?.getBoundingClientRect();
		let moved = false;
		let frameReq = 0;
		let last: PointerEvent | null = null;
		// The window follows the pointer by a transform (no new layout per frame);
		// its place is written once, when the pointer lets go.
		const apply = () => {
			frameReq = 0;
			const e = last;
			if (!e) return;
			const dx = e.clientX - start.px;
			const dy = e.clientY - start.py;
			const x = Math.round(start.x + dx);
			const y = Math.max(0, Math.round(start.y + dy));
			this.shift = { x: x - start.x, y: y - start.y };
			const snap = bounds ? snapAt(e.clientX - bounds.left, e.clientY - bounds.top) : null;
			if (snap !== wm.preview) wm.preview = snap;
		};
		const move = (e: PointerEvent) => {
			const dx = e.clientX - start.px;
			const dy = e.clientY - start.py;
			if (!moved && Math.abs(dx) + Math.abs(dy) < 4) return;
			if (!moved) {
				moved = true;
				this.dragging = true;
				wm.dragging = true;
				if (this.win.snap) {
					// Comes loose under the pointer at its own size.
					Object.assign(this.win, { x: Math.round(start.x), y: start.y, snap: null });
					start.x = this.win.x;
				}
			}
			last = e;
			if (!frameReq) frameReq = requestAnimationFrame(apply);
		};
		const up = () => {
			window.removeEventListener('pointermove', move);
			window.removeEventListener('pointerup', up);
			if (frameReq) cancelAnimationFrame(frameReq);
			if (moved) {
				apply();
				this.win.x = start.x + this.shift.x;
				this.win.y = start.y + this.shift.y;
				// Let go over the dock: the window stays above it (PLAN 71).
				if (!wm.preview) Object.assign(this.win, clearOfDock(this.win));
				this.shift = { x: 0, y: 0 };
			}
			this.dragging = false;
			wm.dragging = false;
			if (wm.preview) setSnap(this.win.id, wm.preview);
			wm.preview = null;
			saveWindows();
		};
		window.addEventListener('pointermove', move);
		window.addEventListener('pointerup', up);
	};

	// Resizing at every edge and corner.
	startResize = (event: PointerEvent, edge: string) => {
		if (event.button !== 0) return;
		event.preventDefault();
		event.stopPropagation();
		focus(this.win.id);
		const start = { px: event.clientX, py: event.clientY, ...this.rect };
		this.win.snap = null;
		Object.assign(this.win, { x: start.x, y: start.y, w: start.w, h: start.h });
		this.dragging = true;
		wm.dragging = true;
		const move = (e: PointerEvent) => {
			const dx = e.clientX - start.px;
			const dy = e.clientY - start.py;
			if (edge.includes('e')) this.win.w = Math.max(MIN_W, start.w + dx);
			if (edge.includes('s'))
				this.win.h = Math.max(MIN_H, Math.min(start.h + dy, wm.area.h - wm.inset.b - start.y));
			if (edge.includes('w')) {
				const w = Math.max(MIN_W, start.w - dx);
				this.win.x = start.x + start.w - w;
				this.win.w = w;
			}
			if (edge.includes('n')) {
				const h = Math.max(MIN_H, start.h - dy);
				const y = Math.max(0, start.y + start.h - h);
				this.win.h = start.y + start.h - y;
				this.win.y = y;
			}
		};
		const up = () => {
			window.removeEventListener('pointermove', move);
			window.removeEventListener('pointerup', up);
			this.dragging = false;
			wm.dragging = false;
			saveWindows();
		};
		window.addEventListener('pointermove', move);
		window.addEventListener('pointerup', up);
	};

	keyResize = (event: KeyboardEvent) => {
		// Alt + arrows dock the window in front (also announced in the switcher).
		if (!event.altKey) return;
		const map: Record<string, Parameters<typeof setSnap>[1]> = {
			ArrowLeft: 'left',
			ArrowRight: 'right',
			ArrowUp: 'max',
			ArrowDown: null,
		};
		if (event.key in map) {
			event.preventDefault();
			setSnap(this.win.id, map[event.key]);
		}
	};
}
