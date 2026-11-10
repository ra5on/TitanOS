// PLAN 61: which frame TitanOS shows. From 900 px width the desktop with
// windows; below the phone's home grid (a tablet held upright, too). A page
// inside a window of the desktop shows only itself.
import { inWindow } from './frame.ts';

const QUERY = '(min-width: 900px)';

/** PLAN 65: the UI test scripts open a page alone, the way a window shows it,
 * without desktop or phone frame: localStorage "titanos.layout" = "bare"
 * ("classic" is the name older scripts use). Not offered in the UI. */
function bareLayout(): boolean {
	try {
		const layout = localStorage.getItem('titanos.layout');
		return layout === 'bare' || layout === 'classic';
	} catch {
		return false;
	}
}

export const mode = $state<{ wide: boolean; bare: boolean }>({
	wide: typeof matchMedia === 'function' ? matchMedia(QUERY).matches : true,
	bare: typeof localStorage !== 'undefined' && bareLayout(),
});

// PLAN 76 (F-12-8): a browser window dragged narrower than 900 px (half the
// screen, a tablet turned upright) must not tear the desktop down: its
// windows hold terminals, uploads and forms. While windows are open the
// desktop stays; the phone's frame comes once the last one is closed.
let holdDesktop: () => boolean = () => false;
/** The desktop says whether it holds windows (lib/desktop/wm.svelte.ts). */
export function setDesktopHold(hold: () => boolean) {
	holdDesktop = hold;
}
/** Follows the width of the browser again (called when the last window closed). */
export function followWidth() {
	if (typeof matchMedia === 'function') mode.wide = matchMedia(QUERY).matches;
}
if (typeof matchMedia === 'function') {
	matchMedia(QUERY).addEventListener('change', (e) => {
		if (e.matches || !mode.wide || !holdDesktop()) mode.wide = e.matches;
	});
}

export { inWindow };

/** PLAN 64: the page is shown full screen on the phone (the bar above names the program). */
export function phoneView(): boolean {
	return !inWindow && !mode.bare && !mode.wide;
}

/** PLAN 64: the page looks like a window app – in a window of the desktop, on
 * the phone, or alone for the test scripts. */
export function windowView(): boolean {
	return inWindow || mode.bare || phoneView();
}

export type Frame = 'window' | 'desktop' | 'phone' | 'bare';

/**
 * PLAN 65: the frame around a page of a logged-in account. `known`: the path
 * belongs to a program of the desktop (or is the desktop itself); `agentApp`:
 * the KI-Agent as an app of its own; `agentAppMode`: installed as one.
 * Pages without a program (/welcome, unknown addresses), the agent app and
 * the test scripts get the bare frame: the page alone.
 */
export function frameFor(o: { known: boolean; agentApp: boolean; agentAppMode: boolean }): Frame {
	if (inWindow) return 'window';
	if (mode.bare || !o.known) return 'bare';
	if (mode.wide) return o.agentAppMode ? 'bare' : 'desktop';
	return o.agentApp ? 'bare' : 'phone';
}
