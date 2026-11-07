// PLAN 70: quick actions in the main menu – typing finds not only programs
// and pages but also things to do ("docker neu starten", "backup jetzt").
// Only routes the pages use themselves; only what the account may do (the
// NAS checks again). Disturbing ones ask first (plain dialog, never a name
// to type).
import type { DockerContainer, VM } from '../api.ts';

export type QuickAction = {
	id: string;
	/** Shown text, e.g. "Container neu starten: immich". */
	label: string;
	/** Extra words the search finds it by. */
	words: string;
	icon: string;
	/** POST route, or a local step ("lock", "logout", "restart", "shutdown"). */
	post?: string;
	local?: 'lock' | 'logout' | 'restart' | 'shutdown';
	/** Ask before doing it (stops, shuts down, logs out). */
	confirm?: boolean;
};

type T = (key: string, params?: Record<string, string | number>) => string;

export function quickActions(
	t: T,
	admin: boolean,
	data: { containers: DockerContainer[]; vms: VM[]; backups: string[] },
): QuickAction[] {
	const out: QuickAction[] = [
		{ id: 'lock', label: t('quick.lock'), words: 'sperren lock', icon: 'lock', local: 'lock' },
		{
			id: 'logout',
			label: t('quick.logout'),
			words: 'abmelden logout',
			icon: 'logout',
			local: 'logout',
			confirm: true,
		},
	];
	if (!admin) return out;
	for (const c of data.containers) {
		const name = c.name;
		const at = (action: string) => `/docker/containers/${encodeURIComponent(c.id)}/${action}`;
		if (c.state === 'running') {
			out.push(
				{
					id: `c-restart-${c.id}`,
					label: t('quick.containerRestart', { name }),
					words: `docker container neu starten restart ${name}`,
					icon: 'restart',
					post: at('restart'),
					confirm: true,
				},
				{
					id: `c-stop-${c.id}`,
					label: t('quick.containerStop', { name }),
					words: `docker container stoppen stop ${name}`,
					icon: 'stop',
					post: at('stop'),
					confirm: true,
				},
			);
		} else
			out.push({
				id: `c-start-${c.id}`,
				label: t('quick.containerStart', { name }),
				words: `docker container starten start ${name}`,
				icon: 'play',
				post: at('start'),
			});
	}
	for (const vm of data.vms) {
		const name = vm.displayName || vm.name;
		const at = (action: string) => `/vms/${encodeURIComponent(vm.name)}/${action}`;
		if (vm.state === 'running')
			out.push({
				id: `vm-down-${vm.name}`,
				label: t('quick.vmShutdown', { name }),
				words: `vm maschine herunterfahren shutdown ${name}`,
				icon: 'power',
				post: at('shutdown'),
				confirm: true,
			});
		else
			out.push({
				id: `vm-start-${vm.name}`,
				label: t('quick.vmStart', { name }),
				words: `vm maschine starten start ${name}`,
				icon: 'play',
				post: at('start'),
			});
	}
	for (const id of data.backups)
		out.push({
			id: `b-${id}`,
			label: t('quick.backupRun', { name: id }),
			words: `backup sicherung jetzt sichern ${id}`,
			icon: 'refresh',
			post: `/backups/${encodeURIComponent(id)}/run`,
		});
	out.push(
		{
			id: 'stick',
			label: t('quick.stickBackup'),
			words: 'stick sichern sicherung konfiguration usb',
			icon: 'download',
			post: '/config-backup/run',
		},
		{
			id: 'restart',
			label: t('quick.restart'),
			words: 'nas neu starten reboot restart',
			icon: 'restart',
			local: 'restart',
		},
		{
			id: 'shutdown',
			label: t('quick.shutdown'),
			words: 'nas herunterfahren ausschalten shutdown',
			icon: 'power',
			local: 'shutdown',
		},
	);
	return out;
}

/** The actions whose text has every word of the query. */
export function matchActions(list: QuickAction[], query: string, most = 8): QuickAction[] {
	const words = query.toLowerCase().split(/\s+/).filter(Boolean);
	if (!words.length) return [];
	return list
		.filter((a) => {
			const text = (a.label + ' ' + a.words).toLowerCase();
			return words.every((w) => text.includes(w));
		})
		.slice(0, most);
}
