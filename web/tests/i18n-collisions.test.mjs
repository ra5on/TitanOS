import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

// The texts of the features are merged over the base files. A key that
// exists in two files silently takes the text of the later one: the share
// right "Kein Zugriff" once read "Nichts zum Ablegen" that way.
const dir = new URL('../src/lib/i18n', import.meta.url).pathname;

for (const lang of ['de', 'en']) {
	test(`no translation key is defined twice (${lang})`, () => {
		const files = [
			join(dir, `${lang}.json`),
			...readdirSync(join(dir, 'features'))
				.filter((name) => name.endsWith(`.${lang}.json`))
				.map((name) => join(dir, 'features', name)),
		];
		const seen = new Map();
		const twice = [];
		for (const file of files) {
			for (const key of Object.keys(JSON.parse(readFileSync(file, 'utf8')))) {
				if (seen.has(key)) twice.push(`${key}: ${seen.get(key)} and ${file}`);
				seen.set(key, file);
			}
		}
		assert.deepEqual(twice, []);
	});
}
