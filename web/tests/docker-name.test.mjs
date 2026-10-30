import { test } from 'node:test';
import assert from 'node:assert/strict';
import { dockerName } from '../src/lib/docker-name.ts';
test('entered app name replaces generated compose repetitions; service and unmanaged identities remain distinct', () => {
	const container = { managed: true, app: 'ollama', service: 'ollama', name: 'ollama-ollama-1' };
	assert.equal(dockerName(container), 'ollama');
	assert.equal(dockerName(container, 'distinct'), 'ollama');
	assert.equal(dockerName({ ...container, service: 'database' }, 'distinct'), 'ollama · database');
	assert.equal(dockerName({ ...container, service: 'database' }, 'service'), 'database');
	assert.equal(dockerName({ ...container, managed: false }), 'ollama-ollama-1');
	assert.equal(container.name, 'ollama-ollama-1');
});
