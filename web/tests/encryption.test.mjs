import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { isLocked, lockedBlocks, lockedTone } from '../src/routes/storage/crypt.ts';
import { noticeLink } from '../src/lib/notice-link.ts';

const volume = (over) => ({ name: 'safe', level: 'raid1', filesystem: 'ext4', state: 'ok', members: [], ...over });
const source = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');

const locked = { locked: true, members: 1, unlocked: 0, passwords: 0 };

test('a locked volume is shown as waiting, a broken one keeps its own state', () => {
	assert.equal(lockedTone(volume({})), null);
	assert.equal(lockedTone(volume({ encryption: locked })), 'warn');
	assert.equal(lockedTone(volume({ state: 'degraded', encryption: locked })), null);
	assert.equal(lockedTone(volume({ encryption: { ...locked, locked: false, unlocked: 1 } })), null);
	assert.ok(isLocked(volume({ encryption: locked })));
	assert.ok(!isLocked(volume({})));
});

test('a locked array still takes disks below its encryption, a locked Btrfs volume does not', () => {
	assert.ok(!lockedBlocks(volume({ encryption: locked })));
	assert.ok(lockedBlocks(volume({ filesystem: 'btrfs', encryption: locked })));
	assert.ok(!lockedBlocks(volume({ filesystem: 'btrfs' })));
});

test('the "waiting for password" notice leads to the volume', () => {
	assert.equal(noticeLink('volume_locked', 'safe'), '/storage#volume-safe');
});

test('every encryption text exists in both languages', () => {
	const de = JSON.parse(source('../src/lib/i18n/de.json'));
	const en = JSON.parse(source('../src/lib/i18n/en.json'));
	const used = new Set();
	for (const file of ['../src/routes/storage/EncryptionPanel.svelte', '../src/routes/storage/CreateVolumeDialog.svelte', '../src/routes/storage/AddDiskDialog.svelte', '../src/routes/storage/import/+page.svelte']) {
		for (const match of source(file).matchAll(/t\('(crypt\.[A-Za-z]+)'/g)) used.add(match[1]);
	}
	for (const kind of ['unlock', 'lock', 'change', 'add', 'remove', 'header']) {
		for (const part of ['Title', 'Text', 'Submit']) used.add(`crypt.${kind}${part}`);
	}
	for (const code of ['volume_locked', 'volume_wrong_password', 'volume_password_attempts', 'volume_weak_password', 'volume_password_required', 'volume_last_password', 'volume_encryption_members', 'web_session_required']) used.add(`error.${code}`);
	used.add('notify.kind.volume_locked.title');
	used.add('notify.kind.volume_locked.text');
	for (const key of used) {
		assert.ok(de[key], `de: ${key}`);
		assert.ok(en[key], `en: ${key}`);
	}
});

test('dialogs never ask to type a name, and keep passwords out of storage', () => {
	const panel = source('../src/routes/storage/EncryptionPanel.svelte');
	assert.ok(!/localStorage|sessionStorage/.test(panel));
	// Destructive buttons are plain buttons: Enter submits only the unlock dialog.
	assert.match(panel, /type=\{dialog === 'unlock' \? 'submit' : 'button'\}/);
	const create = source('../src/routes/storage/CreateVolumeDialog.svelte');
	assert.match(create, /encrypt: encrypt \|\| undefined/);
	assert.match(create, /let encrypt = \$state\(false\)/, 'encryption is off by default');
});
