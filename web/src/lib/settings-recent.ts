// "Zuletzt geändert" on the settings overview (PLAN 61): which settings area
// a change in the activity log belongs to. The log records the route pattern
// ("PUT /api/v1/system/timezone"); only an explicit list of settings routes
// counts, so file, container or chat changes never show up there.
import type { ActivityEntry } from './activity.ts';

// Runs, checks and lookups inside a settings area change nothing there.
const notAChange =
	/\/(\{action\}|run|check|probe|preview|restore|scan|discover|call|join-info|test|confirm|revert|applications\/\{step\}|application-recovery)(\/|$)/;

// Longest prefix first; a prefix matches the path itself or anything below it.
const prefixes: [string, string][] = (
	[
		['system/timezone', 'timezone'],
		['system/energy', 'energy'],
		['system/power-schedule', 'energy'],
		['system/power-button', 'power-button'],
		['system/memory', 'memory'],
		['system/mcp', 'mcp'],
		['system/letsencrypt', 'letsencrypt'],
		['system/ssh', 'access'],
		['system/certificate', 'access'],
		['system/login', 'access'],
		['system/webdav', 'access'],
		['terminal', 'access'],
		['system/hostname', 'network'],
		['system/fans', 'temperatures'],
		['network/ddns', 'ddns'],
		['network/ipv64', 'ddns'],
		['ipv64', 'ddns'],
		['network/wireguard', 'wireguard'],
		['network', 'network'],
		['firewall', 'firewall'],
		['update/settings', 'updates'],
		['ups', 'ups'],
		['notifications/settings', 'notifications'],
		['notifications/push', 'notifications'],
		['notifications/temperatures', 'temperatures'],
		['homeassistant/settings', 'homeassistant'],
		['homeassistant/connections', 'homeassistant'],
		['homeassistant/storage', 'homeassistant'],
		['homeassistant/undo', 'homeassistant'],
		['agent/settings', 'agent'],
		['docker/proxy', 'proxy'],
		['services', 'services'],
		['hardware', 'hardware'],
		['backups', 'backups'],
		['config-backup', 'backups'],
		['replication', 'backups'],
		['account/tokens', 'api'],
		['login-appearance', 'appearance'],
		['app-icon', 'appearance'],
		['files/index', 'files'],
		['cluster', 'cluster'],
	] as [string, string][]
).sort((a, b) => b[0].length - a[0].length);

/** The settings area a logged action changed, or null for anything else. */
export function settingsAreaOfAction(action: string): string | null {
	const [method = '', path = ''] = action.split(' ', 2);
	if (!['PUT', 'POST', 'DELETE', 'PATCH'].includes(method)) return null;
	if (!path.startsWith('/api/v1/')) return null;
	const rest = path.slice('/api/v1/'.length);
	if (notAChange.test(`/${rest}`)) return null;
	for (const [prefix, area] of prefixes)
		if (rest === prefix || rest.startsWith(`${prefix}/`)) return area;
	return null;
}

/** The last successful settings changes, newest first, at most `count`. */
export function recentSettingsChanges(
	entries: ActivityEntry[],
	count = 5,
): (ActivityEntry & { area: string })[] {
	return entries
		.filter((entry) => entry.ok)
		.map((entry) => ({ ...entry, area: settingsAreaOfAction(entry.action) ?? '' }))
		.filter((entry) => entry.area)
		.sort((a, b) => b.at.localeCompare(a.at))
		.slice(0, count);
}
