// PLAN 87: menus and dialogs on the phone come up from the bottom as a
// sheet; a swipe down on the sheet closes it (only from its top, or while
// its content is scrolled to the top). The sheet follows the finger.

/** Whether motion is wanted: "Bewegung reduzieren" of the system turns it off. */
export function motionOK(): boolean {
	return !(
		typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
	);
}

type Options = {
	/** Asked when the finger lets go far enough down; false keeps the sheet (a locked dialog). */
	close: () => void;
	/** The sheet may be swiped away now. */
	enabled?: () => boolean;
};

/**
 * Svelte action on the sheet's element. Uses touch events, so the content
 * of the sheet keeps scrolling natively; the sheet only moves while the
 * finger goes down from content that is at its top.
 */
export function swipeDown(node: HTMLElement, options: Options) {
	let opts = options;
	let startY = 0;
	let startX = 0;
	let startT = 0;
	let dy = 0;
	let active = false;
	let decided = false;

	const scrolledAtTop = (target: EventTarget | null) => {
		let el = target as HTMLElement | null;
		while (el && el !== node) {
			if (el.scrollHeight > el.clientHeight + 1 && el.scrollTop > 0) return false;
			el = el.parentElement;
		}
		return node.scrollTop <= 0;
	};

	function start(e: TouchEvent) {
		// A second finger (zooming) is never a swipe of the sheet.
		if (e.touches.length !== 1) {
			if (active) {
				active = false;
				node.style.transform = '';
			}
			return;
		}
		if (opts.enabled?.() === false) return;
		const target = e.target as HTMLElement;
		// Fields, sliders, lists with their own pull and zoomable pictures keep their gestures.
		if (target.closest('input[type=range], textarea, [data-no-sheet-swipe], [data-pinch]')) return;
		if (!scrolledAtTop(target)) return;
		startY = e.touches[0].clientY;
		startX = e.touches[0].clientX;
		startT = e.timeStamp;
		dy = 0;
		active = true;
		decided = false;
	}
	function move(e: TouchEvent) {
		if (!active) return;
		const y = e.touches[0].clientY - startY;
		const x = e.touches[0].clientX - startX;
		if (!decided) {
			if (Math.abs(y) < 8 && Math.abs(x) < 8) return;
			decided = true;
			// Sideways or upwards: not a swipe of the sheet.
			if (y <= 0 || Math.abs(x) > Math.abs(y)) {
				active = false;
				return;
			}
		}
		dy = Math.max(0, y);
		if (e.cancelable) e.preventDefault();
		node.style.transition = 'none';
		node.style.transform = `translateY(${dy}px)`;
	}
	function end(e: TouchEvent) {
		if (!active) return;
		active = false;
		const fast = dy / Math.max(1, e.timeStamp - startT) > 0.6;
		node.style.transition = motionOK() ? 'transform 0.18s ease-out' : 'none';
		if (decided && (dy > Math.min(120, node.offsetHeight * 0.3) || (fast && dy > 30))) {
			node.style.transform = '';
			opts.close();
		} else node.style.transform = '';
		dy = 0;
	}
	node.addEventListener('touchstart', start, { passive: true });
	node.addEventListener('touchmove', move, { passive: false });
	node.addEventListener('touchend', end);
	node.addEventListener('touchcancel', end);
	return {
		update(next: Options) {
			opts = next;
		},
		destroy() {
			node.removeEventListener('touchstart', start);
			node.removeEventListener('touchmove', move);
			node.removeEventListener('touchend', end);
			node.removeEventListener('touchcancel', end);
		},
	};
}
