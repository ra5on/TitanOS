// 1.15 (PLAN 115-5, draft 39-kalender-uebergang.gif): the movements of
// Kalender – a day of the month opens with its week moving up to become
// the week strip while the hours come in from below, a swipe goes to the
// next day or week and the hours slide along. Only with "Animationen: An"
// (Settings › Appearance › Effects, `data-fx-transitions` on <html>) and
// never for prefers-reduced-motion: otherwise everything changes at once.
// Transforms and opacity only (the compositor), through the Web Animations
// API: nothing is laid out again while it moves.

/** Whether Kalender moves now. */
export function motionOn(): boolean {
	if (typeof document === 'undefined' || typeof matchMedia !== 'function') return false;
	if (document.documentElement.dataset.fxTransitions !== 'on') return false;
	return !matchMedia('(prefers-reduced-motion: reduce)').matches;
}

export const EASE = 'cubic-bezier(0.2, 0.8, 0.2, 1)';

/** Runs keyframes on an element when Kalender moves (else nothing). */
export function play(
	el: Element | null | undefined,
	frames: Keyframe[],
	options: KeyframeAnimationOptions = {},
): Animation | null {
	if (!el || !motionOn() || typeof (el as HTMLElement).animate !== 'function') return null;
	return (el as HTMLElement).animate(frames, { duration: 320, easing: EASE, ...options });
}

/**
 * Moves an element from where `from` stood to its own place (FLIP): it
 * starts at the old box and glides home.
 */
export function glideFrom(el: Element | null | undefined, from: DOMRect | null, duration = 340) {
	if (!el || !from) return null;
	const to = el.getBoundingClientRect();
	const dy = from.top - to.top;
	const dx = from.left - to.left;
	if (Math.abs(dy) < 1 && Math.abs(dx) < 1) return null;
	return play(el, [{ transform: `translate(${dx}px, ${dy}px)` }, { transform: 'none' }], {
		duration,
	});
}

/** Where a swipe for "Zurück" of the phone starts (PhoneGestures.EDGE): left alone. */
const EDGE = 24;

export type SwipeOptions = {
	/** The finger moved sideways by dx (0: back to its place). */
	onfollow?: (dx: number) => void;
	/** A swipe: 1 to the next (finger to the left), -1 to the previous. */
	onswipe: (dir: 1 | -1) => void;
	enabled?: boolean;
};

/**
 * Sideways swipes of a finger (or pen) on an element (use:swipe): the
 * element keeps vertical scrolling (touch-action: pan-y); a mouse never
 * swipes (it drags events on the desktop).
 */
export function swipe(node: HTMLElement, initial: SwipeOptions) {
	let opts = initial;
	let start: { id: number; x: number; y: number; side: boolean } | null = null;
	node.style.touchAction = 'pan-y';
	function down(e: PointerEvent) {
		if (opts.enabled === false || e.pointerType === 'mouse' || e.clientX < EDGE) return;
		start = { id: e.pointerId, x: e.clientX, y: e.clientY, side: false };
	}
	function move(e: PointerEvent) {
		if (!start || e.pointerId !== start.id) return;
		const dx = e.clientX - start.x;
		const dy = e.clientY - start.y;
		if (!start.side) {
			if (Math.abs(dy) > 12 && Math.abs(dy) > Math.abs(dx)) {
				start = null;
				return;
			}
			if (Math.abs(dx) < 10 || Math.abs(dx) < Math.abs(dy) * 1.3) return;
			start.side = true;
		}
		opts.onfollow?.(dx);
	}
	function up(e: PointerEvent) {
		if (!start || e.pointerId !== start.id) return;
		const s = start;
		start = null;
		if (!s.side) return;
		const dx = e.clientX - s.x;
		if (Math.abs(dx) > Math.min(60, node.clientWidth / 5)) opts.onswipe(dx < 0 ? 1 : -1);
		else opts.onfollow?.(0);
	}
	function cancel() {
		if (start?.side) opts.onfollow?.(0);
		start = null;
	}
	node.addEventListener('pointerdown', down);
	node.addEventListener('pointermove', move);
	node.addEventListener('pointerup', up);
	node.addEventListener('pointercancel', cancel);
	return {
		update(next: SwipeOptions) {
			opts = next;
		},
		destroy() {
			node.removeEventListener('pointerdown', down);
			node.removeEventListener('pointermove', move);
			node.removeEventListener('pointerup', up);
			node.removeEventListener('pointercancel', cancel);
		},
	};
}
