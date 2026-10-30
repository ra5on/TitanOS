import type { Role } from './api.ts';
import type { MessageKey } from './i18n/index.svelte.ts';

export type NavItem = { id: string; href: string; label: MessageKey; roles?: Role[] };

/** The tabs of the header in their standard order. A plain user only works with their own shares and files. */
export const NAV: NavItem[] = [
	{ id: 'dashboard', href: '/', label: 'nav.dashboard' },
	{ id: 'storage', href: '/storage', label: 'nav.storage', roles: ['owner', 'admin'] },
	{ id: 'shares', href: '/shares', label: 'nav.shares', roles: ['owner', 'admin'] },
	{ id: 'files', href: '/files', label: 'nav.files' },
	{ id: 'users', href: '/users', label: 'nav.users', roles: ['owner', 'admin'] },
	{ id: 'docker', href: '/docker', label: 'nav.docker', roles: ['owner', 'admin'] },
	{ id: 'vms', href: '/vms', label: 'nav.vms', roles: ['owner', 'admin'] },
	{ id: 'lxc', href: '/lxc', label: 'nav.lxc', roles: ['owner', 'admin'] },
	{ id: 'apps', href: '/apps', label: 'nav.apps', roles: ['owner', 'admin'] },
	{ id: 'plugins', href: '/plugins', label: 'nav.plugins', roles: ['owner', 'admin'] },
	{ id: 'settings', href: '/settings', label: 'nav.settings' },
	{ id: 'tools', href: '/tools', label: 'nav.tools', roles: ['owner', 'admin'] },
];

/** Settings can never be hidden: it is the way back to this choice. */
export const NAV_FIXED = 'settings';

/** Own order and hidden tabs of an account; empty = standard. */
export type NavPrefs = { order: string[]; hidden: string[] };

export const defaultNav = (): NavPrefs => ({ order: [], hidden: [] });

// Unknown ids are kept, so a tab of a newer app version keeps its place
// when an older version saves the appearance in between.
const ID = /^[a-z][a-z0-9-]{0,31}$/;

function ids(value: unknown): string[] {
	if (!Array.isArray(value)) return [];
	return [
		...new Set(value.filter((id): id is string => typeof id === 'string' && ID.test(id))),
	].slice(0, 64);
}

export function sanitizeNav(input: unknown): NavPrefs {
	if (typeof input !== 'object' || input === null) return defaultNav();
	const v = input as Record<string, unknown>;
	return { order: ids(v.order), hidden: ids(v.hidden).filter((id) => id !== NAV_FIXED) };
}

export function navForRole(role: Role): NavItem[] {
	return NAV.filter((item) => !item.roles || item.roles.includes(role));
}

/**
 * The tabs in the account's order, hidden ones included. A tab missing from
 * the saved order (new in an update) goes right after the tab that precedes
 * it in the standard order.
 */
export function arrangeNav(items: NavItem[], prefs: NavPrefs): NavItem[] {
	const byId = new Map(items.map((item) => [item.id, item]));
	const out = prefs.order.flatMap((id) => byId.get(id) ?? []);
	items.forEach((item, index) => {
		if (out.includes(item)) return;
		const before = items
			.slice(0, index)
			.reverse()
			.find((other) => out.includes(other));
		out.splice(before ? out.indexOf(before) + 1 : 0, 0, item);
	});
	return out;
}

/** The tabs shown in the header. */
export function visibleNav(items: NavItem[], prefs: NavPrefs): NavItem[] {
	return arrangeNav(items, prefs).filter(
		(item) => item.id === NAV_FIXED || !prefs.hidden.includes(item.id),
	);
}

/** Moves a tab one place up (-1) or down (+1) and returns the new order of all ids. */
export function moveNav(items: NavItem[], prefs: NavPrefs, id: string, step: -1 | 1): string[] {
	const order = arrangeNav(items, prefs).map((item) => item.id);
	const from = order.indexOf(id);
	const to = from + step;
	if (from >= 0 && to >= 0 && to < order.length)
		[order[from], order[to]] = [order[to], order[from]];
	// Ids of tabs this role does not see keep their saved place at the end.
	return [...order, ...prefs.order.filter((other) => !order.includes(other))];
}
