import { test } from 'node:test';
import assert from 'node:assert/strict';
import { matchActions, quickActions } from '../src/lib/desktop/quick-actions.ts';

const t = (key, params = {}) => key + (params.name ? ':' + params.name : '');
const data = {
	containers: [
		{ id: 'a1', name: 'immich', state: 'running' },
		{ id: 'b2', name: 'paperless', state: 'exited' },
	],
	vms: [
		{ name: 'haos', displayName: 'Home Assistant', state: 'running' },
		{ name: 'win', state: 'stopped' },
	],
	backups: ['nightly'],
};

test('an account without admin rights gets only lock and log out', () => {
	assert.deepEqual(
		quickActions(t, false, data).map((a) => a.id),
		['lock', 'logout'],
	);
});

test('containers, VMs and backups by their state, with the pages’ routes', () => {
	const all = quickActions(t, true, data);
	const byId = Object.fromEntries(all.map((a) => [a.id, a]));
	assert.equal(byId['c-restart-a1'].post, '/docker/containers/a1/restart');
	assert.equal(byId['c-restart-a1'].confirm, true);
	assert.equal(byId['c-stop-a1'].post, '/docker/containers/a1/stop');
	assert.equal(byId['c-start-b2'].post, '/docker/containers/b2/start');
	assert.equal(byId['c-start-b2'].confirm, undefined);
	assert.equal(byId['c-start-a1'], undefined, 'a running container is not started');
	assert.equal(byId['vm-down-haos'].post, '/vms/haos/shutdown');
	assert.equal(byId['vm-start-win'].post, '/vms/win/start');
	assert.equal(byId['b-nightly'].post, '/backups/nightly/run');
	assert.equal(byId.stick.post, '/config-backup/run');
	assert.equal(byId.restart.local, 'restart');
	assert.equal(byId.logout.confirm, true);
});

test('every word of the query must match', () => {
	const all = quickActions(t, true, data);
	assert.deepEqual(
		matchActions(all, 'immich neu').map((a) => a.id),
		['c-restart-a1'],
	);
	assert.deepEqual(
		matchActions(all, 'backup jetzt').map((a) => a.id),
		['b-nightly'],
	);
	assert.deepEqual(matchActions(all, '   '), []);
	assert.ok(matchActions(all, 'home assistant').some((a) => a.id === 'vm-down-haos'));
});
