import { i18n, t } from './i18n/index.svelte.ts';

function number(value: number, maximumFractionDigits: number): string {
	return new Intl.NumberFormat(i18n.locale, { maximumFractionDigits }).format(value);
}

const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

/** 3328599654 → "3,1 GB". Binary steps (1024), like the tools on the NAS itself. */
export function formatBytes(bytes: number): string {
	let value = bytes;
	let unit = 0;
	while (value >= 1024 && unit < UNITS.length - 1) {
		value /= 1024;
		unit++;
	}
	const digits = unit === 0 || value >= 100 ? 0 : value >= 10 ? 1 : 2;
	return `${number(value, digits)} ${UNITS[unit]}`;
}

/** "3,1 / 16 GB": both values in the unit of the larger one. */
export function formatBytesOf(used: number, total: number): string {
	let unit = 0;
	let scale = 1;
	while (total / scale >= 1024 && unit < UNITS.length - 1) {
		scale *= 1024;
		unit++;
	}
	const digits = (value: number) => (unit === 0 || value >= 100 ? 0 : 1);
	return `${number(used / scale, digits(used / scale))} / ${number(total / scale, digits(total / scale))} ${UNITS[unit]}`;
}

export function formatPercent(value: number): string {
	return `${number(value, 0)} %`;
}

/** Bytes per second → megabytes per second, as a bare number. */
export function formatMegabytesPerSecond(bytesPerSecond: number): string {
	const value = bytesPerSecond / (1024 * 1024);
	return number(value, value >= 100 ? 0 : value >= 10 ? 1 : 2);
}

/** 1051200 → "12 T 4 Std": the two largest units. */
export function formatDuration(seconds: number): string {
	const days = Math.floor(seconds / 86400);
	const hours = Math.floor((seconds % 86400) / 3600);
	const minutes = Math.floor((seconds % 3600) / 60);
	if (days > 0) return t('duration.daysHours', { days, hours });
	if (hours > 0) return t('duration.hoursMinutes', { hours, minutes });
	if (seconds < 60) return t('duration.underMinute');
	return t('duration.minutes', { minutes });
}

export function formatTime(date: Date): string {
	return new Intl.DateTimeFormat(i18n.locale, { hour: '2-digit', minute: '2-digit' }).format(date);
}

export function formatDateTime(date: Date): string {
	return new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }).format(date);
}

/**
 * Sizes of disks and volumes in decimal units (1 TB = 1000 GB), the way
 * disks are sold and labelled: 8001563222016 → "8 TB".
 */
export function formatDiskBytes(bytes: number): string {
	let value = bytes;
	let unit = 0;
	while (value >= 1000 && unit < UNITS.length - 1) {
		value /= 1000;
		unit++;
	}
	return `${number(value, unit === 0 || value >= 100 ? 0 : 1)} ${UNITS[unit]}`;
}
