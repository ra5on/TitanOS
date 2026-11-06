import assert from 'node:assert/strict';
import { test } from 'node:test';
import { applyFormat, noteMove, notePath, noteName } from '../src/lib/agent-notes.ts';

test('note paths keep .md and stay inside', () => {
	assert.equal(notePath('', 'Einkauf'), 'Einkauf.md');
	assert.equal(notePath('Haushalt', 'a/b'), 'Haushalt/a-b.md');
	assert.equal(noteName('..versteckt'), 'versteckt');
	assert.equal(noteMove('x.md', 'Arbeit', 'Ideen.md', false), 'Arbeit/Ideen.md');
	assert.equal(noteMove('Alt', '', 'Neu', true), 'Neu');
});

test('bold wraps the selection, a checklist switches lines', () => {
	assert.deepEqual(applyFormat('Milch Brot', 0, 5, 'bold'), {
		text: '**Milch** Brot',
		start: 2,
		end: 7,
	});
	const r = applyFormat('Milch\nBrot', 0, 10, 'check');
	assert.equal(r.text, '- [ ] Milch\n- [ ] Brot');
	assert.equal(applyFormat(r.text, 0, r.text.length, 'check').text, 'Milch\nBrot');
	assert.equal(applyFormat('- [ ] Milch', 0, 0, 'list').text, '- Milch');
	assert.equal(applyFormat('Titel', 2, 2, 'heading').text, '## Titel');
	assert.equal(applyFormat('a', 1, 1, 'rule').text, 'a\n\n---\n\n');
});
