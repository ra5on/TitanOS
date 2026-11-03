import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	checksHintVolumes,
	sanitizeHints,
	uncheckedVolumes,
} from '../src/routes/storage/checks-hint.ts';

const volume = (name, over = {}) => ({
	name,
	level: 'raid1',
	filesystem: 'ext4',
	state: 'ok',
	members: [
		{ disk: 'sda', state: 'active' },
		{ disk: 'sdb', state: 'active' },
	],
	...over,
});
const disks = [
	{ name: 'sda', serial: 'A1' },
	{ name: 'sdb', serial: 'B2' },
];
const job = (kind, target, enabled = true) => ({ kind, target, enabled });
const settings = (jobs, enabled = true) => ({ enabled, jobs });

test('a volume with an enabled RAID check or scrub has a plan', () => {
	const volumes = [volume('Data'), volume('Fast', { filesystem: 'btrfs' }), volume('Old')];
	const result = uncheckedVolumes(
		volumes,
		disks,
		settings([
			job('raid-check', 'Data'),
			job('raid-check', 'Fast'),
			job('raid-check', 'Old', false),
		]),
	);
	assert.deepEqual(
		result.map((v) => v.name),
		['Old'],
	);
});

test('switched-off maintenance checks nothing', () => {
	const result = uncheckedVolumes(
		[volume('Data')],
		disks,
		settings([job('raid-check', 'Data')], false),
	);
	assert.deepEqual(
		result.map((v) => v.name),
		['Data'],
	);
});

test('an mdadm RAID 0 counts SMART tests of its disks; missing volumes are left out', () => {
	const stripe = volume('Stripe', { level: 'raid0' });
	assert.deepEqual(uncheckedVolumes([stripe], disks, settings([job('smart-short', 'B2')])), []);
	assert.equal(
		uncheckedVolumes([stripe], disks, settings([job('raid-check', 'Stripe')])).length,
		1,
	);
	assert.deepEqual(
		uncheckedVolumes([volume('Gone', { state: 'missing' })], disks, settings([])),
		[],
	);
});

test('dismissed volumes do not show the hint again', () => {
	const unchecked = [volume('Data'), volume('Media')];
	assert.deepEqual(checksHintVolumes(unchecked, sanitizeHints(null)), ['Data', 'Media']);
	assert.deepEqual(checksHintVolumes(unchecked, sanitizeHints({ checksDismissed: ['Data', 3] })), [
		'Media',
	]);
});
