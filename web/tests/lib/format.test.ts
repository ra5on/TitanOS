import { afterEach, beforeEach, describe, expect, it, test } from 'vitest';
import {
	formatBytes,
	formatBytesOf,
	formatDateTime,
	formatDiskBytes,
	formatDuration,
	formatMegabytesPerSecond,
	formatNumber,
	formatPercent,
	osVersion,
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

test('formatBytes: decimal steps, fewer digits for larger numbers', () => {
	expect(formatBytes(0)).toBe('0 B');
	expect(formatBytes(999)).toBe('999 B');
	expect(formatBytes(1000)).toBe('1 KB');
	expect(formatBytes(1500)).toBe('1,5 KB');
	expect(formatBytes(3_100_000_000)).toBe('3,1 GB');
	expect(formatBytes(150 * 1000 ** 2)).toBe('150 MB');
	expect(formatBytes(2 * 1000 ** 6)).toBe('2.000 PB');
	i18n.locale = 'en';
	expect(formatBytes(1500)).toBe('1.5 KB');
});

test('formatBytesOf: both values in the unit of the total', () => {
	expect(formatBytesOf(3.1 * 1000 ** 3, 16 * 1000 ** 3)).toBe('3,1 / 16 GB');
	expect(formatBytesOf(512, 999)).toBe('512 / 999 B');
	expect(formatBytesOf(0, 200 * 1000 ** 2)).toBe('0 / 200 MB');
});

test('formatDiskBytes: decimal units like on the label of a disk', () => {
	expect(formatDiskBytes(8001563222016)).toBe('8 TB');
	expect(formatDiskBytes(500107862016)).toBe('500 GB');
	expect(formatDiskBytes(999)).toBe('999 B');
});

test('formatPercent, formatNumber and formatMegabytesPerSecond', () => {
	expect(formatPercent(42.6)).toBe('43 %');
	expect(formatNumber(1234567)).toBe('1.234.567');
	expect(formatMegabytesPerSecond(150 * 1000 ** 2)).toBe('150');
	expect(formatMegabytesPerSecond(12.34 * 1000 ** 2)).toBe('12,3');
	expect(formatMegabytesPerSecond(1.234 * 1000 ** 2)).toBe('1,23');
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

describe('osVersion (PLAN 75-20)', () => {
	it('shows releases since 0.72 in two parts', () => {
		expect(osVersion('0.72.0')).toBe('0.72');
		expect(osVersion('1.3.0')).toBe('1.3');
	});
	it('keeps older versions with a third number and pre-releases', () => {
		expect(osVersion('0.71.1')).toBe('0.71.1');
		expect(osVersion('0.72.0-rc.1')).toBe('0.72.0-rc.1');
		expect(osVersion('0.70.10')).toBe('0.70.10');
	});
});
