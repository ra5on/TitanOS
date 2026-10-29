import type { DiskSleepOrigin } from '#lib/api.ts';

/**
 * The idle time a disk gets, as titand resolves it: its own value, else the
 * value of its volume, else the default. 0 means never.
 */
export function effectiveSleep(
	defaultMinutes: number,
	volumes: Record<string, number>,
	volume: string | undefined,
	own: number | undefined,
): { minutes: number; origin: DiskSleepOrigin } {
	if (own !== undefined) return { minutes: own, origin: 'disk' };
	if (volume && volumes[volume] !== undefined)
		return { minutes: volumes[volume], origin: 'volume' };
	return { minutes: defaultMinutes, origin: 'default' };
}

/** Only the overrides of keys that exist now, sorted for a stable comparison. */
export function overridesFrom(
	values: Record<string, number>,
	keys: string[],
): Record<string, number> {
	const result: Record<string, number> = {};
	for (const key of [...keys].sort()) if (values[key] !== undefined) result[key] = values[key];
	return result;
}

/** Long serial numbers keep their end, which tells disks apart. */
export function shortSerial(serial: string): string {
	return serial.length > 12 ? `…${serial.slice(-8)}` : serial;
}
