// PLAN 61: all programs of the account – TitanOS programs, Docker apps and
// VM screens – with what their icons and windows need.
import { api } from '../api.ts';
import { t } from '../i18n/index.svelte.ts';
import { session } from '../session.svelte.ts';
import { sideFor, appAllowed, SYSTEM_APPS, type SideItem } from './apps.ts';
import { dockerPrograms, dockerUpdates, sources } from './sources.svelte.ts';
import { openWindow } from './wm.svelte.ts';
import { storage } from '../storage.svelte.ts';
import { readyApps } from '../webapps/webapps.svelte.ts';
import { letterGradient } from '../webapps/protocol.ts';

export type Program = {
	key: string;
	label: string;
	kind: 'system' | 'docker' | 'vm' | 'webapp';
	glyph: string;
	gradient: string;
	image: string;
	href: string;
	side: SideItem[];
	multi: boolean;
	external: boolean;
	/** Number on the badge (updates), a dot (running/stopped) or a warning. */
	badge: { count?: number; dot?: 'on' | 'off'; warn?: boolean; title?: string } | null;
};

export function programs(): Program[] {
	const role = session.data?.user?.role ?? 'user';
	const services = session.services;
	const out: Program[] = SYSTEM_APPS.filter((a) => appAllowed(a, role, services)).map((a) => ({
		key: 'app:' + a.id,
		label: t(a.label),
		kind: 'system' as const,
		glyph: a.glyph,
		gradient: a.gradient,
		image: '',
		href: a.href,
		side: sideFor(a.side, role),
		multi: !!a.multi,
		external: false,
		badge: null,
	}));
	const docker = out.find((p) => p.key === 'app:docker');
	const updates = dockerUpdates();
	if (docker && updates)
		docker.badge = { count: updates, title: t('desktop.badge.updates', { count: updates }) };
	const storageApp = out.find((p) => p.key === 'app:storage');
	const bad = (storage.status?.volumes ?? []).filter((v) => v.state !== 'ok').length;
	if (storageApp && bad)
		storageApp.badge = { warn: true, title: t('desktop.badge.volumes', { count: bad }) };
	const vmsApp = out.find((p) => p.key === 'app:vms');
	const running = sources.vms.filter((v) => v.state === 'running').length;
	if (vmsApp && sources.vms.length)
		vmsApp.badge = {
			dot: running ? 'on' : 'off',
			title: t('desktop.badge.vms', { running, total: sources.vms.length }),
		};
	for (const d of dockerPrograms(location.hostname))
		out.push({
			key: d.key,
			label: d.label,
			kind: 'docker',
			glyph: '',
			gradient: 'blue',
			image: d.icon,
			href: d.url,
			side: [],
			multi: false,
			external: true,
			badge: d.updates
				? { count: d.updates, title: t('desktop.badge.updates', { count: d.updates }) }
				: {
						dot: d.running ? 'on' : 'off',
						title: d.running ? t('desktop.widget.running') : t('desktop.widget.stopped'),
					},
		});
	// PLAN 66: web apps installed on the NAS run in the browser in a window of their own.
	for (const a of readyApps())
		out.push({
			key: 'webapp:' + a.id,
			label: a.name,
			kind: 'webapp',
			glyph: '',
			gradient: letterGradient(a.id),
			image: a.icon ?? '',
			href: a.url,
			side: [],
			multi: true,
			external: false,
			badge: null,
		});
	return out;
}

/** VM screens for the menu (each VM's screen is a window of its own). */
export function vmScreens(): Program[] {
	return sources.vms.map((vm) => ({
		key: 'vm:' + vm.name,
		label: t('desktop.vmScreen', { name: vm.displayName || vm.name }),
		kind: 'vm' as const,
		glyph: 'screen',
		gradient: 'yellow',
		image: '',
		href: '/vms/screen?vm=' + encodeURIComponent(vm.name),
		side: [],
		multi: false,
		external: false,
		badge: { dot: vm.state === 'running' ? 'on' : 'off' },
	}));
}

export function findProgram(key: string): Program | undefined {
	return [...programs(), ...vmScreens()].find((p) => p.key === key);
}

/** A fresh entry for a Docker app (the gate's cookie of a window does not carry over to a tab). */
export async function appEntry(key: string, fallback: string): Promise<string> {
	if (!key.startsWith('docker:')) return fallback;
	try {
		const r = await api.post<{ url: string }>(`/apps/${encodeURIComponent(key.slice(7))}/window`);
		if (r?.url?.startsWith('https://')) return r.url;
	} catch {
		// Older NAS or no HTTPS name: the app's own address.
	}
	return fallback;
}

/** Opens a program in a tab of its own. */
export async function openTab(key: string, href: string) {
	// Opened at once (pop-up blockers), then pointed at the entry.
	const tab = window.open('about:blank', '_blank');
	const url = await appEntry(key, href);
	if (tab) {
		tab.opener = null;
		tab.location.href = url;
	} else window.open(url, '_blank', 'noopener');
}

/** Opens a program; a Docker app's window shows its remote view (PLAN 63). */
export async function openProgram(
	key: string,
	opts: {
		href?: string;
		from?: { x: number; y: number };
		fresh?: boolean;
		/** PLAN 66: the file a web app opens. */
		doc?: { root: string; path: string };
	} = {},
) {
	const p = findProgram(key);
	if (!p) return;
	if (p.kind === 'docker') {
		// The window keeps the app's address for its title bar and "open in a tab".
		openWindow(key, p.href, p.label, { from: opts.from, fresh: opts.fresh });
		return;
	}
	if (p.kind === 'webapp') {
		openWindow(key, p.href, p.label, { from: opts.from, fresh: opts.fresh, doc: opts.doc });
		return;
	}
	openWindow(key, opts.href ?? p.href, p.label, {
		multi: p.multi,
		from: opts.from,
		fresh: opts.fresh,
	});
}
