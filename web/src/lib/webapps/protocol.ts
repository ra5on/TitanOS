// PLAN 66, docs/WEBAPPS.md: web apps that run directly in the browser
// (the Craft programs). The pure part – messages of host.js, which file
// goes where on save, which app opens which file type. No DOM, no API.

export type WebAppState = 'missing' | 'downloading' | 'ready' | 'error';

/** An app of the catalog as GET /api/v1/webapps lists it. */
export type WebApp = {
	id: string;
	name: string;
	version: string;
	installed: boolean;
	state: WebAppState;
	progress?: number;
	error?: string;
	bridge: 'host' | 'adapter';
	types: string[];
	default: boolean;
	url: string;
	/** Our own build (TitanOS-Office): runs on the web UI's origin, without the sandbox. */
	trusted?: boolean;
	/** Our own build that replaces this app (wordcraft → t-word, PLAN 75-20). */
	successor?: string;
	/** Opened without a file it shows nothing: the window asks it for a new document. */
	startNew?: boolean;
	/** Optional: the app's own icon among its files. */
	icon?: string;
	/** Size of the archive in bytes (for the offer to install). */
	size?: number;
	/** Chosen at the first start; installs once there is a volume. */
	pending?: boolean;
};

/** A file of the file manager: its root and the path in it. */
export type DocRef = { root: string; path: string };

export type HostMessage =
	| { type: 'webapp-ready' }
	| { type: 'webapp-save'; name: string; saveAs: boolean; bytes: ArrayBuffer }
	| { type: 'webapp-dirty'; dirty: boolean }
	| { type: 'webapp-failed'; text: string }
	/** The app opened its file picker (an `<input type=file>`): the window shows its own. */
	| { type: 'webapp-pick'; types: string[]; multiple: boolean };

const ID = /^[a-z0-9][a-z0-9-]{0,63}$/;

function isBuffer(value: unknown): value is ArrayBuffer {
	// Across frames instanceof fails; the tag stays.
	return Object.prototype.toString.call(value) === '[object ArrayBuffer]';
}

/** A file name from the app: the last part of a path, no control characters, at most 255 characters. */
export function cleanName(raw: unknown, fallback: string): string {
	const base = (typeof raw === 'string' ? raw : '')
		.split(/[\\/]/)
		.pop()!
		// eslint-disable-next-line no-control-regex
		.replace(/[\u0000-\u001f\u007f]/g, '')
		.trim();
	if (!base || base === '.' || base === '..') return fallback;
	return Array.from(base).slice(0, 255).join('');
}

/** A message of host.js in the app's frame, or null for anything else. */
export function hostMessage(data: unknown): HostMessage | null {
	if (!data || typeof data !== 'object') return null;
	const d = data as Record<string, unknown>;
	switch (d.type) {
		case 'webapp-ready':
			return { type: 'webapp-ready' };
		case 'webapp-save':
			if (!isBuffer(d.bytes)) return null;
			return {
				type: 'webapp-save',
				name: cleanName(d.name, 'document'),
				saveAs: d.saveAs === true,
				bytes: d.bytes,
			};
		case 'webapp-dirty':
			return { type: 'webapp-dirty', dirty: d.dirty === true };
		case 'webapp-failed':
			return {
				type: 'webapp-failed',
				text: typeof d.text === 'string' ? d.text.slice(0, 500) : '',
			};
		case 'webapp-pick':
			return {
				type: 'webapp-pick',
				types: acceptTypes(typeof d.accept === 'string' ? d.accept : ''),
				multiple: d.multiple === true,
			};
		default:
			return null;
	}
}

const IMAGE_TYPES = [
	'png',
	'jpg',
	'jpeg',
	'gif',
	'webp',
	'bmp',
	'svg',
	'tif',
	'tiff',
	'avif',
	'ico',
];
const AUDIO_TYPES = ['wav', 'mp3', 'ogg', 'flac', 'm4a', 'aac', 'opus', 'aif', 'aiff'];
const VIDEO_TYPES = ['mp4', 'mov', 'webm', 'mkv', 'avi', 'm4v'];
const FONT_TYPES = ['ttf', 'otf', 'woff', 'woff2', 'ttc'];

/**
 * The file types an `accept` attribute of a file picker names, as extensions
 * without the dot (`.docx,.doc` → docx, doc; `image/*` → the usual picture
 * types). Anything not understood, or no attribute: [] (every file).
 */
export function acceptTypes(accept: string): string[] {
	const out = new Set<string>();
	for (const raw of accept.split(',')) {
		const token = raw.trim().toLowerCase();
		if (!token) continue;
		if (/^\.[a-z0-9]{1,10}$/.test(token)) out.add(token.slice(1));
		else if (token === 'image/*') IMAGE_TYPES.forEach((x) => out.add(x));
		else if (token === 'audio/*') AUDIO_TYPES.forEach((x) => out.add(x));
		else if (token === 'video/*') VIDEO_TYPES.forEach((x) => out.add(x));
		else if (token.startsWith('font/')) FONT_TYPES.forEach((x) => out.add(x));
		else if (token === 'application/pdf') out.add('pdf');
		else if (/^[a-z]+\/[a-z0-9.+-]+$/.test(token)) {
			const sub = token.split('/')[1].replace(/^x-/, '');
			if (/^[a-z0-9]{1,10}$/.test(sub)) out.add(sub === 'jpeg' ? 'jpg' : sub);
			if (sub === 'jpeg') out.add('jpeg');
		} else return [];
	}
	return [...out];
}

/** The lower-case extension of a file name without the dot ("" without one). */
export function extOf(name: string): string {
	const base = name.split('/').pop() ?? '';
	const dot = base.lastIndexOf('.');
	return dot > 0 ? base.slice(dot + 1).toLowerCase() : '';
}

export function baseName(path: string): string {
	return path.split('/').filter(Boolean).pop() ?? path;
}

export function parentDir(path: string): string {
	const parts = path.split('/').filter(Boolean);
	parts.pop();
	return parts.join('/');
}

/**
 * Where a save of the app goes (PLAN 66):
 * - `original`: Save of the open document – it replaces the file (PUT with expect);
 * - `dialog`: Save As, nothing open yet, or the document in another format
 *   the app edits (GridCraft saves a .csv as .xlsx, WordCraft a .doc as
 *   .docx) – the save dialog, then it is the window's file;
 * - `created`: an export (PDF, picture) or another file of the same format
 *   under another name – never over the original; a notice offers to keep it.
 *
 * An app without the bridge only downloads, so it cannot say "Save" or
 * "Save As": a download under the name of the window's document is its Save
 * and replaces the file (PLAN 79-15: T-Vector and T-Design asked where on
 * every Ctrl+S); every other download asks where.
 */
export function saveTarget(
	doc: DocRef | null,
	save: { name: string; saveAs: boolean },
	app: Pick<WebApp, 'bridge' | 'types'>,
	/** The name the app itself uses for the open document, if it differs from the file's. */
	appName = '',
): 'original' | 'dialog' | 'created' {
	if (!doc) return 'dialog';
	// PLAN 76 (hands-on test): the same format is not yet the same document.
	// A second file of that format – a backup copy next to the project, the
	// next exported picture – must never replace the open one.
	const same = (a: string, b: string) =>
		a.normalize('NFC').toLowerCase() === b.normalize('NFC').toLowerCase();
	const sent = baseName(save.name);
	const own = same(sent, baseName(doc.path)) || (appName !== '' && same(sent, baseName(appName)));
	if (app.bridge === 'adapter') return own ? 'original' : 'dialog';
	if (save.saveAs) return 'dialog';
	const ext = extOf(save.name);
	if (ext === extOf(doc.path)) return own ? 'original' : 'created';
	return app.types.includes(ext) ? 'dialog' : 'created';
}

/**
 * The modification time of a file in nanoseconds since 1970, exact, from the
 * RFC 3339 time the NAS sends (`2026-11-07T09:12:03.123456789+01:00`). A
 * JavaScript Date would lose everything below a millisecond.
 */
export function mtimeNs(modified: string): string | null {
	const m =
		/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(
			modified.trim(),
		);
	if (!m) return null;
	const [, y, mo, d, h, mi, s, frac = '', zone] = m;
	const ms = Date.UTC(+y, +mo - 1, +d, +h, +mi, +s);
	if (!Number.isFinite(ms)) return null;
	let offset = 0;
	if (zone !== 'Z') {
		const sign = zone[0] === '-' ? -1 : 1;
		offset = sign * (+zone.slice(1, 3) * 60 + +zone.slice(4, 6));
	}
	const seconds = BigInt(ms / 1000 - offset * 60);
	return (seconds * 1_000_000_000n + BigInt(frac.padEnd(9, '0') || '0')).toString();
}

/** The modification time a PUT answers with: `modified` (RFC 3339) or `mtime` (ns, string or number). */
export function savedMtime(answer: unknown): string | null {
	if (!answer || typeof answer !== 'object') return null;
	const a = answer as Record<string, unknown>;
	if (typeof a.modified === 'string') {
		const ns = mtimeNs(a.modified);
		if (ns) return ns;
	}
	if (typeof a.mtime === 'string' && /^\d+$/.test(a.mtime)) return a.mtime;
	if (typeof a.mtime === 'number' && Number.isSafeInteger(a.mtime)) return String(a.mtime);
	return null;
}

/** "Angebot.docx" → "Angebot (Kopie).docx", then "(Kopie 2)" … while the name is taken. */
export function copyName(name: string, taken: (n: string) => boolean, word: string): string {
	const dot = name.lastIndexOf('.');
	const stem = dot > 0 ? name.slice(0, dot) : name;
	const ext = dot > 0 ? name.slice(dot) : '';
	for (let i = 1; i < 1000; i++) {
		const next = `${stem} (${i === 1 ? word : `${word} ${i}`})${ext}`;
		if (!taken(next)) return next;
	}
	return `${stem} (${word} ${Date.now()})${ext}`;
}

/** A name for a file in a folder of the NAS: no separators, not empty, not "." or "..". */
export function validFileName(name: string): boolean {
	const n = name.trim();
	return !!n && n !== '.' && n !== '..' && !/[\\/\u0000]/.test(n) && n.length <= 255;
}

// --- Standard apps (preference "fileapps") ----------------------------------

/** "webapp:<id>" for a web app; "editor" for TitanOS itself (preview or text editor). */
export type FileAppChoice = string;
export const EDITOR = 'editor';

export function webAppKey(id: string): string {
	return 'webapp:' + id;
}

/** Only well-formed entries of the stored preference count. */
export function sanitizeFileApps(raw: unknown): Record<string, FileAppChoice> {
	const out: Record<string, FileAppChoice> = {};
	if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return out;
	for (const [ext, value] of Object.entries(raw as Record<string, unknown>)) {
		if (!/^[a-z0-9]{1,16}$/.test(ext) || typeof value !== 'string') continue;
		if (value === EDITOR || (value.startsWith('webapp:') && ID.test(value.slice(7))))
			out[ext] = value;
	}
	return out;
}

/** The installed web apps that open a file type. */
export function appsFor(ext: string, apps: WebApp[]): WebApp[] {
	if (!ext) return [];
	return apps.filter((a) => a.installed && a.state === 'ready' && a.types.includes(ext));
}

/**
 * The default of the catalog: an app of the basic set (WordCraft, GridCraft,
 * PdfCraft); for other types the first installed app, unless TitanOS shows
 * the file itself (pictures, videos, text: `titanosShows`).
 */
export function catalogDefault(ext: string, apps: WebApp[], titanosShows = false): WebApp | null {
	const fit = appsFor(ext, apps);
	return fit.find((a) => a.default) ?? (titanosShows ? null : (fit[0] ?? null));
}

/**
 * What a double click opens: the account's choice when it is still there, else
 * the catalog's default; null = TitanOS itself (preview or text editor).
 */
export function chosenApp(
	ext: string,
	apps: WebApp[],
	prefs: Record<string, FileAppChoice>,
	titanosShows = false,
): FileAppChoice | null {
	const own = prefs[ext];
	if (own === EDITOR) return EDITOR;
	if (own && appsFor(ext, apps).some((a) => webAppKey(a.id) === own)) return own;
	const fallback = catalogDefault(ext, apps, titanosShows);
	return fallback ? webAppKey(fallback.id) : null;
}

/** Every type an installed app opens, sorted, for Settings › Standard apps. */
export function knownTypes(apps: WebApp[]): string[] {
	return [...new Set(apps.filter((a) => a.installed).flatMap((a) => a.types))].sort();
}

/** The address of the app in its frame: the app's files, the user's name and how host.js talks to it. */
export function appSrc(app: WebApp, author: string): string {
	const url = app.url.endsWith('/') ? app.url : app.url + '/';
	const q = new URLSearchParams({ author, bridge: app.bridge, app: app.id });
	return `${url}?${q}`;
}

/** Only addresses of TitanOS's own web app files are framed. */
export function ownAppUrl(url: string): boolean {
	return /^\/webapps\/[a-z0-9][a-z0-9-]*\/[A-Za-z0-9._-]+\/?$/.test(url);
}

/** The gradients of the desktop's icons (DesktopIcon.svelte) used for letter icons. */
const LETTER_GRADIENTS = ['blue', 'green', 'red', 'violet', 'teal', 'orange', 'pink', 'yellow'];
const KNOWN_GRADIENTS: Record<string, string> = {
	wordcraft: 'blue',
	't-word': 'blue',
	't-exel': 'green',
	't-pdf': 'red',
	gridcraft: 'green',
	pdfcraft: 'red',
	deckcraft: 'orange',
	photocraft: 'teal',
	cadcraft: 'violet',
};

/** The gradient of an app's letter icon (first letter of its name on it): fixed per app. */
export function letterGradient(id: string): string {
	if (KNOWN_GRADIENTS[id]) return KNOWN_GRADIENTS[id];
	let h = 0;
	for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
	return LETTER_GRADIENTS[h % LETTER_GRADIENTS.length];
}

/**
 * Whether the files of a web app reach a frame that sends no cookies
 * (PLAN 75-18): Cloudflare Access answers such a request with a redirect to
 * its login. Network errors count as reachable (the frame tells the rest).
 */
export async function probeFiles(url: string, fetcher: typeof fetch = fetch): Promise<boolean> {
	for (const path of ['/webapps/_host/host.js', url]) {
		try {
			const res = await fetcher(path, {
				credentials: 'omit',
				redirect: 'manual',
				cache: 'no-store',
			});
			if (
				res.type === 'opaqueredirect' ||
				(res.status >= 300 && res.status < 400) ||
				res.status === 401 ||
				res.status === 403
			)
				return false;
		} catch {
			return true;
		}
	}
	return true;
}
