import { volumeOf } from '#lib/appform.ts';
import { formatBytes } from '#lib/format.ts';
import { t } from '#lib/i18n/index.svelte.ts';
import { storage } from '#lib/storage.svelte.ts';

/** "Volume Data · 600 GB free" for a folder of the NAS; empty off the volumes. */
export function where(path: string): string {
	const volumes = (storage.status?.volumes ?? []).filter((volume) => volume.mounted);
	const volume = volumeOf(path, volumes);
	return volume
		? t('appform.onVolume', {
				volume: volume.name,
				free: formatBytes(Math.max(0, volume.sizeBytes - volume.usedBytes)),
			})
		: '';
}
