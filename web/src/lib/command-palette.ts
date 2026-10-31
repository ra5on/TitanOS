import type { MessageKey } from './i18n/index.svelte.ts';
import type { Role } from './api.ts';
import { settingSections, settingsAreas } from './settings-search.ts';

export type PaletteEntry = { href: string; label: string; section: string; terms?: string };
export const pages: { href: string; label: MessageKey; admin?: boolean }[] = [
	{ href: '/', label: 'nav.dashboard' },
	{ href: '/storage', label: 'nav.storage', admin: true },
	{ href: '/shares', label: 'nav.shares', admin: true },
	{ href: '/files', label: 'nav.files' },
	{ href: '/users', label: 'nav.users', admin: true },
	{ href: '/docker', label: 'nav.docker', admin: true },
	{ href: '/vms', label: 'nav.vms', admin: true },
	{ href: '/lxc', label: 'nav.lxc', admin: true },
	{ href: '/apps', label: 'nav.apps', admin: true },
	{ href: '/plugins', label: 'nav.plugins', admin: true },
	{ href: '/settings', label: 'nav.settings' },
	{ href: '/tools', label: 'nav.tools', admin: true },
	{ href: '/tools/logs', label: 'tools.logs', admin: true },
	{ href: '/tools/tasks', label: 'ops.tasks', admin: true },
	{ href: '/tools/maintenance', label: 'ops.maintenance', admin: true },
	{ href: '/tools/monitoring', label: 'ops.monitoring', admin: true },
	{ href: '/tools/activity', label: 'ops.activity', admin: true },
	{ href: '/storage/import', label: 'ops.import', admin: true },
	{ href: '/storage/manage', label: 'ops.usage', admin: true },
	{ href: '/storage/smart', label: 'smart.title', admin: true },
	{ href: '/account', label: 'menu.account' },
];
/** Pages and settings the account may open; the settings list is shared with the settings overview. */
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
			.flatMap((s) => [
				{
					href: `/settings/${s.id}`,
					label: translate(s.label),
					section: translate('nav.settings'),
					terms: s.id,
				},
				...(settingSections[s.id] ?? []).map((p) => ({
					href: p.href,
					label: `${translate(s.label)} › ${translate(p.label)}`,
					section: translate('nav.settings'),
					terms: p.id,
				})),
			]),
	];
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
