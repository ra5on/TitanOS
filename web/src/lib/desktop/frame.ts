// PLAN 61: a page of TitanOS shown in a window of the desktop runs in a
// frame of its own origin. It then shows no head line, side bar or status
// bar of its own and tells the window which page it shows.

/** True inside a window of the desktop (a frame of the same origin). */
export const inWindow: boolean = (() => {
	if (typeof window === 'undefined') return false;
	try {
		return window.self !== window.top && window.top?.location.origin === location.origin;
	} catch {
		// A frame of another origin: not ours.
		return false;
	}
})();

export type FrameMessage =
	| { type: 'titanos-page'; path: string; title: string }
	| { type: 'titanos-goto'; href: string }
	| { type: 'titanos-open'; href: string }
	| { type: 'titanos-upload'; files: File[] }
	| {
			type: 'titanos-key';
			key: string;
			alt: boolean;
			ctrl: boolean;
			shift: boolean;
			meta: boolean;
	  };

/** Tells the window which page the frame shows now. */
export function announcePage(path: string, title: string) {
	if (!inWindow) return;
	window.parent.postMessage(
		{ type: 'titanos-page', path, title } satisfies FrameMessage,
		location.origin,
	);
}

/** Only messages of our own origin from a frame or the top window count. */
export function frameMessage(event: MessageEvent): FrameMessage | null {
	if (event.origin !== location.origin) return null;
	const data = event.data as FrameMessage | undefined;
	if (!data || typeof data !== 'object' || typeof data.type !== 'string') return null;
	if (!data.type.startsWith('titanos-')) return null;
	return data;
}

/** Whether a path is a page of this UI (not an outside address). */
export function ownPath(href: string): boolean {
	return href.startsWith('/') && !href.startsWith('//') && !href.startsWith('/api/');
}
