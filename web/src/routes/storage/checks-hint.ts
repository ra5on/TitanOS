// One-time hint for volumes without regular checks (plan 7a). New volumes
// get their checks in the create dialog; this covers the ones from before.
import type { Disk, Volume } from '#lib/api.ts';
import type { MaintenanceSettings } from '#lib/operations.ts';

/** What the account stores under the preference kind "hints" (other hints keep their own keys, e.g. PLAN 66 "webapps-0.63"). */
export type Hints = { checksDismissed?: string[]; [other: string]: unknown };

export function sanitizeHints(value: unknown): Hints {
	const list = (value as Hints | null)?.checksDismissed;
	const others =
		value && typeof value === 'object' && !Array.isArray(value)
			? (value as Record<string, unknown>)
			: {};
	return {
		...others,
		checksDismissed: Array.isArray(list)
			? list.filter((name): name is string => typeof name === 'string')
			: [],
	};
}

/**
 * Volumes that no maintenance job checks. A RAID check or Btrfs scrub counts;
 * an mdadm RAID 0 has nothing to compare (storage.Check refuses it), so
 * there a SMART test of one of its disks counts. With maintenance switched
 * off nothing runs, so every volume is unchecked.
 */
export function uncheckedVolumes(
	volumes: Volume[],
	disks: Disk[],
	maintenance: MaintenanceSettings,
): Volume[] {
	const jobs = maintenance.enabled ? maintenance.jobs.filter((job) => job.enabled) : [];
	return volumes.filter((volume) => {
		if (volume.state === 'missing') return false;
		if (volume.level === 'raid0' && volume.filesystem !== 'btrfs') {
			const serials = new Set(
				volume.members
					.map((member) => disks.find((disk) => disk.name === member.disk)?.serial)
					.filter((serial): serial is string => !!serial),
			);
			return !jobs.some(
				(job) =>
					(job.kind === 'smart-short' || job.kind === 'smart-long') && serials.has(job.target),
			);
		}
		return !jobs.some((job) => job.kind === 'raid-check' && job.target === volume.name);
	});
}

/** The unchecked volumes the account has not dismissed the hint for yet. */
export function checksHintVolumes(unchecked: Volume[], hints: Hints): string[] {
	const dismissed = hints.checksDismissed ?? [];
	return unchecked.map((volume) => volume.name).filter((name) => !dismissed.includes(name));
}
