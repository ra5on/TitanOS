import { test } from 'node:test';
import assert from 'node:assert/strict';
import { newVMIdentifier, validVMDisplayName, vmLabel } from '../src/lib/vm-name.ts';

test('free VM labels map to safe unique identifiers without changing the label', () => {
	for (const label of ['Home Assistant', 'Büro – Windows', '日本語', 'VM <Test> & NAS']) {
		assert.equal(validVMDisplayName(label), true);
		const id = newVMIdentifier(label, []);
		assert.match(id, /^[a-z0-9][a-z0-9-]{0,30}$/);
		assert.equal(vmLabel({ name: id, displayName: label }), label);
		assert.notEqual(newVMIdentifier(label, [id]), id);
	}
	assert.equal(
		newVMIdentifier('Home Assistant', ['home-assistant', 'home-assistant-2']),
		'home-assistant-3',
	);
	assert.equal(vmLabel({ name: 'old-vm' }), 'old-vm');
	for (const label of ['', ' ', 'a\nb', 'a\u202eb', 'a'.repeat(81)])
		assert.equal(validVMDisplayName(label), false);
});
