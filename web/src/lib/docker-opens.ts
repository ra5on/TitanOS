// PLAN 72: „Öffnen mit“ für Docker-Apps. titand knows which app opens which
// file types (x-titanos.opens), whether the file lies in a folder of the app
// and starts the program there; this module only asks and shows.
import { api } from './api.ts';
import { session } from './session.svelte.ts';
import { inWindow } from './desktop/frame.ts';
import { openProgram } from './desktop/programs.svelte.ts';

/** A Docker app that can open one file (GET /docker/openers?root=&path=). */
export type FileOpener = {
	app: string;
	name: string;
	running: boolean;
	/** The file lies in a folder of the app. */
	mounted: boolean;
	readOnly?: boolean;
	/** The share of the file, when it is not mounted yet. */
	share?: string;
	/** This account may mount the share into the app. */
	canMount?: boolean;
};

/** The opens block of an app with its folders (Docker › Bearbeiten). */
export type AppOpens = {
	app: string;
	name: string;
	types: string[];
	command: string[];
	user?: string;
	env?: string[];
	service: string;
	hidden?: boolean;
	running: boolean;
	mounts: { source: string; target: string; readOnly: boolean }[];
	error?: string;
};

export const DOCKER_PREFIX = 'docker:';
export { joinCommand, mountedShares, splitCommand, SHARES_TARGET } from './docker-opens-command.ts';

function sanitizeOpener(raw: unknown): FileOpener | null {
	if (!raw || typeof raw !== 'object') return null;
	const o = raw as Record<string, unknown>;
	if (typeof o.app !== 'string' || !/^[a-z0-9][a-z0-9_-]{0,31}$/.test(o.app)) return null;
	return {
		app: o.app,
		name: typeof o.name === 'string' && o.name ? o.name : o.app,
		running: o.running === true,
		mounted: o.mounted === true,
		readOnly: o.readOnly === true,
		share: typeof o.share === 'string' && o.share ? o.share : undefined,
		canMount: o.canMount === true,
	};
}

/** The Docker apps that open this file; none when the NAS cannot say. */
export async function fileOpeners(root: string, path: string): Promise<FileOpener[]> {
	try {
		const list = await api.get<unknown[]>(
			`/docker/openers?root=${encodeURIComponent(root)}&path=${encodeURIComponent(path)}`,
		);
		return (Array.isArray(list) ? list : [])
			.map(sanitizeOpener)
			.filter((o): o is FileOpener => !!o);
	} catch {
		return [];
	}
}

/** Starts the app's program for the file and shows the app's window. */
export async function openInDockerApp(app: string, root: string, path: string) {
	await api.post(`/docker/apps/${encodeURIComponent(app)}/open`, { root, path });
	showDockerApp(app);
}

/** Brings the window of a Docker app forward (or opens it). */
export function showDockerApp(app: string) {
	const key = DOCKER_PREFIX + app;
	if (inWindow) {
		window.parent.postMessage({ type: 'titanos-open-program', key }, location.origin);
		return;
	}
	void openProgram(key);
}

/** Mounts a share into an app; the app's container is created anew. */
export function mountShare(app: string, share: string, readOnly: boolean) {
	return api.post(`/docker/apps/${encodeURIComponent(app)}/shares`, { share, readOnly });
}

export function unmountShare(app: string, share: string) {
	return api.delete(`/docker/apps/${encodeURIComponent(app)}/shares/${encodeURIComponent(share)}`);
}

/**
 * Waits until the file lies in a folder of the app (after mounting, the
 * container is created anew). False after `seconds`.
 */
export async function waitMounted(
	app: string,
	root: string,
	path: string,
	seconds = 90,
	sleep = (ms: number) => new Promise((r) => setTimeout(r, ms)),
): Promise<boolean> {
	const until = Date.now() + seconds * 1000;
	while (Date.now() < until) {
		const o = (await fileOpeners(root, path)).find((x) => x.app === app);
		if (o?.mounted && o.running) return true;
		await sleep(2000);
	}
	return false;
}

/** The account's standard Docker apps ({"3mf": "docker:bambu-lab"}). */
export function dockerFileApps(): Record<string, string> {
	const out: Record<string, string> = {};
	const raw = session.fileappsdocker;
	if (!raw || typeof raw !== 'object') return out;
	for (const [ext, app] of Object.entries(raw))
		if (
			/^[a-z0-9]{1,10}$/.test(ext) &&
			typeof app === 'string' &&
			/^docker:[a-z0-9][a-z0-9_-]{0,31}$/.test(app)
		)
			out[ext] = app;
	return out;
}

/** The standard Docker app of a file type, or null. */
export function dockerAppForExt(ext: string): string | null {
	return dockerFileApps()[ext]?.slice(DOCKER_PREFIX.length) ?? null;
}

/** Sets (or with null resets) the standard Docker app of a file type. */
export async function setDockerFileApp(ext: string, app: string | null) {
	const next = { ...dockerFileApps() };
	if (app) next[ext] = DOCKER_PREFIX + app;
	else delete next[ext];
	const before = session.fileappsdocker;
	session.fileappsdocker = next;
	try {
		await api.put('/preferences/fileappsdocker', next);
	} catch (e) {
		session.fileappsdocker = before;
		throw e;
	}
}

/** The opens block of an app; null when it has none. */
export async function appOpens(app: string): Promise<AppOpens | null> {
	try {
		return await api.get<AppOpens>(`/docker/apps/${encodeURIComponent(app)}/opens`);
	} catch {
		return null;
	}
}

export function setAppOpens(
	app: string,
	opens: Pick<AppOpens, 'types' | 'command' | 'user' | 'env' | 'service' | 'hidden'>,
) {
	return api.put(`/docker/apps/${encodeURIComponent(app)}/opens`, opens);
}
