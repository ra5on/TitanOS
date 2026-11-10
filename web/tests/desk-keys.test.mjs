// PLAN 76 (F-12-10): the desktop's keys behind Alt also work on macOS, where
// Option changes the character, and leave Option+arrow to text fields.
import assert from 'node:assert/strict';
import test from 'node:test';
import { deskKey } from '../src/lib/desktop/menubar/keys.ts';

const ev = (o) => ({ altKey: true, ctrlKey: false, metaKey: false, target: null, ...o });
const field = (tag, type = 'text') => {
	const el = { tagName: tag, type, isContentEditable: false };
	el.closest = () => (tag === 'DIV' ? null : el);
	return el;
};

test('letters with Alt', () => {
	assert.equal(deskKey(ev({ key: 'w', code: 'KeyW' })), 'w');
	assert.equal(deskKey(ev({ key: 'Q', code: 'KeyQ' })), 'q');
	assert.equal(deskKey(ev({ key: 'x', code: 'KeyX' })), '');
	assert.equal(deskKey(ev({ key: 'w', code: 'KeyW', altKey: false })), '');
	assert.equal(deskKey(ev({ key: 'w', code: 'KeyW', ctrlKey: true })), '');
});

test('macOS: Option changes the character, the place of the key counts', () => {
	assert.equal(deskKey(ev({ key: '∑', code: 'KeyW' })), 'w');
	assert.equal(deskKey(ev({ key: 'œ', code: 'KeyQ' })), 'q');
	assert.equal(deskKey(ev({ key: 'µ', code: 'KeyM' })), 'm');
	assert.equal(deskKey(ev({ key: '˚', code: 'KeyK' })), 'k');
});

test('another layout: a plain letter on the place of W is not W', () => {
	assert.equal(deskKey(ev({ key: 'z', code: 'KeyW' })), '');
});

test('arrows stay with a text field', () => {
	assert.equal(deskKey(ev({ key: 'ArrowLeft' })), 'ArrowLeft');
	assert.equal(deskKey(ev({ key: 'ArrowLeft', target: field('INPUT') })), '');
	assert.equal(deskKey(ev({ key: 'ArrowUp', target: field('TEXTAREA') })), '');
	assert.equal(deskKey(ev({ key: 'ArrowLeft', target: field('INPUT', 'checkbox') })), 'ArrowLeft');
	assert.equal(deskKey(ev({ key: 'ArrowLeft', target: field('DIV') })), 'ArrowLeft');
	assert.equal(deskKey(ev({ key: 'w', code: 'KeyW', target: field('INPUT') })), 'w');
});
