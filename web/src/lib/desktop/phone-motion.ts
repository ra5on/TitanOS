// PLAN 87: on the phone a program opens out of its symbol and closes back
// into it. A plate in the colour of the page grows from the symbol to the
// whole screen while the program loads, then fades; back on the home screen
// a plate the size of the screen shrinks into the symbol. Nothing moves with
// "Bewegung reduzieren" (system) or with the effect "Seitenübergänge" off.

import { appearance } from '../theme/appearance.svelte.ts';
import { effectOn } from '../theme/themes.ts';
import { motionOK } from '../components/sheet.ts';

let last: { key: string; at: number } | null = null;

export function phoneMotion(): boolean {
	return (
		motionOK() &&
		effectOn(appearance.current, 'transitions') &&
		typeof Element !== 'undefined' &&
		'animate' in Element.prototype
	);
}

function plate(rect: DOMRect | { left: number; top: number; width: number; height: number }) {
	const el = document.createElement('div');
	el.className = 'phone-zoom';
	el.setAttribute('aria-hidden', 'true');
	el.dataset.phoneZoom = '';
	Object.assign(el.style, {
		position: 'fixed',
		left: '0px',
		top: '0px',
		width: '100vw',
		height: '100dvh',
		zIndex: '2147483000',
		pointerEvents: 'none',
		background: 'var(--bg)',
		borderRadius: '0px',
		transformOrigin: '0 0',
	});
	document.body.append(el);
	const sx = rect.width / window.innerWidth;
	const sy = rect.height / window.innerHeight;
	return { el, from: `translate(${rect.left}px, ${rect.top}px) scale(${sx}, ${sy})` };
}

/** A tap on a symbol: the plate grows from it (the page loads under it). */
export function markOpening(key: string, from: HTMLElement | null) {
	last = { key, at: Date.now() };
	if (!from || !phoneMotion()) return;
	const rect = from.getBoundingClientRect();
	const { el, from: start } = plate(rect);
	// Skips the page's cross-fade (+layout.svelte): the plate does it.
	document.documentElement.dataset.phoneZoom = 'open';
	const grow = el.animate(
		[
			{ transform: start, borderRadius: '40px', opacity: 0.6 },
			{ transform: 'none', borderRadius: '0px', opacity: 1 },
		],
		{ duration: 260, easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)', fill: 'forwards' },
	);
	void grow.finished
		.then(
			() =>
				el.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 180, delay: 80, fill: 'forwards' })
					.finished,
		)
		.catch(() => {})
		.finally(() => {
			el.remove();
			delete document.documentElement.dataset.phoneZoom;
		});
}

/** Back on the home screen: the plate shrinks into the symbol of the program left. */
export function closeIntoSymbol(find: (key: string) => HTMLElement | null) {
	const was = last;
	last = null;
	if (!was || !phoneMotion()) return;
	const target = find(was.key);
	if (!target) return;
	const rect = target.getBoundingClientRect();
	if (!rect.width) return;
	const { el } = plate({ left: 0, top: 0, width: window.innerWidth, height: window.innerHeight });
	const end = `translate(${rect.left}px, ${rect.top}px) scale(${rect.width / window.innerWidth}, ${rect.height / window.innerHeight})`;
	const shrink = el.animate(
		[
			{ transform: 'none', borderRadius: '0px', opacity: 1 },
			{ transform: end, borderRadius: '40px', opacity: 0 },
		],
		{ duration: 260, easing: 'cubic-bezier(0.4, 0, 0.2, 1)', fill: 'forwards' },
	);
	void shrink.finished.catch(() => {}).finally(() => el.remove());
}
