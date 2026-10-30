import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { effectiveSleep, overridesFrom, shortSerial } from '../src/routes/storage/disk-sleep.ts';

test('disk sleep resolves disk before volume before default, like titand', () => {
	const volumes = { tank: 10, cold: 0 };
	assert.deepEqual(effectiveSleep(30, volumes, undefined, undefined), {
		minutes: 30,
		origin: 'default',
	});
	assert.deepEqual(effectiveSleep(30, volumes, 'other', undefined), {
		minutes: 30,
		origin: 'default',
	});
	assert.deepEqual(effectiveSleep(30, volumes, 'tank', undefined), {
		minutes: 10,
		origin: 'volume',
	});
	assert.deepEqual(effectiveSleep(30, volumes, 'cold', undefined), {
		minutes: 0,
		origin: 'volume',
	});
	assert.deepEqual(effectiveSleep(30, volumes, 'tank', 0), { minutes: 0, origin: 'disk' });
	assert.deepEqual(effectiveSleep(0, volumes, 'cold', 120), { minutes: 120, origin: 'disk' });
	assert.deepEqual(effectiveSleep(0, {}, undefined, 60), { minutes: 60, origin: 'disk' });
});

test('saving sends only overrides of volumes and disks that exist now', () => {
	const stored = { 'serial:B': 5, 'serial:gone': 10, 'serial:A': 0 };
	assert.deepEqual(overridesFrom(stored, ['serial:A', 'serial:B', 'serial:C']), {
		'serial:A': 0,
		'serial:B': 5,
	});
	assert.deepEqual(
		JSON.stringify(overridesFrom({ b: 1, a: 2 }, ['b', 'a'])),
		JSON.stringify({ a: 2, b: 1 }),
	);
	assert.deepEqual(overridesFrom({}, ['a']), {});
});

test('long serial numbers are shortened to their end', () => {
	assert.equal(shortSerial('WD-123'), 'WD-123');
	assert.equal(shortSerial('WD-WCC4N7XYZ12345'), '…XYZ12345');
});

test('disk sleep texts exist in both languages', () => {
	const de = JSON.parse(readFileSync(new URL('../src/lib/i18n/de.json', import.meta.url)));
	const en = JSON.parse(readFileSync(new URL('../src/lib/i18n/en.json', import.meta.url)));
	const dialog = readFileSync(
		new URL('../src/routes/storage/DiskSleepDialog.svelte', import.meta.url),
		'utf8',
	);
	const keys = [...dialog.matchAll(/'(storage\.sleep[A-Za-z]+)'/g)].map((match) => match[1]);
	assert.ok(keys.length > 10);
	for (const key of [...keys, 'error.unknown_disk_sleep_target']) {
		assert.ok(de[key], `de ${key}`);
		assert.ok(en[key], `en ${key}`);
	}
	assert.equal(de['storage.sleepFromVolume'], 'vom Volume');
	assert.equal(de['storage.sleepFromDefault'], 'Standard');
	assert.equal(de['storage.sleepFromDisk'], 'eigene');
});
