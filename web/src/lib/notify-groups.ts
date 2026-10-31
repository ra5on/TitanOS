/** Groups of notifications an account can hide from its own bell. */
export const bellGroups = [
	'storage',
	'disks',
	'backups',
	'updates',
	'ups',
	'usb',
	'system',
] as const;
export type BellGroup = (typeof bellGroups)[number];
export type Severity = 'info' | 'warning' | 'error';
export type BellSettings = { minSeverity: Severity; groups: Record<BellGroup, boolean> };

const prefixes: [string, BellGroup][] = [
	['volume_', 'storage'],
	['snapshot_restore_', 'storage'],
	['disk_', 'disks'],
	['backup_', 'backups'],
	['config_backup_', 'backups'],
	['update_', 'updates'],
	['ups_', 'ups'],
	['usb_', 'usb'],
];

/** The group of a kind of notification; unknown kinds belong to system. */
export function bellGroup(kind: string): BellGroup {
	let best: [string, BellGroup] | undefined;
	for (const entry of prefixes)
		if (kind.startsWith(entry[0]) && (!best || entry[0].length > best[0].length)) best = entry;
	return best?.[1] ?? 'system';
}

const rank: Record<Severity, number> = { info: 0, warning: 1, error: 2 };

export function sanitizeBell(value: unknown): BellSettings {
	const raw = (value && typeof value === 'object' ? value : {}) as Record<string, unknown>;
	const minSeverity =
		raw.minSeverity === 'warning' || raw.minSeverity === 'error' ? raw.minSeverity : 'info';
	const stored = (raw.groups && typeof raw.groups === 'object' ? raw.groups : {}) as Record<
		string,
		unknown
	>;
	const groups = Object.fromEntries(
		bellGroups.map((group) => [group, stored[group] !== false]),
	) as Record<BellGroup, boolean>;
	return { minSeverity, groups };
}

/**
 * Whether a notification shows in the bell. Errors about volumes and disks
 * always show: losing data matters more than a quiet bell.
 */
export function bellShows(
	settings: BellSettings,
	notice: { kind: string; severity: Severity },
): boolean {
	const group = bellGroup(notice.kind);
	if (notice.severity === 'error' && (group === 'storage' || group === 'disks')) return true;
	return settings.groups[group] && rank[notice.severity] >= rank[settings.minSeverity];
}
