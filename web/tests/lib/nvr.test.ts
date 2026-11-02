import { describe, expect, it } from 'vitest';
import {
	clockOf,
	discoveryProblem,
	fileOf,
	resolution,
	type Discovery,
} from '../../src/routes/nvr/nvr.ts';

const base: Discovery = { found: false, streams: [], rejected: 0, locked: false };

describe('nvr helpers', () => {
	it('names why a search found nothing', () => {
		expect(discoveryProblem({ ...base, found: true })).toBe('');
		expect(discoveryProblem({ ...base, locked: true, rejected: 1 })).toBe('locked');
		expect(discoveryProblem({ ...base, rejected: 3 })).toBe('rejected');
		expect(discoveryProblem(base)).toBe('none');
	});
	it('formats resolution and file names', () => {
		expect(resolution({ width: 2560, height: 1440 })).toBe('2560 × 1440');
		expect(resolution({})).toBe('');
		expect(clockOf('2026-11-02/14-05-00.mp4')).toBe('14:05:00');
		expect(fileOf('2026-11-02/14-05-00.mp4')).toEqual({ day: '2026-11-02', file: '14-05-00.mp4' });
	});
});
