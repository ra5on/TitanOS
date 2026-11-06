// PLAN 61: which frame TitanOS shows. From 900 px width the desktop with
// windows; below the phone's home grid (a tablet held upright, too). A page
// inside a window of the desktop shows only itself.
import { inWindow } from './frame.ts';

const QUERY = '(min-width: 900px)';

/** The classic frame (head line or side bar) for the UI test scripts of the
 * pages: they set localStorage "titanos.layout" = "classic". Not offered in the UI. */
function classicLayout(): boolean {
	try {
		return localStorage.getItem('titanos.layout') === 'classic';
	} catch {
		return false;
	}
}

export const mode = $state<{ wide: boolean; classic: boolean }>({
	wide: typeof matchMedia === 'function' ? matchMedia(QUERY).matches : true,
	classic: typeof localStorage !== 'undefined' && classicLayout(),
});

if (typeof matchMedia === 'function') {
	matchMedia(QUERY).addEventListener('change', (e) => (mode.wide = e.matches));
}

export { inWindow };

/** PLAN 64: the page is shown full screen on the phone (the bar above names the program). */
export function phoneView(): boolean {
	return !inWindow && !mode.classic && !mode.wide;
}

/** PLAN 64: the page looks like a window app – in a window of the desktop or on the phone. */
export function windowView(): boolean {
	return inWindow || phoneView();
}
