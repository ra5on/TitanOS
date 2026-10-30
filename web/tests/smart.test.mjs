import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	isSmartList,
	sectorProblems,
	smartCounts,
	smartTone,
	sortReports,
	validDiskName,
} from '../src/lib/smart.ts';

const report = (disk, state, extra = {}) => ({
	disk,
	path: `/dev/${disk}`,
	model: '',
	serial: '',
	kind: 'hdd',
	role: 'free',
	state,
	warnings: [],
	sleeping: false,
	values: null,
	...extra,
});

test('SMART badges keep the backend judgement and sort problems first', () => {
	assert.equal(smartTone('ok'), 'ok');
	assert.equal(smartTone('warning'), 'warn');
	assert.equal(smartTone('failed'), 'error');
	assert.equal(smartTone('unknown'), '');
	assert.equal(smartTone('unavailable'), '');
	const sorted = sortReports([
		report('sdb', 'ok'),
		report('sda10', 'unknown'),
		report('sda2', 'ok'),
		report('sdc', 'failed'),
		report('nvme0n1', 'warning'),
		report('sdz', 'unavailable'),
	]);
	assert.deepEqual(
		sorted.map((r) => r.disk),
		['sdc', 'nvme0n1', 'sda2', 'sdb', 'sda10', 'sdz'],
	);
});

test('sleeping disks are counted without hiding the state of their last values', () => {
	const counts = smartCounts([
		report('sda', 'warning', { sleeping: true }),
		report('sdb', 'unknown', { sleeping: true }),
		report('sdc', 'ok'),
	]);
	assert.deepEqual(counts, { ok: 1, warning: 1, failed: 0, sleeping: 2, other: 1 });
});

test('sector counters add up only what the disk reports', () => {
	assert.equal(sectorProblems(null), null);
	assert.equal(
		sectorProblems({ reallocatedSectors: null, pendingSectors: null, uncorrectableSectors: null }),
		null,
	);
	assert.equal(
		sectorProblems({ reallocatedSectors: 8, pendingSectors: 1, uncorrectableSectors: null }),
		9,
	);
	assert.equal(
		sectorProblems({ reallocatedSectors: 0, pendingSectors: 0, uncorrectableSectors: 0 }),
		0,
	);
});

test('unexpected answers and disk names are refused', () => {
	assert.ok(isSmartList({ disks: [report('sda', 'ok')] }));
	assert.ok(isSmartList({ disks: [] }));
	for (const bad of [null, '<html>', {}, { disks: {} }, { disks: [{ disk: 'sda' }] }])
		assert.equal(isSmartList(bad), false);
	assert.ok(validDiskName('nvme0n1'));
	assert.ok(validDiskName('sda'));
	for (const bad of ['', '..', 'sda/1', '../etc', 'a'.repeat(40)])
		assert.equal(validDiskName(bad), false);
});
