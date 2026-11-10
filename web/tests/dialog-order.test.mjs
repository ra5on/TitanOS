import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

// A dialog gives the focus to the first thing in it that can take it. In a
// bar of buttons only, that must not be the dangerous one: Enter would
// carry it out. "Cancel" comes first in the markup; Dialog.svelte moves the
// dangerous button to the left by CSS (docs/rules/WEB-UI.md).
function sources(dir) {
	const out = [];
	for (const entry of readdirSync(dir, { withFileTypes: true })) {
		const path = join(dir, entry.name);
		if (entry.isDirectory()) out.push(...sources(path));
		else if (entry.name.endsWith('.svelte')) out.push(path);
	}
	return out;
}

test('the dangerous button is never the first one in a bar of dialog buttons', () => {
	const wrong = [];
	for (const file of sources(new URL('../src', import.meta.url).pathname)) {
		const text = readFileSync(file, 'utf8');
		for (const bar of text.matchAll(/<div class="dialog-actions[^"]*"[^>]*>([\s\S]*?)<\/div>/g)) {
			const body = bar[1];
			const buttons = [...body.matchAll(/<button\b([\s\S]*?)>/g)].map((m) => m[1]);
			// Only bars of plain buttons: a form with fields before the bar
			// gives the focus to its first field, a bar with blocks is
			// looked at by hand.
			if (buttons.length < 2 || body.replace(/<button\b[\s\S]*?<\/button\s*>/g, '').trim())
				continue;
			const danger = /btn-danger(?!-outline)|btn-warn/.test(buttons[0]);
			const focusElsewhere = buttons.slice(1).some((b) => /\bautofocus\b/.test(b));
			if (danger && !focusElsewhere)
				wrong.push(`${file}:${text.slice(0, bar.index).split('\n').length}`);
		}
	}
	assert.deepEqual(wrong, []);
});
