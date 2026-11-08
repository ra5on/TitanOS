// PLAN 61: the programs of the desktop. Every page of TitanOS belongs to
// exactly one program; a program's window shows its pages in a frame and
// lists them in its own side bar.
import type { Role } from '../api.ts';
import type { IconName } from '../components/Icon.svelte';
import type { MessageKey } from '../i18n/index.svelte.ts';

export type SideItem =
	| { group: MessageKey }
	| {
			href: string;
			label: MessageKey;
			icon: IconName;
			match?: string[];
			/** PLAN 69: on this entry's pages the page draws the window's one side
			 * bar itself (the file manager's folder tree); the window shows none. */
			page?: true;
			/** PLAN 74: a command of the program (e.g. "Neue Freigabe"), never the current page. */
			action?: true;
	  }
	/** One thing of the program by name (a VM), with its state as a dot. */
	| { href: string; text: string; state: 'on' | 'off' };

export type Gradient =
	'blue' | 'teal' | 'green' | 'orange' | 'violet' | 'grey' | 'dark' | 'red' | 'yellow' | 'pink';

export type SystemApp = {
	id: string;
	label: MessageKey;
	/** Key of the filled symbol in DesktopIcon. */
	glyph: string;
	gradient: Gradient;
	/** The page a new window opens. */
	href: string;
	/** Paths that belong to this program (prefixes); the first match wins. */
	paths: string[];
	side: SideItem[];
	roles?: Role[];
	/** Needs this service in /services/state (false = switched off). */
	service?: string;
	/** Shown only when the NAS says so (KI-Agent). */
	optIn?: boolean;
	/** Several windows at once (terminal, VM screens). */
	multi?: boolean;
	/** Kind for sorting and the filters of the menu. */
	kind: 'system';
};

const ADMIN: Role[] = ['owner', 'admin'];

export const SYSTEM_APPS: SystemApp[] = [
	{
		id: 'files',
		label: 'desktop.app.files',
		glyph: 'files',
		gradient: 'orange',
		href: '/files',
		paths: ['/files', '/settings/files'],
		side: [
			{ href: '/files', label: 'desktop.side.browse', icon: 'folder', page: true },
			{ group: 'desktop.side.settings' },
			{ href: '/settings/files', label: 'desktop.side.filesSettings', icon: 'gear' },
		],
		kind: 'system',
	},
	{
		id: 'shares',
		label: 'desktop.app.shares',
		glyph: 'shares',
		gradient: 'yellow',
		href: '/shares',
		paths: ['/shares', '/settings/services'],
		side: [
			{ href: '/shares', label: 'desktop.side.shares', icon: 'globe' },
			{ href: '/shares?new=1', label: 'shares.new', icon: 'plus', action: true },
			{ group: 'desktop.side.settings' },
			{ href: '/settings/services', label: 'desktop.side.services', icon: 'gear' },
		],
		roles: ADMIN,
		kind: 'system',
	},
	{
		id: 'storage',
		label: 'desktop.app.storage',
		glyph: 'storage',
		gradient: 'dark',
		href: '/storage',
		paths: ['/storage', '/tools/disks', '/settings/memory'],
		side: [
			{ href: '/storage', label: 'desktop.side.overview', icon: 'disk', match: ['/storage'] },
			{ href: '/storage/manage', label: 'desktop.side.manage', icon: 'tool' },
			{ href: '/storage/smart', label: 'desktop.side.smart', icon: 'activity' },
			{ href: '/storage/import', label: 'desktop.side.import', icon: 'download' },
			{ href: '/storage/replace', label: 'desktop.side.replace', icon: 'restart' },
			{ href: '/tools/disks', label: 'desktop.side.diskTools', icon: 'disc' },
			{ group: 'desktop.side.settings' },
			{ href: '/settings/memory', label: 'desktop.side.memory', icon: 'chip' },
		],
		roles: ADMIN,
		kind: 'system',
	},
	{
		id: 'docker',
		label: 'desktop.app.docker',
		glyph: 'docker',
		gradient: 'blue',
		href: '/docker',
		paths: ['/docker', '/settings/proxy'],
		side: [
			{
				href: '/docker',
				label: 'desktop.side.containers',
				icon: 'box',
				match: ['/docker', '/docker/apps'],
			},
			// PLAN 67: the areas of Docker in the side bar.
			{ href: '/docker/images', label: 'desktop.side.images', icon: 'archive' },
			{ href: '/docker/volumes', label: 'desktop.side.volumes', icon: 'disk' },
			{ href: '/docker/networks', label: 'desktop.side.networks', icon: 'globe' },
			{ href: '/docker/cleanup', label: 'desktop.side.dockerCleanup', icon: 'trash' },
			{ group: 'desktop.side.settings' },
			{ href: '/settings/proxy', label: 'desktop.side.httpsNames', icon: 'lock' },
		],
		roles: ADMIN,
		service: 'docker',
		kind: 'system',
	},
	{
		id: 'vms',
		label: 'desktop.app.vms',
		glyph: 'vm',
		gradient: 'violet',
		href: '/vms',
		paths: ['/vms'],
		side: [
			{
				href: '/vms',
				label: 'desktop.side.machines',
				icon: 'monitor',
				match: ['/vms', '/vms/edit'],
			},
			{ href: '/vms/new', label: 'desktop.side.newVm', icon: 'plus' },
			{ href: '/vms/media', label: 'desktop.side.media', icon: 'disc' },
			{ href: '/vms?adopt=1', label: 'desktop.side.adoptVm', icon: 'download', match: [] },
		],
		roles: ADMIN,
		service: 'vms',
		kind: 'system',
	},
	{
		id: 'lxc',
		label: 'desktop.app.lxc',
		glyph: 'lxc',
		gradient: 'teal',
		href: '/lxc',
		paths: ['/lxc'],
		side: [{ href: '/lxc', label: 'desktop.side.lxc', icon: 'chip' }],
		roles: ADMIN,
		service: 'lxc',
		kind: 'system',
	},
	{
		id: 'appcenter',
		label: 'desktop.app.appcenter',
		glyph: 'store',
		gradient: 'pink',
		href: '/apps',
		paths: ['/apps', '/plugins'],
		side: [
			{
				href: '/apps',
				label: 'desktop.side.catalog',
				icon: 'grid',
				match: ['/apps', '/apps/install'],
			},
			{ href: '/apps/new', label: 'desktop.side.ownApp', icon: 'plus' },
			{ group: 'desktop.side.plugins' },
			{ href: '/plugins', label: 'desktop.side.plugins', icon: 'zap', match: ['/plugins'] },
			{ href: '/plugins/cleanup', label: 'desktop.side.cleanup', icon: 'trash' },
		],
		roles: ADMIN,
		kind: 'system',
	},
	{
		id: 'backup',
		label: 'desktop.app.backup',
		glyph: 'backup',
		gradient: 'green',
		href: '/settings/backups',
		paths: ['/settings/backups'],
		side: [
			{
				href: '/settings/backups',
				label: 'desktop.side.overview',
				icon: 'home',
				match: ['/settings/backups'],
			},
			{ href: '/settings/backups/data', label: 'desktop.side.data', icon: 'folder' },
			{ href: '/settings/backups/applications', label: 'desktop.side.applications', icon: 'box' },
			{
				href: '/settings/backups/configuration',
				label: 'desktop.side.configuration',
				icon: 'gear',
			},
			{ href: '/settings/backups/coverage', label: 'desktop.side.coverage', icon: 'shield' },
			{ href: '/settings/backups/restore', label: 'desktop.side.restore', icon: 'undo' },
		],
		roles: ADMIN,
		kind: 'system',
	},
	{
		id: 'agent',
		label: 'desktop.app.agent',
		glyph: 'agent',
		gradient: 'violet',
		href: '/agent',
		paths: ['/agent', '/settings/agent', '/settings/mcp'],
		side: [],
		optIn: true,
		kind: 'system',
	},
	{
		id: 'monitor',
		label: 'desktop.app.monitor',
		glyph: 'monitor',
		gradient: 'dark',
		href: '/tools/taskmanager',
		paths: [
			'/tools/taskmanager',
			'/tools/monitoring',
			'/tools/tasks',
			'/tools/hardware',
			'/settings/temperatures',
			'/settings/hardware',
		],
		side: [
			{ href: '/tools/taskmanager', label: 'desktop.side.taskManager', icon: 'activity' },
			{ href: '/tools/monitoring', label: 'desktop.side.history', icon: 'clock' },
			{ href: '/tools/tasks', label: 'desktop.side.tasks', icon: 'list' },
			{ href: '/tools/hardware', label: 'desktop.side.hardwareCheck', icon: 'chip' },
			{ group: 'desktop.side.settings' },
			{ href: '/settings/hardware', label: 'desktop.side.hardware', icon: 'gpu' },
			{ href: '/settings/temperatures', label: 'desktop.side.temperatures', icon: 'zap' },
		],
		roles: ADMIN,
		kind: 'system',
	},
	{
		id: 'logs',
		label: 'desktop.app.logs',
		glyph: 'logs',
		gradient: 'grey',
		href: '/tools/logs',
		paths: ['/tools/logs', '/tools/activity', '/tools/bundle'],
		side: [
			{ href: '/tools/logs', label: 'desktop.side.logs', icon: 'fileText' },
			{ href: '/tools/activity', label: 'desktop.side.activity', icon: 'list' },
			{ href: '/tools/bundle', label: 'desktop.side.bundle', icon: 'archive' },
		],
		roles: ADMIN,
		kind: 'system',
	},
	{
		id: 'planner',
		label: 'desktop.app.planner',
		glyph: 'planner',
		gradient: 'teal',
		href: '/tools/scripts',
		paths: ['/tools/scripts', '/tools/maintenance', '/settings/energy', '/tools'],
		side: [
			{ href: '/tools/scripts', label: 'desktop.side.scripts', icon: 'terminal' },
			{ href: '/tools/maintenance', label: 'desktop.side.maintenance', icon: 'tool' },
			{ href: '/settings/energy', label: 'desktop.side.energySchedule', icon: 'clock' },
		],
		roles: ADMIN,
		kind: 'system',
	},
	{
		id: 'terminal',
		label: 'desktop.app.terminal',
		glyph: 'terminal',
		gradient: 'dark',
		href: '/terminal',
		paths: ['/terminal'],
		side: [],
		roles: ADMIN,
		multi: true,
		kind: 'system',
	},
	{
		id: 'users',
		label: 'desktop.app.users',
		glyph: 'users',
		gradient: 'blue',
		href: '/users',
		paths: ['/users', '/settings/access', '/settings/programs'],
		side: [
			{ href: '/users', label: 'desktop.side.accounts', icon: 'users' },
			{ href: '/settings/programs', label: 'desktop.side.programs', icon: 'grid' },
			{ href: '/settings/access', label: 'desktop.side.access', icon: 'key' },
		],
		roles: ADMIN,
		kind: 'system',
	},
	{
		id: 'security',
		label: 'desktop.app.security',
		glyph: 'shield',
		gradient: 'green',
		href: '/settings/firewall',
		paths: ['/settings/firewall'],
		side: [{ href: '/settings/firewall', label: 'desktop.side.firewall', icon: 'shield' }],
		roles: ADMIN,
		kind: 'system',
	},
	{
		id: 'account',
		label: 'desktop.app.account',
		glyph: 'account',
		gradient: 'grey',
		href: '/account',
		paths: ['/account'],
		side: [],
		kind: 'system',
	},
	{
		id: 'settings',
		label: 'desktop.app.settings',
		glyph: 'settings',
		gradient: 'grey',
		href: '/settings',
		paths: ['/settings'],
		side: [
			{ href: '/settings', label: 'desktop.side.allSettings', icon: 'home', match: ['/settings'] },
			{ group: 'desktop.side.system' },
			{ href: '/settings/about', label: 'desktop.side.about', icon: 'home' },
			{ href: '/settings/updates', label: 'desktop.side.updates', icon: 'download' },
			{ href: '/settings/timezone', label: 'desktop.side.timezone', icon: 'clock' },
			{ href: '/settings/appearance', label: 'desktop.side.appearance', icon: 'palette' },
			{ href: '/settings/hardware', label: 'desktop.side.hardware', icon: 'monitor' },
			{ href: '/settings/temperatures', label: 'desktop.side.temperatures', icon: 'activity' },
			{ href: '/settings/energy', label: 'desktop.side.energy', icon: 'zap' },
			{ href: '/settings/ups', label: 'desktop.side.ups', icon: 'battery' },
			{ href: '/settings/power-button', label: 'desktop.side.powerButton', icon: 'power' },
			{ href: '/settings/viewer', label: 'desktop.side.viewer', icon: 'monitor' },
			{ group: 'desktop.side.network' },
			{ href: '/settings/network', label: 'desktop.side.network', icon: 'globe' },
			{ href: '/settings/firewall', label: 'desktop.side.firewall', icon: 'shield' },
			{ href: '/settings/wireguard', label: 'desktop.side.remote', icon: 'key' },
			{ href: '/settings/ddns', label: 'desktop.side.ddns', icon: 'refresh' },
			{ href: '/settings/letsencrypt', label: 'desktop.side.certificates', icon: 'lock' },
			{ href: '/settings/proxy', label: 'desktop.side.httpsNames', icon: 'external' },
			{ group: 'desktop.side.accountsServices' },
			{ href: '/settings/users', label: 'desktop.app.users', icon: 'users' },
			{ href: '/settings/access', label: 'desktop.side.access', icon: 'lock' },
			{ href: '/settings/programs', label: 'desktop.side.programs', icon: 'grid' },
			{ href: '/settings/services', label: 'desktop.side.services', icon: 'gear' },
			{ href: '/settings/notifications', label: 'desktop.side.notifications', icon: 'bell' },
			{ href: '/settings/homeassistant', label: 'desktop.side.homeassistant', icon: 'home' },
			{ href: '/settings/agent', label: 'desktop.side.agent', icon: 'sparkle' },
			{ href: '/settings/mcp', label: 'desktop.side.mcp', icon: 'chat' },
			{ href: '/settings/api', label: 'desktop.side.api', icon: 'terminal' },
			{ href: '/settings/cluster', label: 'desktop.side.cluster', icon: 'columns' },
			{ group: 'desktop.side.dataGroup' },
			{ href: '/settings/backups', label: 'desktop.side.backups', icon: 'archive' },
			{ href: '/settings/files', label: 'desktop.side.filesSettings', icon: 'search' },
			{ href: '/settings/memory', label: 'desktop.side.memory', icon: 'chip' },
		],
		kind: 'system',
	},
];

/** Whether an account and the NAS's services allow a program. */
export function appAllowed(app: SystemApp, role: Role, services: Record<string, boolean>): boolean {
	if (app.roles && !app.roles.includes(role)) return false;
	if (app.optIn) return services[app.id] === true;
	return !app.service || services[app.service] !== false;
}

/** The program a page belongs to: the longest matching path prefix. */
export function appForPath(path: string, apps: SystemApp[] = SYSTEM_APPS): SystemApp | undefined {
	let best: SystemApp | undefined;
	let length = -1;
	for (const app of apps)
		for (const prefix of app.paths) {
			const hit = path === prefix || path.startsWith(prefix + '/');
			if (hit && prefix.length > length) {
				best = app;
				length = prefix.length;
			}
		}
	return best;
}

/** Whether a side bar entry stands for the page shown. */
export function sideActive(item: SideItem, path: string, items: SideItem[]): boolean {
	if (!('href' in item) || 'text' in item || item.action) return false;
	const prefixes = item.match ?? [item.href];
	if (prefixes.some((p) => path === p)) return true;
	// The most specific entry wins when several prefixes match.
	const own = prefixes
		.filter((p) => path.startsWith(p + '/'))
		.sort((a, b) => b.length - a.length)[0];
	if (!own) return false;
	return !items.some(
		(other) =>
			'href' in other &&
			!('text' in other) &&
			!other.action &&
			other !== item &&
			(other.match ?? [other.href]).some(
				(p) => (path === p || path.startsWith(p + '/')) && p.length > own.length,
			),
	);
}

/** PLAN 69: the page at this path draws the window's side bar itself. */
export function pageDrawsSide(items: SideItem[], path: string): boolean {
	return items.some((item) => 'page' in item && item.page && sideActive(item, path, items));
}
