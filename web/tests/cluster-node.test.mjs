import { test } from 'node:test';
import assert from 'node:assert/strict';
import { apiPath } from '../src/lib/cluster-node.ts';

test('requests go to the chosen node, login, own account and the cluster stay here', () => {
	assert.equal(apiPath('/storage'), '/api/v1/storage');
	assert.equal(apiPath('/storage', 'abc'), '/api/v1/cluster/nodes/abc/api/storage');
	assert.equal(
		apiPath('/files/download?root=share%2Fa&path=x', 'abc'),
		'/api/v1/cluster/nodes/abc/api/files/download?root=share%2Fa&path=x',
	);
	assert.equal(apiPath('/sessions', 'abc'), '/api/v1/cluster/nodes/abc/api/sessions');
	for (const local of [
		'/session',
		'/session/confirm',
		'/login',
		'/logout',
		'/setup',
		'/preferences',
		'/account',
		'/account/2fa',
		'/cluster',
		'/cluster/nodes',
		'/background?v=2',
		'/login-appearance',
		'/app-icon',
	]) {
		assert.equal(apiPath(local, 'abc'), '/api/v1' + local, local);
	}
	assert.equal(apiPath('/users', 'a/b'), '/api/v1/cluster/nodes/a%2Fb/api/users');
});
