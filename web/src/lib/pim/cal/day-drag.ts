// 1.13 (Baustand step 10): moving an event to another day by dragging it –
// the cells of the month and the all-day row of the week. Used on the
// container (use:dayDrag): an element with data-drag-day (the day it
// stands on) and data-drag-key (its key in the view) is dragged with the
// mouse or a pen (never a finger: that scrolls) onto an element with
// data-day; that one is marked (data-drop) while the pointer is over it.
// A drag of less than 5 px is a click. The keys do the same through the
// event window ("Bearbeiten": date and time), the month says so in its
// hint.
import type { CalEvent } from '../calendar.ts';
import { daysBetween } from '../caldates.ts';

export type DayDragOptions = {
	enabled: boolean;
	/** The event of a data-drag-key. */
	eventOf: (key: string) => CalEvent | undefined;
	/** The event moved by whole days (never 0). */
	onmove: (ev: CalEvent, days: number) => void;
};

export function dayDrag(node: HTMLElement, initial: DayDragOptions) {
	let opts = initial;
	let drag: {
		el: HTMLElement;
		pointer: number;
		x0: number;
		y0: number;
		from: string;
		moved: boolean;
		target: HTMLElement | null;
	} | null = null;
	let swallow = false;

	/** The day under the pointer: bars of the month lie over the days (1.15), so look through them. */
	function dayAt(x: number, y: number): HTMLElement | null {
		const all =
			typeof document.elementsFromPoint === 'function'
				? document.elementsFromPoint(x, y)
				: [document.elementFromPoint(x, y)];
		for (const el of all) {
			const cell = el?.closest<HTMLElement>('[data-day]');
			if (cell && node.contains(cell)) return cell;
		}
		return null;
	}
	function mark(target: HTMLElement | null) {
		if (!drag || drag.target === target) return;
		drag.target?.removeAttribute('data-drop');
		drag.target = target;
		target?.setAttribute('data-drop', '');
	}
	function down(e: PointerEvent) {
		if (!opts.enabled || e.button !== 0 || e.pointerType === 'touch') return;
		const el = (e.target as HTMLElement).closest<HTMLElement>('[data-drag-day]');
		if (!el || !node.contains(el)) return;
		drag = {
			el,
			pointer: e.pointerId,
			x0: e.clientX,
			y0: e.clientY,
			// A bar over several days: the day it was taken at.
			from: dayAt(e.clientX, e.clientY)?.dataset.day ?? el.dataset.dragDay ?? '',
			moved: false,
			target: null,
		};
		el.setPointerCapture?.(e.pointerId);
	}
	function move(e: PointerEvent) {
		if (!drag || e.pointerId !== drag.pointer) return;
		if (!drag.moved && Math.abs(e.clientX - drag.x0) + Math.abs(e.clientY - drag.y0) < 5) return;
		drag.moved = true;
		drag.el.setAttribute('data-dragging', '');
		mark(dayAt(e.clientX, e.clientY));
	}
	function up(e: PointerEvent) {
		if (!drag || e.pointerId !== drag.pointer) return;
		const d = drag;
		mark(null);
		d.el.removeAttribute('data-dragging');
		drag = null;
		if (!d.moved) return;
		swallow = true;
		setTimeout(() => (swallow = false), 0);
		const to = dayAt(e.clientX, e.clientY)?.dataset.day ?? '';
		const ev = opts.eventOf(d.el.dataset.dragKey ?? '');
		if (!to || !ev || !d.from) return;
		const n = daysBetween(d.from, to);
		if (n) opts.onmove(ev, n);
	}
	function cancel() {
		if (!drag) return;
		mark(null);
		drag.el.removeAttribute('data-dragging');
		drag = null;
	}
	/** The click that ends a drag opens nothing. */
	function click(e: MouseEvent) {
		if (!swallow) return;
		e.stopPropagation();
		e.preventDefault();
	}
	node.addEventListener('pointerdown', down);
	node.addEventListener('pointermove', move);
	node.addEventListener('pointerup', up);
	node.addEventListener('pointercancel', cancel);
	node.addEventListener('click', click, true);
	return {
		update(next: DayDragOptions) {
			opts = next;
		},
		destroy() {
			node.removeEventListener('pointerdown', down);
			node.removeEventListener('pointermove', move);
			node.removeEventListener('pointerup', up);
			node.removeEventListener('pointercancel', cancel);
			node.removeEventListener('click', click, true);
		},
	};
}
