import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

// The build keeps only the last of the two declarations when the plain
// "backdrop-filter" stands before "-webkit-backdrop-filter" with a var() or
// max() value. Chromium and Firefox ignore the prefixed one, so the glass
// lost its blur there (0.70 to 0.74). The prefixed line always comes first.
function sources(dir) {
	const out = [];
	for (const entry of readdirSync(dir, { withFileTypes: true })) {
		const path = join(dir, entry.name);
		if (entry.isDirectory()) out.push(...sources(path));
		else if (/\.(svelte|css)$/.test(entry.name)) out.push(path);
	}
	return out;
}

test('the plain backdrop-filter comes after the prefixed one', () => {
	const wrong = [];
	for (const file of sources(new URL('../src', import.meta.url).pathname)) {
		const lines = readFileSync(file, 'utf8').split('\n');
		lines.forEach((line, i) => {
			if (
				/^\s*backdrop-filter\s*:/.test(line) &&
				/^\s*-webkit-backdrop-filter\s*:/.test(lines[i + 1] ?? '')
			)
				wrong.push(`${file}:${i + 1}`);
		});
	}
	assert.deepEqual(wrong, []);
});
