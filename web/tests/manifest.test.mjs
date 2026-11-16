import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

// 1.03: the manifest of TitanOS in each language (owner 16 Nov 2026). Both
// must describe the same app: an installed app keeps its identity whatever
// the language of the page that offers the manifest.
const read = (name) => JSON.parse(readFileSync(new URL(`../static/${name}`, import.meta.url)));
const de = read('manifest.webmanifest');
const en = read('manifest.en.webmanifest');

test('German and English manifests are one app', () => {
	const same = (m) => ({
		...m,
		description: '',
		lang: '',
		shortcuts: m.shortcuts.map((s) => ({ ...s, name: '' })),
	});
	assert.deepEqual(same(en), same(de));
	assert.equal(de.id, '/');
});

test('each speaks its language', () => {
	assert.equal(de.lang, 'de');
	assert.equal(en.lang, 'en');
	assert.deepEqual(
		en.shortcuts.map((s) => s.name),
		['Files', 'Docker', 'Control panel'],
	);
	assert.deepEqual(
		de.shortcuts.map((s) => s.name),
		['Dateien', 'Docker', 'Systemsteuerung'],
	);
	assert.match(en.description, /^Your NAS/);
});
