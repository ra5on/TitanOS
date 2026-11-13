import type { MessageKey } from './i18n/index.svelte.ts';
import type { Role } from './api.ts';
import { settingsAreas, settingsKeywords } from './settings-search.ts';
import { findSettings, type SettingsIndexEntry } from './settings-find.ts';

export type PaletteEntry = { href: string; label: string; section: string; terms?: string };
export const pages: { href: string; label: MessageKey; admin?: boolean }[] = [
	{ href: '/', label: 'desktop.phone.home' },
	{ href: '/storage', label: 'nav.storage', admin: true },
	{ href: '/shares', label: 'nav.shares', admin: true },
	{ href: '/files', label: 'nav.files' },
	{ href: '/users', label: 'nav.users', admin: true },
	{ href: '/docker', label: 'nav.docker', admin: true },
	{ href: '/vms', label: 'nav.vms', admin: true },
	{ href: '/lxc', label: 'nav.lxc', admin: true },
	{ href: '/apps', label: 'nav.apps', admin: true },
	{ href: '/plugins', label: 'nav.plugins', admin: true },
	// PLAN 79-7: the NVR and its pages.
	{ href: '/nvr', label: 'nvr.search.live' },
	{ href: '/nvr/recordings', label: 'nvr.search.recordings' },
	{ href: '/nvr/events', label: 'nvr.search.events' },
	{ href: '/nvr/cameras', label: 'nvr.search.cameras', admin: true },
	{ href: '/nvr/cameras?new=1', label: 'nvr.search.add', admin: true },
	{ href: '/nvr/settings', label: 'nvr.search.settings', admin: true },
	{ href: '/nvr/diagnosis', label: 'nvr.search.diagnosis', admin: true },
	{ href: '/settings', label: 'nav.settings' },
	{ href: '/tools', label: 'nav.tools', admin: true },
	{ href: '/tools/logs', label: 'tools.logs', admin: true },
	{ href: '/tools/tasks', label: 'ops.tasks', admin: true },
	{ href: '/tools/taskmanager', label: 'taskmanager.title' },
	{ href: '/tools/maintenance', label: 'ops.maintenance', admin: true },
	{ href: '/tools/scripts', label: 'scripts.title', admin: true },
	{ href: '/tools/monitoring', label: 'ops.monitoring', admin: true },
	{ href: '/tools/activity', label: 'ops.activity', admin: true },
	{ href: '/tools/hardware', label: 'hwcheck.title', admin: true },
	{ href: '/tools/disks', label: 'disktest.title', admin: true },
	{ href: '/tools/bundle', label: 'tools.bundle', admin: true },
	{ href: '/storage/import', label: 'ops.import', admin: true },
	{ href: '/storage/manage', label: 'ops.usage', admin: true },
	{ href: '/storage/smart', label: 'smart.title', admin: true },
	{ href: '/account', label: 'menu.account' },
];
/** Pages and settings pages the account may open; the settings list is shared with the settings overview. */
export function palettePages(role: Role, translate: (key: MessageKey) => string): PaletteEntry[] {
	const admin = role === 'owner' || role === 'admin';
	return [
		...pages
			.filter((p) => !p.admin || admin)
			.map((p) => ({
				href: p.href,
				label: translate(p.label),
				section: translate('palette.pages'),
			})),
		...settingsAreas
			.filter((s) => !s.admin || admin)
			.map((s) => ({
				href: `/settings/${s.id}`,
				label: translate(s.label),
				section: translate('nav.settings'),
				terms: `${s.id} ${settingsKeywords(s.id)}`,
			})),
	];
}

/**
 * PLAN 86: the single settings a query finds – tabs, groups, rows and parts
 * of rows from the same index as the control panel's search (only with a
 * query: hundreds of rows would bury the pages).
 */
export function paletteSettings(
	query: string,
	role: Role,
	translate: (key: MessageKey) => string,
	index: SettingsIndexEntry[] | null,
): PaletteEntry[] {
	if (!query.trim()) return [];
	const admin = role === 'owner' || role === 'admin';
	return findSettings(query, admin, translate, index, 40)
		.filter((hit) => hit.kind !== 'page')
		.map((hit) => ({ href: hit.href, label: hit.label, section: hit.where }));
}
export function findPaletteEntries(entries: PaletteEntry[], query: string): PaletteEntry[] {
	const normalize = (s: string) => s.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase();
	const words = normalize(query).trim().split(/\s+/);
	return entries
		.filter((e) =>
			words.every((w) => normalize(`${e.label} ${e.section} ${e.terms ?? ''}`).includes(w)),
		)
		.slice(0, 60);
}
