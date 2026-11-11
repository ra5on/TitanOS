import { describe, expect, it } from 'vitest';
import {
	offeredVolumes,
	sleepingChoices,
	sysDataHint,
	type SysDataFunction,
	type SysDataState,
} from '../../src/lib/sysdata.ts';

const fn = (id: SysDataFunction['id'], volume: string, choice = ''): SysDataFunction => ({
	id,
	choice,
	volume,
	source: choice ? 'own' : 'automatic',
	enabled: true,
	moving: false,
	sizeBytes: null,
	sizeAt: null,
});
const state = (over: Partial<SysDataState> = {}): SysDataState => ({
	volume: '',
	resolved: 'nvme',
	sleepKnown: true,
	volumes: [
		{ name: 'Backup', mounted: true, sleeps: true },
		{ name: 'nvme', mounted: true, sleeps: false },
		{ name: 'locked', mounted: false, sleeps: false },
	],
	functions: [fn('logs', 'nvme'), fn('activity', 'nvme')],
	...over,
});

describe('system data', () => {
	it('praises a volume that never sleeps and names the spared ones', () => {
		expect(sysDataHint(state())).toEqual({ tone: 'ok', volume: 'nvme', spared: ['Backup'] });
	});
	it('does not call a sleeping volume spared while a function still writes there', () => {
		const s = state({ functions: [fn('logs', 'nvme'), fn('activity', 'Backup', 'Backup')] });
		expect(sysDataHint(s)).toEqual({ tone: 'ok', volume: 'nvme', spared: [] });
		expect(sleepingChoices(s).map((f) => f.id)).toEqual(['activity']);
	});
	it('warns about a common volume with sleeping disks', () => {
		expect(sysDataHint(state({ volume: 'Backup', resolved: 'Backup' }))).toEqual({
			tone: 'warn',
			volume: 'Backup',
		});
	});
	it('says nothing it does not know', () => {
		expect(sysDataHint(state({ sleepKnown: false })).tone).toBe('unknown');
		expect(sysDataHint(state({ resolved: '' })).tone).toBe('none');
		expect(sleepingChoices(state({ sleepKnown: false }))).toEqual([]);
	});
	it('offers mounted volumes and keeps a stored one that is not mounted', () => {
		expect(offeredVolumes(state(), '').map((v) => v.name)).toEqual(['Backup', 'nvme']);
		expect(offeredVolumes(state(), 'locked').map((v) => v.name)).toEqual([
			'Backup',
			'nvme',
			'locked',
		]);
	});
});
