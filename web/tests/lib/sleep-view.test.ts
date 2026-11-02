import { describe, expect, it } from 'vitest';
import type { DiskSleepStatus } from '../../src/lib/api.ts';
import { diskSleep, sleepListed, volumeSleep } from '../../src/routes/storage/sleep-view.ts';

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
});
