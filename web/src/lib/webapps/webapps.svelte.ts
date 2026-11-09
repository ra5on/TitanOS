// PLAN 66: the web apps of the NAS (GET /api/v1/webapps) and the account's
// standard apps per file type (preference "fileapps").
import { api } from '../api.ts';
import { session } from '../session.svelte.ts';
import { previewKind } from '../file-actions.ts';
import {
	chosenApp,
	sanitizeFileApps,
	webAppKey,
	type FileAppChoice,
	type WebApp,
} from './protocol.ts';

/** A Docker app that is a Craft app the NAS now runs as a web app (update to 0.63). */
export type CraftDocker = { app: string; webapp: string; running: boolean };

/** The last move from Docker apps to web apps (POST /webapps/migrate), while it runs and after. */
export type Migration = {
	running: boolean;
	install: string[];
	items: { app: string; webapp: string; state: string; error?: string }[];
};

export const webapps = $state<{
	apps: WebApp[];
	volume: string;
	docker: CraftDocker[];
	migration: Migration | null;
	loaded: boolean;
	/** The NAS has no web apps (older version or no volume yet). */
	unavailable: boolean;
}>({ apps: [], volume: '', docker: [], migration: null, loaded: false, unavailable: false });

function sanitizeApp(raw: unknown): WebApp | null {
	if (!raw || typeof raw !== 'object') return null;
	const a = raw as Record<string, unknown>;
	if (typeof a.id !== 'string' || !/^[a-z0-9][a-z0-9-]{0,63}$/.test(a.id)) return null;
	const state = ['missing', 'downloading', 'ready', 'error'].includes(a.state as string)
		? (a.state as WebApp['state'])
		: 'missing';
	return {
		id: a.id,
		name: typeof a.name === 'string' && a.name ? a.name : a.id,
		version: typeof a.version === 'string' ? a.version : '',
		installed: a.installed === true,
		state,
		progress: typeof a.progress === 'number' ? a.progress : undefined,
		error: typeof a.error === 'string' ? a.error : undefined,
		bridge: a.bridge === 'adapter' ? 'adapter' : 'host',
		types: Array.isArray(a.types)
			? a.types.filter((x): x is string => typeof x === 'string').map((x) => x.toLowerCase())
			: [],
		default: a.default === true,
		url: typeof a.url === 'string' ? a.url : '',
		icon: typeof a.icon === 'string' && a.icon.startsWith('/webapps/') ? a.icon : undefined,
		size: typeof a.size === 'number' && a.size > 0 ? a.size : undefined,
		pending: a.pending === true,
		// Our own builds (T-Word …): framed without the sandbox (PLAN 75-18).
		trusted: a.trusted === true,
		successor:
			typeof a.successor === 'string' && /^[a-z0-9][a-z0-9-]{0,63}$/.test(a.successor)
				? a.successor
				: undefined,
		startNew: a.startNew === true,
	};
}

let loading: Promise<void> | null = null;
/** Reads the list of the NAS (again). */
export function loadWebApps(): Promise<void> {
	loading ??= api
		.get<{ apps?: unknown[]; volume?: string; docker?: unknown[]; migration?: unknown }>('/webapps')
		.then((r) => {
			webapps.apps = (r?.apps ?? []).map(sanitizeApp).filter((a): a is WebApp => !!a);
			webapps.volume = typeof r?.volume === 'string' ? r.volume : '';
			webapps.docker = (Array.isArray(r?.docker) ? r.docker : [])
				.filter(
					(d): d is CraftDocker =>
						!!d &&
						typeof (d as CraftDocker).app === 'string' &&
						typeof (d as CraftDocker).webapp === 'string',
				)
				.map((d) => ({ app: d.app, webapp: d.webapp, running: d.running === true }));
			const m = r?.migration as Migration | null | undefined;
			webapps.migration =
				m && typeof m === 'object' && Array.isArray(m.items)
					? {
							running: m.running === true,
							install: Array.isArray(m.install) ? m.install : [],
							items: m.items.filter((i) => i && typeof i.app === 'string'),
						}
					: null;
			webapps.unavailable = false;
		})
		.catch(() => {
			webapps.unavailable = true;
		})
		.finally(() => {
			webapps.loaded = true;
			loading = null;
		});
	return loading;
}

/** The installed web apps that can open now. */
export function readyApps(): WebApp[] {
	return webapps.apps.filter((a) => a.installed && a.state === 'ready' && !!a.url);
}

export function webAppById(id: string): WebApp | undefined {
	return webapps.apps.find((a) => a.id === id);
}

/** The account's choices (Dateien › Einstellungen › Standard-Apps). */
export function fileApps(): Record<string, FileAppChoice> {
	return sanitizeFileApps(session.fileapps);
}

/**
 * What a double click on a file of this type opens; null = TitanOS itself.
 * `titanosShows`: TitanOS has a preview or the text editor for it.
 */
export function appForExt(ext: string, titanosShows = false): FileAppChoice | null {
	return chosenApp(ext, webapps.apps, fileApps(), titanosShows);
}

/** TitanOS shows files of this name itself (preview: pictures, PDF, media, text). */
export function titanosShows(name: string): boolean {
	return previewKind(name, 1) !== null;
}

/** What a double click on this file opens; null = TitanOS itself. */
export function appForFile(name: string): FileAppChoice | null {
	const ext = name.includes('.') ? name.split('.').pop()!.toLowerCase() : '';
	return appForExt(ext, titanosShows(name));
}

/** Sets (or with null resets) the standard app of a file type. */
export async function setFileApp(ext: string, choice: FileAppChoice | null) {
	const next = { ...fileApps() };
	if (choice) next[ext] = choice;
	else delete next[ext];
	const before = session.fileapps;
	session.fileapps = next;
	try {
		await api.put('/preferences/fileapps', next);
	} catch (e) {
		session.fileapps = before;
		throw e;
	}
}

export { webAppKey };
