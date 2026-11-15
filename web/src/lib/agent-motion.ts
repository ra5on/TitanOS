// One motion for everything that slides in the KI-Agent (owner 15 Nov
// 2026: the sheets and menus did not feel smooth): a soft, long
// deceleration like the sheets of the phone, the same curve everywhere.
// Only transform and opacity move, so the browser can animate them without
// laying the page out again. "Animationen: Aus" (calm) gives 0 ms.
import { quintOut } from 'svelte/easing';

/** The curve as CSS, for keyframes. */
export const EASE = 'cubic-bezier(0.22, 1, 0.36, 1)';

const ms = (calm: boolean, value: number) => (calm ? 0 : value);

/** A sheet from the bottom of the phone. */
export const sheetUp = (calm = false) => ({
	y: 460,
	duration: ms(calm, 420),
	opacity: 1,
	easing: quintOut,
});
/** A small panel that appears where it stands (desktop). */
export const panelIn = (calm = false, y = 14) => ({
	y,
	duration: ms(calm, 240),
	easing: quintOut,
});
/** A page or drawer from the side; x is where it comes from. */
export const slideIn = (calm = false, x = 380) => ({
	x,
	duration: ms(calm, 400),
	opacity: 1,
	easing: quintOut,
});
/** The dark layer behind a sheet. */
export const dim = (calm = false) => ({ duration: ms(calm, 300) });
