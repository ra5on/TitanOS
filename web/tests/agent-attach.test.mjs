import assert from 'node:assert/strict';
import { test } from 'node:test';
import { freeName, numberedName, sameFile } from '../src/lib/agent-attach.ts';

// 1.01: two files of the same name are both attached (every photo of an
// iPhone is "image.jpg"); the second gets a number before the extension.
test('a second file of the same name gets a number', () => {
	assert.equal(numberedName('image.jpg', 1), 'image.jpg');
	assert.equal(numberedName('image.jpg', 2), 'image (2).jpg');
	assert.equal(numberedName('README', 3), 'README (3)');
	assert.equal(numberedName('.env', 2), '.env (2)');
	assert.equal(freeName('image.jpg', []), 'image.jpg');
	assert.equal(freeName('image.jpg', ['image.jpg']), 'image (2).jpg');
	assert.equal(freeName('image.jpg', ['image.jpg', 'image (2).jpg']), 'image (3).jpg');
});

test('the same file chosen twice is one', () => {
	const a = new File(['abc'], 'image.jpg', { lastModified: 1 });
	const b = new File(['abc'], 'image.jpg', { lastModified: 1 });
	const c = new File(['abcd'], 'image.jpg', { lastModified: 1 });
	assert.ok(sameFile(a, b));
	assert.ok(!sameFile(a, c));
});
