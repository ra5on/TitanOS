// 1.13 (owner 18 Nov 2026): the compact view of Mail, Kontakte and
// Kalender – the project's phone mode, or a window (or browser tab) under
// 620 px. There the programs go level by level (ColumnFrame without
// columns), without cards, with their actions in a bar at the bottom
// (BottomBar) and "Neu" at its right.
import { phoneView } from '../desktop/mode.svelte.ts';

/** Below this width of the window the programs are compact (ColumnFrame folds its side bar). */
export const COMPACT_BELOW = 620;

const narrow = $state({ on: false });
if (typeof matchMedia === 'function') {
	const query = matchMedia(`(max-width: ${COMPACT_BELOW - 1}px)`);
	narrow.on = query.matches;
	query.addEventListener('change', () => (narrow.on = query.matches));
}

/** Whether the page is compact now (phone mode or under 620 px). */
export function compactView(): boolean {
	return phoneView() || narrow.on;
}
