import type {
	DiskSleepDisk,
	DiskSleepOrigin,
	DiskSleepStatus,
	DiskSleepUnavailable,
} from '#lib/api.ts';
import { t } from '#lib/i18n/index.svelte.ts';

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
