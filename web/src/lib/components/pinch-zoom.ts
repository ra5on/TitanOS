// PLAN 87: zooming with two fingers (pictures, the page of a PDF, the
// preview, the NVR's live picture). On the element that holds the picture:
// two fingers zoom around their middle (1× to 5×), one finger moves the
// zoomed picture, a double tap zooms in to 2.5× there or back to 1×. Not
// zoomed, the page scrolls as before and swipes stay free (e.g. to the next
// camera). The picture moves by `transform` only; the box stays as it is.

export type PinchOptions = {
	/** The element that is scaled; the node's first child without it. */
	target?: () => HTMLElement | null;
	/** Told whenever the zoom changes (1 = not zoomed). */
	onzoom?: (scale: number) => void;
	max?: number;
	/** What the browser may still do with one finger while not zoomed (ab Werk: scroll both ways). */
	idle?: 'pan-x pan-y' | 'pan-y';
};

export function pinchZoom(node: HTMLElement, options: PinchOptions = {}) {
	let opts = options;
	let scale = 1;
	let x = 0;
	let y = 0;
	const pointers = new Map<number, { x: number; y: number }>();
	let start: {
		dist: number;
		scale: number;
		cx: number;
		cy: number;
		x: number;
		y: number;
	} | null = null;
	let pan: { px: number; py: number; x: number; y: number } | null = null;
	let lastTap = { t: -1e9, x: 0, y: 0 };

	const el = () => opts.target?.() ?? (node.firstElementChild as HTMLElement | null);
	const max = () => opts.max ?? 5;

	function apply() {
		const target = el();
		if (!target) return;
		target.style.transformOrigin = '0 0';
		target.style.transform = scale === 1 ? '' : `translate(${x}px, ${y}px) scale(${scale})`;
		node.style.touchAction = scale > 1 ? 'none' : (opts.idle ?? 'pan-x pan-y');
		node.dataset.zoomed = scale > 1 ? 'true' : 'false';
		opts.onzoom?.(scale);
	}
	/** Keeps the picture covering its box: no empty space dragged in. */
	function clamp() {
		const box = node.getBoundingClientRect();
		const minX = box.width - box.width * scale;
		const minY = box.height - box.height * scale;
		x = Math.min(0, Math.max(minX, x));
		y = Math.min(0, Math.max(minY, y));
	}
	/** Zooms to `next` keeping the point (px, py) of the box where it is. */
	function zoomAt(next: number, px: number, py: number) {
		const s = Math.max(1, Math.min(max(), next));
		x = px - ((px - x) / scale) * s;
		y = py - ((py - y) / scale) * s;
		scale = s;
		if (scale <= 1.02) {
			scale = 1;
			x = 0;
			y = 0;
		}
		clamp();
		apply();
	}
	function local(e: PointerEvent) {
		const box = node.getBoundingClientRect();
		return { x: e.clientX - box.left, y: e.clientY - box.top };
	}

	function down(e: PointerEvent) {
		if (e.pointerType === 'mouse') return;
		pointers.set(e.pointerId, local(e));
		if (pointers.size === 2) {
			const [a, b] = [...pointers.values()];
			start = {
				dist: Math.hypot(a.x - b.x, a.y - b.y) || 1,
				scale,
				cx: (a.x + b.x) / 2,
				cy: (a.y + b.y) / 2,
				x,
				y,
			};
			pan = null;
			node.setPointerCapture?.(e.pointerId);
		} else if (pointers.size === 1 && scale > 1) {
			const p = local(e);
			pan = { px: p.x, py: p.y, x, y };
			node.setPointerCapture?.(e.pointerId);
		}
	}
	function move(e: PointerEvent) {
		if (!pointers.has(e.pointerId)) return;
		pointers.set(e.pointerId, local(e));
		if (start && pointers.size >= 2) {
			e.preventDefault();
			const [a, b] = [...pointers.values()];
			const dist = Math.hypot(a.x - b.x, a.y - b.y);
			const cx = (a.x + b.x) / 2;
			const cy = (a.y + b.y) / 2;
			const s = Math.max(1, Math.min(max(), (start.scale * dist) / start.dist));
			// The point under the fingers' middle at the start follows the middle.
			x = cx - ((start.cx - start.x) / start.scale) * s;
			y = cy - ((start.cy - start.y) / start.scale) * s;
			scale = s;
			clamp();
			apply();
		} else if (pan && scale > 1) {
			e.preventDefault();
			const p = local(e);
			x = pan.x + (p.x - pan.px);
			y = pan.y + (p.y - pan.py);
			clamp();
			apply();
		}
	}
	function up(e: PointerEvent) {
		if (!pointers.has(e.pointerId)) return;
		const p = pointers.get(e.pointerId)!;
		pointers.delete(e.pointerId);
		if (pointers.size < 2 && start) {
			start = null;
			if (scale <= 1.02) zoomAt(1, 0, 0);
			return;
		}
		if (pointers.size === 0) {
			pan = null;
			// A double tap: in to 2.5× at the finger, or back.
			const now = e.timeStamp;
			if (now - lastTap.t < 300 && Math.hypot(p.x - lastTap.x, p.y - lastTap.y) < 30) {
				zoomAt(scale > 1 ? 1 : 2.5, p.x, p.y);
				lastTap = { t: -1e9, x: 0, y: 0 };
			} else lastTap = { t: now, x: p.x, y: p.y };
		}
	}

	node.style.touchAction = opts.idle ?? 'pan-x pan-y';
	node.addEventListener('pointerdown', down);
	node.addEventListener('pointermove', move);
	node.addEventListener('pointerup', up);
	node.addEventListener('pointercancel', up);
	return {
		update(next: PinchOptions) {
			opts = next;
		},
		/** Called by the owner when the picture changes: back to 1×. */
		destroy() {
			node.removeEventListener('pointerdown', down);
			node.removeEventListener('pointermove', move);
			node.removeEventListener('pointerup', up);
			node.removeEventListener('pointercancel', up);
		},
	};
}
