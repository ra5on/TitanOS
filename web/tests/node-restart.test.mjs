import { test } from 'node:test';
import assert from 'node:assert/strict';
import { api, ApiError } from '../src/lib/api.ts';
import { onNodeRestart, restartPause } from '../src/lib/node-restart.ts';

const json = (status, body) =>
	new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
const restarting = () =>
	json(503, { error: { code: 'cluster_restarting', message: 'restarting', retryAfter: 1 } });

test('a restarting node is waited for and asked again, the strip follows', async () => {
	const seen = [];
	const stop = onNodeRestart((value) => seen.push(value));
	const answers = [restarting(), json(200, { ok: true })];
	const original = globalThis.fetch;
	globalThis.fetch = async () => answers.shift();
	try {
		assert.deepEqual(await api.get('/storage'), { ok: true });
	} finally {
		globalThis.fetch = original;
		stop();
	}
	assert.deepEqual(seen, [false, true, false]);
	assert.equal(answers.length, 0);
});

test('other 503 answers are errors at once', async () => {
	const original = globalThis.fetch;
	let calls = 0;
	globalThis.fetch = async () => {
		calls++;
		return json(503, { error: { code: 'config_unavailable', message: 'no' } });
	};
	try {
		await assert.rejects(
			api.get('/storage'),
			(err) => err instanceof ApiError && err.code === 'config_unavailable',
		);
	} finally {
		globalThis.fetch = original;
	}
	assert.equal(calls, 1);
});

test('the pause stays between one and ten seconds', () => {
	assert.equal(restartPause(3), 3000);
	assert.equal(restartPause(0), 1000);
	assert.equal(restartPause(60), 10000);
	assert.equal(restartPause(Number.NaN), 3000);
});
