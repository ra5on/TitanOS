import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isEditableText, MAX_TEXT_BYTES } from '../src/lib/text-files.ts';
test('common text formats offered without binary files or oversized documents', () => {
	for (const name of [
		'settings.JSON',
		'compose.yaml',
		'notes.md',
		'data.csv',
		'config.ini',
		'server.conf',
		'Dockerfile',
		'.env',
	])
		assert.equal(isEditableText(name, MAX_TEXT_BYTES), true, name);
	for (const name of ['photo.jpg', 'archive.zip', 'document.docx', 'settings.json.prev'])
		assert.equal(isEditableText(name, 5), false, name);
	assert.equal(isEditableText('large.json', MAX_TEXT_BYTES + 1), false);
});
