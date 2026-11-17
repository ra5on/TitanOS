import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	formatCode,
	quotaBytes,
	quotaEdited,
	quotaField,
	quotaPercent,
	stepsPreview,
} from '../src/lib/features/family.ts';

test('pairing codes are typed in groups of three', () => {
	assert.equal(formatCode('k7q4mz92x'), 'K7Q-4MZ-92X');
	assert.equal(formatCode('K7Q 4MZ-92X-EXTRA'), 'K7Q-4MZ-92X');
	assert.equal(formatCode('k7'), 'K7');
});

test('quota share stays between 0 and 100', () => {
	assert.equal(quotaPercent(0, 0), 0);
	assert.equal(quotaPercent(2.3, 3), 77);
	assert.equal(quotaPercent(5, 3), 100);
});

test('the buddy quota is typed and shown in the same GB (A-33)', () => {
	// 500 typed in the field labelled GB is shown as 500 GB again, not 537.
	assert.equal(quotaBytes(500), 500e9);
	assert.equal(quotaField(quotaBytes(500)), 500);
	// A quota saved before 1.03 (500 GiB) keeps its stored bytes when the
	// edit dialog is saved without touching the field.
	const old = 500 * 2 ** 30;
	assert.equal(quotaField(old), 537);
	assert.equal(quotaEdited(quotaField(old), old), old);
	assert.equal(quotaEdited(600, old), 600e9);
});

test('steps preview keeps the first lines', () => {
	assert.deepEqual(stepsPreview('1. a\n\n2. b\n3. c\n4. d', 2), ['1. a', '2. b']);
});

test('family texts have the same keys in German and English', () => {
	const de = JSON.parse(
		readFileSync(new URL('../src/lib/i18n/features/family.de.json', import.meta.url)),
	);
	const en = JSON.parse(
		readFileSync(new URL('../src/lib/i18n/features/family.en.json', import.meta.url)),
	);
	assert.deepEqual(Object.keys(de).sort(), Object.keys(en).sort());
	assert.ok(Object.keys(de).every((k) => k.startsWith('agent.f.family.')));
});
