import { describe, expect, it } from 'vitest';
import type { DiskSleepStatus } from '../../src/lib/api.ts';
import { i18n } from '../../src/lib/i18n/index.svelte.ts';
import {
	diskSleep,
	sleepListed,
	sleepSince,
	volumeSleep,
	wakeCauseText,
	wakeHints,
	wakeTime,
} from '../../src/routes/storage/sleep-view.ts';

const base: DiskSleepStatus = { idleMinutes: 30, available: true, errors: [] };

describe('sleep view', () => {
	it('knows when the disks were listed, also with no disk that can sleep', () => {
		expect(sleepListed(undefined)).toBe(false);
		expect(sleepListed(base)).toBe(false);
		// The empty lists are left out of the answer; listed says they were read.
		expect(sleepListed({ ...base, listed: true })).toBe(true);
		expect(sleepListed({ ...base, volumeStatus: [], diskStatus: [] })).toBe(true);
	});

	it('counts the sleeping disks of a volume', () => {
		const status: DiskSleepStatus = {
			...base,
			listed: true,
			volumeStatus: [{ name: 'tank', override: null, effective: 30, origin: 'default' }],
			diskStatus: [
				{
					id: 'a',
					name: 'sda',
					model: 'M',
					serial: 'a',
					sizeBytes: 1,
					volume: 'tank',
					override: null,
					effective: 30,
					origin: 'default',
					state: 'asleep',
				},
				{
					id: 'b',
					name: 'sdb',
					model: 'M',
					serial: 'b',
					sizeBytes: 1,
					volume: 'tank',
					override: null,
					effective: 30,
					origin: 'default',
					state: 'active',
				},
				{
					id: 'c',
					name: 'sdc',
					model: 'M',
					serial: 'c',
					sizeBytes: 1,
					volume: 'other',
					override: null,
					effective: 30,
					origin: 'default',
					state: 'asleep',
				},
			],
		};
		const sleep = volumeSleep(status, 'tank');
		expect(sleep?.total).toBe(2);
		expect(sleep?.asleep).toBe(1);
		expect(volumeSleep(status, 'missing')).toBeUndefined();
	});

	it('says why a disk has no sleep entry', () => {
		const status: DiskSleepStatus = {
			...base,
			listed: true,
			unavailable: [{ name: 'sde', model: 'SSD', sizeBytes: 1, volume: 'fast', reason: 'ssd' }],
		};
		expect(diskSleep(status, 'sde')).toEqual({ reason: 'ssd' });
		expect(diskSleep(status, 'sdx')).toEqual({ reason: undefined });
		expect(diskSleep(base, 'sde')).toBeUndefined();
	});

	it('shows since when a disk sleeps or when it was last woken', () => {
		const disk = {
			id: 'a',
			name: 'sda',
			model: 'M',
			serial: 'a',
			sizeBytes: 1,
			override: null,
			effective: 30,
			origin: 'default' as const,
			state: 'asleep' as const,
		};
		const now = new Date(2026, 10, 3, 18, 0);
		const today = new Date(2026, 10, 3, 14, 2).toISOString();
		const earlier = new Date(2026, 10, 1, 9, 5).toISOString();
		expect(sleepSince(disk, now)).toBeUndefined();
		expect(sleepSince({ ...disk, since: 'nonsense' }, now)).toBeUndefined();
		const locale = i18n.locale;
		i18n.locale = 'de';
		try {
			expect(sleepSince({ ...disk, since: today }, now)).toBe('schläft seit 14:02');
			expect(sleepSince({ ...disk, state: 'active', since: today }, now)).toBe(
				'zuletzt geweckt 14:02',
			);
		} finally {
			i18n.locale = locale;
		}
		// Not today: the date is part of it.
		expect(sleepSince({ ...disk, since: earlier }, now)).not.toBe(
			sleepSince({ ...disk, since: new Date(2026, 10, 3, 9, 5).toISOString() }, now),
		);
	});

	it('names what woke a disk, with a hint for what TitanOS writes itself', () => {
		const now = new Date(2026, 10, 12, 22, 0);
		const at = new Date(2026, 10, 12, 21, 25).toISOString();
		const disk: DiskSleepStatus['diskStatus'] & object = [
			{
				id: 'a',
				name: 'sda',
				model: 'M',
				serial: 'a',
				sizeBytes: 1,
				override: null,
				effective: 30,
				origin: 'default',
				state: 'active',
				since: at,
				wakes: [
					{ at, causes: [{ code: 'activity_log' }, { code: 'backup_job', detail: 'Nachts' }] },
					{
						at: new Date(2026, 10, 11, 3, 0).toISOString(),
						causes: [{ code: 'maintenance', detail: 'raid' }],
					},
					{ at: new Date(2026, 10, 10, 3, 0).toISOString(), causes: [{ code: 'unknown' }] },
					{ at: new Date(2026, 10, 9, 3, 0).toISOString(), causes: [{ code: 'journal' }] },
				],
			},
		];
		const locale = i18n.locale;
		i18n.locale = 'de';
		try {
			expect(sleepSince(disk[0], now)).toBe(
				'aufgeweckt heute 21:25 durch Aktivitätsprotokoll, Datensicherung „Nachts“',
			);
			// The newest wake is not the one the state names: no cause is claimed.
			expect(
				sleepSince({ ...disk[0], since: new Date(2026, 10, 12, 21, 40).toISOString() }, now),
			).toBe('zuletzt geweckt 21:40');
			expect(sleepSince({ ...disk[0], state: 'asleep' }, now)).toBe('schläft seit 21:25');
			expect(wakeTime(at, now)).toBe('heute 21:25');
			expect(wakeTime(disk[0].wakes![1].at, now)).not.toContain('heute');
			expect(wakeCauseText({ code: 'maintenance', detail: 'raid' })).toBe(
				'RAID-Prüfung oder Wiederaufbau',
			);
			expect(wakeCauseText({ code: 'file_manager', detail: 'anna' })).toBe('Dateizugriff von anna');
			expect(wakeCauseText({ code: 'unknown' })).toContain('nicht TitanOS selbst');
			// A code of a newer titand is shown, not hidden and not a raw key.
			expect(wakeCauseText({ code: 'future_thing', detail: 'x' })).toBe('TitanOS (future_thing x)');
		} finally {
			i18n.locale = locale;
		}
		// Two wakes by TitanOS's own data, one unknown: that order.
		expect(wakeHints(disk[0])).toEqual(['systemData', 'unknown']);
		expect(wakeHints({ ...disk[0], wakes: undefined })).toEqual([]);
	});
});
