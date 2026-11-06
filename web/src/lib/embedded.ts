// Whether this page runs inside a window of the desktop mode (PLAN 61): the
// desktop shows TitanOS pages in an iframe of the same origin. Then the page
// leaves out its own navigation (the window has it). A frame of another
// origin is not "embedded": reading its location throws, which counts as no.
function framedBySameOrigin(): boolean {
	if (typeof window === 'undefined') return false;
	try {
		return window.self !== window.top && window.top?.location.origin === window.location.origin;
	} catch {
		return false;
	}
}

/** True when this page is framed by a page of the same origin (a desktop window). */
export const embedded: boolean = framedBySameOrigin();
