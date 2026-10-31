import { afterEach, beforeEach, expect, test } from 'vitest';
import {
	formatBytes,
	formatBytesOf,
	formatDateTime,
	formatDiskBytes,
	formatDuration,
	formatMegabytesPerSecond,
	formatNumber,
	formatPercent,
} from '#lib/format.ts';
import { i18n } from '#lib/i18n/index.svelte.ts';

// Intl puts a narrow no-break space before units in some locales; compare plain.
const plain = (text: string) => text.replace(/[  ]/g, ' ');
let before: typeof i18n.locale;
beforeEach(() => {
	before = i18n.locale;
	i18n.locale = 'de';
});
afterEach(() => {
	i18n.locale = before;
});

test('formatBytes: binary steps, fewer digits for larger numbers', () => {
	expect(formatBytes(0)).toBe('0 B');
	expect(formatBytes(1023)).toBe('1.023 B');
	expect(formatBytes(1024)).toBe('1 KB');
	expect(formatBytes(1536)).toBe('1,5 KB');
	expect(formatBytes(3328599654)).toBe('3,1 GB');
	expect(formatBytes(150 * 1024 ** 2)).toBe('150 MB');
	expect(formatBytes(2 * 1024 ** 6)).toBe('2.048 PB');
	i18n.locale = 'en';
	expect(formatBytes(1536)).toBe('1.5 KB');
});

test('formatBytesOf: both values in the unit of the total', () => {
	expect(formatBytesOf(3.1 * 1024 ** 3, 16 * 1024 ** 3)).toBe('3,1 / 16 GB');
	expect(formatBytesOf(512, 1000)).toBe('512 / 1.000 B');
	expect(formatBytesOf(0, 200 * 1024 ** 2)).toBe('0 / 200 MB');
});

test('formatDiskBytes: decimal units like on the label of a disk', () => {
	expect(formatDiskBytes(8001563222016)).toBe('8 TB');
	expect(formatDiskBytes(500107862016)).toBe('500 GB');
	expect(formatDiskBytes(999)).toBe('999 B');
});

test('formatPercent, formatNumber and formatMegabytesPerSecond', () => {
	expect(formatPercent(42.6)).toBe('43 %');
	expect(formatNumber(1234567)).toBe('1.234.567');
	expect(formatMegabytesPerSecond(150 * 1024 ** 2)).toBe('150');
	expect(formatMegabytesPerSecond(12.34 * 1024 ** 2)).toBe('12,3');
	expect(formatMegabytesPerSecond(1.234 * 1024 ** 2)).toBe('1,23');
});

test('formatDuration: the two largest units', () => {
	expect(formatDuration(1051200)).toBe('12 T 4 Std');
	expect(formatDuration(3 * 3600 + 5 * 60)).toBe('3 Std 5 Min');
	expect(formatDuration(59)).toBe('unter 1 Min');
	expect(formatDuration(600)).toBe('10 Min');
	i18n.locale = 'en';
	expect(formatDuration(1051200)).toBe('12 d 4 h');
});

test('formatDateTime: in a given zone, an unknown zone falls back', () => {
	const date = new Date('2026-10-31T12:00:00Z');
	expect(plain(formatDateTime(date, 'Europe/Berlin'))).toBe('31.10.2026, 13:00');
	expect(plain(formatDateTime(date, 'UTC'))).toBe('31.10.2026, 12:00');
	expect(formatDateTime(date, 'Nowhere/Invalid')).toBe(formatDateTime(date));
});
