// PLAN 61: what the desktop shows besides its windows – Docker apps as
// programs, badges and the data of the widgets. Asked only while the
// desktop is visible; nothing here wakes disks (the NAS answers from RAM).
import { api, type DockerContainer, type VM } from '../api.ts';
import { appURL } from '../app-links.ts';
import { session } from '../session.svelte.ts';

export type DockerProgram = {
	key: string;
	/** Name of the app (compose project or container). */
	app: string;
	label: string;
	icon: string;
	/** Where its window points; empty without a web interface. */
	url: string;
	running: boolean;
	updates: number;
};

/** GET /apps/mine (PLAN 61-1): the apps this account may use. */
export type MineApp = {
	app: string;
	title?: string;
	icon?: string;
	url: string;
	webui?: string;
	running?: boolean;
};

export const sources = $state<{
	containers: DockerContainer[];
	vms: VM[];
	mine: MineApp[] | null;
	loaded: boolean;
}>({ containers: [], vms: [], mine: null, loaded: false });

const admin = () => ['owner', 'admin'].includes(session.data?.user?.role ?? '');

async function load() {
	const tasks: Promise<unknown>[] = [];
	// Apps the account may use (PLAN 61-1); NAS versions without it fall back to the Docker list.
	tasks.push(
		api
			.get<{ apps: MineApp[] }>('/apps/mine')
			.then((r) => (sources.mine = Array.isArray(r?.apps) ? r.apps : null))
			.catch(() => (sources.mine = null)),
	);
	if (admin() && session.services.docker !== false)
		tasks.push(
			api
				.get<{ containers: DockerContainer[] }>('/docker')
				.then((r) => (sources.containers = r.containers ?? []))
				.catch(() => {}),
		);
	if (admin() && session.services.vms !== false)
		tasks.push(
			api
				.get<{ vms: VM[] }>('/vms')
				.then((r) => (sources.vms = r.vms ?? []))
				.catch(() => {}),
		);
	await Promise.all(tasks);
	sources.loaded = true;
}

/** Keeps the data fresh while the page is visible. */
export function watchSources(): () => void {
	void load();
	const timer = setInterval(() => {
		if (document.visibilityState === 'visible') void load();
	}, 20000);
	const visible = () => document.visibilityState === 'visible' && void load();
	document.addEventListener('visibilitychange', visible);
	return () => {
		clearInterval(timer);
		document.removeEventListener('visibilitychange', visible);
	};
}

/** The Docker apps with a web interface as programs of the desktop. */
export function dockerPrograms(host: string): DockerProgram[] {
	const byApp = new Map<string, DockerContainer[]>();
	for (const c of sources.containers) {
		const key = c.app || c.name;
		byApp.set(key, [...(byApp.get(key) ?? []), c]);
	}
	const out: DockerProgram[] = [];
	if (sources.mine) {
		for (const m of sources.mine) {
			const group = byApp.get(m.app) ?? [];
			out.push({
				key: 'docker:' + m.app,
				app: m.app,
				label: m.title || m.app,
				icon: m.icon || group.find((c) => c.icon)?.icon || '',
				// Without an HTTPS name the app's port (the window explains why it cannot show it).
				url: m.url || (m.webui ? appURL(m.webui, host) : ''),
				running: m.running ?? (group.length ? group.some((c) => c.state === 'running') : true),
				updates: group.filter((c) => c.updateAvailable).length,
			});
		}
		// The NAS lists only apps with a web interface. Their window shows the
		// remote view (PLAN 63), which needs only the app's name: an address the
		// browser cannot use itself (a template like "http://[IP]:8080") must not
		// hide the app (PLAN 64, phone without Docker apps).
		return out.sort((a, b) => a.label.localeCompare(b.label));
	}
	for (const [app, group] of byApp) {
		const web =
			group.find((c) => c.proxyUrl) ?? group.find((c) => c.webui) ?? group.find((c) => c.publicUrl);
		if (!web) continue;
		// The window shows the remote view, which needs only the name (PLAN 63):
		// an address this browser cannot use itself does not hide the app.
		const url = web.proxyUrl || appURL(web.webui, host) || web.publicUrl;
		out.push({
			key: 'docker:' + app,
			app,
			label: app,
			icon: group.find((c) => c.icon)?.icon ?? '',
			url,
			running: group.some((c) => c.state === 'running'),
			updates: group.filter((c) => c.updateAvailable).length,
		});
	}
	return out.sort((a, b) => a.label.localeCompare(b.label));
}

/** Containers with an update waiting (badge of Docker). */
export function dockerUpdates(): number {
	return sources.containers.filter((c) => c.updateAvailable).length;
}
