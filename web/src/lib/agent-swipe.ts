// Swiping a chat aside in the agent app (PLAN 53): with a finger only,
// horizontal moves past 80 px start the action set for that side; the row
// follows the finger and shows what will happen. A long press or a right
// click opens the menu of the chat.

export type SwipeSide = 'left' | 'right';
export type SwipeOptions = {
	enabled: boolean;
	/** Labels for the side the row moves to; '' means nothing happens. */
	left: string;
	right: string;
	onswipe: (side: SwipeSide) => void;
	onmenu: () => void;
};

const THRESHOLD = 80;

export function swipe(node: HTMLElement, options: SwipeOptions) {
	let opts = options;
	let start: { x: number; y: number; id: number } | null = null;
	let dx = 0;
	let horizontal: boolean | null = null;
	let press: ReturnType<typeof setTimeout> | undefined;
	let pressed = false;

	const set = (x: number) => {
		dx = x;
		node.style.setProperty('--dx', `${x}px`);
		const side = x > 0 ? 'right' : x < 0 ? 'left' : '';
		node.dataset.side = side;
		node.dataset.label = side ? opts[side] : '';
		node.dataset.armed = String(Math.abs(x) >= THRESHOLD);
	};
	const reset = () => {
		start = null;
		horizontal = null;
		clearTimeout(press);
		node.classList.remove('dragging');
		set(0);
	};
	function down(e: PointerEvent) {
		if (e.pointerType !== 'touch' || !opts.enabled) return;
		start = { x: e.clientX, y: e.clientY, id: e.pointerId };
		pressed = false;
		press = setTimeout(() => {
			if (start && horizontal === null) {
				pressed = true;
				opts.onmenu();
				reset();
			}
		}, 550);
	}
	function move(e: PointerEvent) {
		if (!start || e.pointerId !== start.id) return;
		const x = e.clientX - start.x;
		const y = e.clientY - start.y;
		if (horizontal === null && Math.hypot(x, y) > 8) {
			horizontal = Math.abs(x) > Math.abs(y) * 1.4;
			clearTimeout(press);
			if (!horizontal) {
				reset();
				return;
			}
			node.classList.add('dragging');
		}
		if (!horizontal) return;
		const side: SwipeSide = x > 0 ? 'right' : 'left';
		const limit = opts[side] ? 140 : 24;
		set(Math.max(-limit, Math.min(limit, x)));
	}
	function up() {
		if (!start) return;
		const x = dx;
		const side: SwipeSide = x > 0 ? 'right' : 'left';
		const fire = horizontal && Math.abs(x) >= THRESHOLD && opts[side];
		if (horizontal) pressed = true;
		reset();
		if (fire) opts.onswipe(side);
	}
	// A swipe or long press must not also open the chat.
	function click(e: MouseEvent) {
		if (pressed) {
			e.preventDefault();
			e.stopPropagation();
			pressed = false;
		}
	}
	function context(e: MouseEvent) {
		e.preventDefault();
		opts.onmenu();
	}
	node.addEventListener('pointerdown', down);
	node.addEventListener('pointermove', move);
	node.addEventListener('pointerup', up);
	node.addEventListener('pointercancel', reset);
	node.addEventListener('click', click, true);
	node.addEventListener('contextmenu', context);
	return {
		update(next: SwipeOptions) {
			opts = next;
		},
		destroy() {
			clearTimeout(press);
			node.removeEventListener('pointerdown', down);
			node.removeEventListener('pointermove', move);
			node.removeEventListener('pointerup', up);
			node.removeEventListener('pointercancel', reset);
			node.removeEventListener('click', click, true);
			node.removeEventListener('contextmenu', context);
		},
	};
}
