import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { btrfsUsable, canShrink, profileOf, takesDisk } from '../src/routes/storage/btrfs.ts';

const G = 2 ** 30;
const volume = (over) => ({ name: 'tank', level: 'raid1', filesystem: 'btrfs', state: 'ok', members: [{ disk: 'sda', state: 'active' }, { disk: 'sdb', state: 'active' }], ...over });

test('Btrfs space estimate matches the backend', () => {
	assert.equal(btrfsUsable('single', [4 * G, 8 * G]), 12 * G);
	assert.equal(btrfsUsable('raid1', [4 * G, 8 * G]), 4 * G);
	assert.equal(btrfsUsable('raid1', [4 * G, 4 * G, 8 * G]), 8 * G);
	assert.equal(btrfsUsable('raid1c3', [8 * G, 8 * G, 8 * G]), 8 * G);
	assert.equal(btrfsUsable('raid1', []), 0);
});

test('which Btrfs volumes take or give a disk', () => {
	assert.equal(profileOf(volume({ level: 'raid1' })), 'raid1');
	assert.equal(profileOf(volume({ level: 'weird' })), 'raid1');
	assert.ok(takesDisk(volume({})));
	assert.ok(takesDisk(volume({ state: 'degraded' })));
	assert.ok(!takesDisk(volume({ state: 'reshaping' })));
	assert.ok(!canShrink(volume({})), 'RAID 1 of two disks keeps both');
	assert.ok(canShrink(volume({ members: [...volume({}).members, { disk: 'sdc', state: 'active' }] })));
	assert.ok(!takesDisk({ ...volume({}), filesystem: 'ext4', level: 'raid0' }));
});

test('every Btrfs text exists in both languages', () => {
	for (const lang of ['de', 'en']) {
		const texts = JSON.parse(readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8'));
		for (const key of ['level.single', 'level.singleDesc', 'level.raid1c3', 'level.raid1c3Desc', 'state.reshaping', 'member.removing', 'btrfs.profileTitle', 'btrfs.removeConfirm', 'create.singleWarning', 'addDisk.btrfsGrow']) {
			assert.ok(texts[key], `${lang}: ${key}`);
		}
	}
});
