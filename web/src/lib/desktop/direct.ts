// PLAN 75-14: apps that bring their own desktop stream (noVNC, Selkies,
// KasmVNC) open directly in their window, under their own HTTPS name; the
// remote view (Chromium on the NAS) is the fallback and can be chosen per
// app. The choice is kept in this browser, like the remote view's own.

const key = 'titanos.direct';

/** The app streams its desktop itself (GET /apps/mine `stream`). */
export function hasOwnStream(stream: string | undefined): boolean {
	return stream === 'novnc' || stream === 'selkies' || stream === 'kasmvnc';
}

function read(): Record<string, 'direct' | 'remote'> {
	try {
		const raw = JSON.parse(localStorage.getItem(key) ?? '{}');
		return raw && typeof raw === 'object' ? raw : {};
	} catch {
		return {};
	}
}

/** Direct is the standard; "remote" when the account chose the remote view. */
export function directChosen(app: string): boolean {
	return read()[app] !== 'remote';
}

export function chooseDirect(app: string, direct: boolean) {
	try {
		const all = read();
		if (direct) delete all[app];
		else all[app] = 'remote';
		localStorage.setItem(key, JSON.stringify(all));
	} catch {
		// No storage (private window): the choice holds until the reload.
	}
}

/** How long the enter page may take to say it arrived. */
export const DIRECT_WAIT_MS = 8000;

/** The enter page's message (backend/internal/appproxy/direct.go). */
export function isDirectHello(data: unknown): boolean {
	return (
		!!data && typeof data === 'object' && (data as { type?: unknown }).type === 'titanos-direct'
	);
}
