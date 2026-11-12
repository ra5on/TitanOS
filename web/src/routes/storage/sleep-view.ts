import type {
	DiskSleepDisk,
	DiskWake,
	DiskWakeCause,
	DiskSleepOrigin,
	DiskSleepStatus,
	DiskSleepUnavailable,
} from '#lib/api.ts';
import { formatDateTime, formatTime } from '#lib/format.ts';
import { t, tOr } from '#lib/i18n/index.svelte.ts';

/** "nach 30 Minuten", "Aus" ... for an idle time in minutes. */
export function sleepDuration(minutes: number): string {
	return minutes === 0
		? t('storage.sleepOff')
		: minutes < 60
			? t('storage.sleepMinutes', { count: minutes })
			: minutes === 60
				? t('storage.sleepHour')
				: t('storage.sleepHours', { count: minutes / 60 });
}

export function sleepOrigin(from: DiskSleepOrigin): string {
	return t(
		from === 'disk'
			? 'storage.sleepFromDisk'
			: from === 'volume'
				? 'storage.sleepFromVolume'
				: 'storage.sleepFromDefault',
	);
}

/** True when the sleep status lists volumes and disks (also empty lists). */
export function sleepListed(status: DiskSleepStatus | undefined): boolean {
	return (
		status?.listed === true ||
		(status?.volumeStatus !== undefined && status?.diskStatus !== undefined)
	);
}

export type VolumeSleep = {
	minutes: number;
	origin: DiskSleepOrigin;
	asleep: number;
	total: number;
	disks: DiskSleepDisk[];
};

/** The setting of a volume with how many of its sleepable disks sleep now. */
export function volumeSleep(
	status: DiskSleepStatus | undefined,
	name: string,
): VolumeSleep | undefined {
	if (!sleepListed(status)) return undefined;
	const volume = status?.volumeStatus?.find((entry) => entry.name === name);
	if (!volume) return undefined;
	const disks = (status?.diskStatus ?? []).filter((disk) => disk.volume === name);
	return {
		minutes: volume.effective,
		origin: volume.origin,
		asleep: disks.filter((disk) => disk.state === 'asleep').length,
		total: disks.length,
		disks,
	};
}

/** The sleep entry of a disk by its device name, or why it has none. */
export function diskSleep(
	status: DiskSleepStatus | undefined,
	name: string,
): { disk?: DiskSleepDisk; reason?: DiskSleepUnavailable['reason'] } | undefined {
	if (!sleepListed(status)) return undefined;
	const disk = status?.diskStatus?.find((entry) => entry.name === name);
	if (disk) return { disk };
	const unavailable = status?.unavailable?.find((entry) => entry.name === name);
	return { reason: unavailable?.reason };
}

/**
 * "schläft seit 14:02", "zuletzt geweckt 14:02" or, when titand knows what
 * woke the disk, "aufgeweckt heute 14:02 durch Aktivitätsprotokoll"; with
 * the date when it was not today. undefined when titand knows no time (RAM
 * only, so nothing before its start). `now` is for tests.
 */
export function sleepSince(disk: DiskSleepDisk, now = new Date()): string | undefined {
	if (!disk.since) return undefined;
	const since = new Date(disk.since);
	if (Number.isNaN(since.getTime())) return undefined;
	const today = since.toDateString() === now.toDateString();
	const time = today ? formatTime(since) : formatDateTime(since);
	if (disk.state === 'asleep') return t('storage.sleepAsleepSince', { time });
	const wake = disk.wakes?.[0];
	if (!wake || new Date(wake.at).getTime() !== since.getTime())
		return t('storage.sleepWokenAt', { time });
	return t('storage.sleepWokenBy', { time: wakeTime(wake.at, now), cause: wakeCausesText(wake) });
}

/** "heute 21:25", or date and time when it was not today. */
export function wakeTime(at: string, now = new Date()): string {
	const date = new Date(at);
	if (Number.isNaN(date.getTime())) return '–';
	return date.toDateString() === now.toDateString()
		? t('storage.wakeTodayAt', { time: formatTime(date) })
		: formatDateTime(date);
}

/** The text for one cause: its code translated, with its value where it has one. */
export function wakeCauseText(cause: DiskWakeCause): string {
	const detail = cause.detail ?? '';
	// Maintenance names its kind in the detail (raid, btrfs, check, plan).
	const key =
		cause.code === 'maintenance'
			? `storage.wakeCause.maintenance_${detail}`
			: `storage.wakeCause.${cause.code}`;
	const text = tOr(key, '');
	if (text) return text.replace('{detail}', detail);
	// A code a newer titand knows: shown as it is rather than hidden.
	return t('storage.wakeCause.other', { code: detail ? `${cause.code} ${detail}` : cause.code });
}

/** All causes of a wake in one line. */
export function wakeCausesText(wake: DiskWake): string {
	return wake.causes.map(wakeCauseText).join(', ');
}

/** What TitanOS writes by itself and the owner can move to another volume. */
const SYSTEM_DATA = [
	'journal',
	'activity_log',
	'notifications',
	'smart_history',
	'monitoring',
	'config_backup',
];
/** Likely, not proven: taken from who was connected when nothing of TitanOS touched the volume. */
const LIKELY = ['smb_session', 'docker_running'];

export type WakeHint = 'systemData' | 'unknown' | 'likely';

/** Which hints fit the recent wakes of a disk, the most frequent kind first. */
export function wakeHints(disk: DiskSleepDisk): WakeHint[] {
	const count: Record<WakeHint, number> = { systemData: 0, unknown: 0, likely: 0 };
	for (const wake of disk.wakes ?? []) {
		const codes = wake.causes.map((cause) => cause.code);
		if (codes.some((code) => SYSTEM_DATA.includes(code))) count.systemData++;
		if (codes.includes('unknown')) count.unknown++;
		if (codes.some((code) => LIKELY.includes(code))) count.likely++;
	}
	return (Object.keys(count) as WakeHint[])
		.filter((hint) => count[hint] > 0)
		.sort((a, b) => count[b] - count[a]);
}
