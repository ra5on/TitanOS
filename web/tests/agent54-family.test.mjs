import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	diarySources,
	formatCode,
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

test('diary sources come only from titanos:diary links', () => {
	assert.deepEqual(
		diarySources([
			{ title: 'Fotos (48)', url: 'titanos:diary/photos' },
			{ title: 'Web', url: 'https://example.org' },
		]),
		[{ title: 'Fotos (48)', id: 'photos' }],
	);
	assert.deepEqual(diarySources(undefined), []);
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
