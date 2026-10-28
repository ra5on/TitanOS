import { test } from 'node:test';
import assert from 'node:assert/strict';
import { newlyAttached, needsSwap, replacementDisks, swapCandidates } from '../src/lib/disk-swap.ts';

const disk = (name, extra = {}) => ({ name, model: 'M', serial: name.toUpperCase(), sizeBytes: 4e12, role: 'member', smart: { healthy: true, errors: 0 }, identity: 'id-' + name, ...extra });
const raid5 = (members, state = 'ok') => ({ name: 'data', level: 'raid5', filesystem: 'ext4', state, members });

test('failed disks are rebuilt, working ones copied while running', () => {
	const volume = raid5([{ disk: 'sda', state: 'active' }, { disk: 'sdb', state: 'faulty' }, { disk: 'sdc', state: 'active' }], 'degraded');
	const list = swapCandidates(volume, [disk('sda'), disk('sdb'), disk('sdc', { smart: { healthy: true, errors: 3 } })]);
	assert.equal(list[0].disk, 'sdb');
	assert.equal(list[0].mode, 'rebuild');
	// While degraded, working disks cannot be copied.
	assert.equal(list.find((c) => c.disk === 'sdc').mode, 'impossible');
	assert.ok(needsSwap(volume));
	const healthy = raid5([{ disk: 'sda', state: 'active' }, { disk: 'sdc', state: 'active' }]);
	const live = swapCandidates(healthy, [disk('sda'), disk('sdc', { smart: { healthy: false, errors: 0 } })]);
	assert.deepEqual(live.map((c) => [c.disk, c.mode, c.warn]), [['sdc', 'live', true], ['sda', 'live', false]]);
	assert.ok(!needsSwap(healthy));
});

test('missing disks, Btrfs and RAID 0', () => {
	const missing = swapCandidates(raid5([{ disk: 'sda', state: 'active' }, { disk: 'sdz', state: 'active' }], 'degraded'), [disk('sda')]);
	assert.equal(missing[0].disk, 'sdz');
	assert.equal(missing[0].mode, 'rebuild');
	assert.equal(missing[0].present, false);
	const btrfs = { name: 'pool', level: 'raid1', filesystem: 'btrfs', state: 'ok', members: [{ disk: 'sda', state: 'active' }, { disk: 'sdb', state: 'active' }] };
	assert.equal(swapCandidates(btrfs, [disk('sda'), disk('sdb')])[0].mode, 'addRemove');
	const stripe = { ...raid5([{ disk: 'sda', state: 'active' }]), level: 'raid0' };
	assert.equal(swapCandidates(stripe, [disk('sda')])[0].mode, 'impossible');
});

test('only free disks at least as large replace the old one', () => {
	const old = disk('sdb');
	const disks = [old, disk('sdx', { role: 'free', sizeBytes: 8e12 }), disk('sdy', { role: 'free', sizeBytes: 2e12 }), disk('sdz', { role: 'used' }), disk('sdu', { role: 'free', unavailableReason: 'stick' })];
	assert.deepEqual(replacementDisks(disks, old, 0).map((d) => d.name), ['sdx', 'sdz']);
	assert.deepEqual(newlyAttached(['id-sda', 'id-sdb'], [disk('sda'), disk('sdb'), disk('sdx')]).map((d) => d.name), ['sdx']);
});

test('a disk mdadm no longer lists leaves an empty place to fill', () => {
	const list = swapCandidates(raid5([{ disk: 'sda', state: 'active' }, { disk: 'sdb', state: 'active' }], 'degraded'), [disk('sda'), disk('sdb')]);
	assert.deepEqual([list[0].disk, list[0].mode, list[0].present], ['', 'rebuild', false]);
});
