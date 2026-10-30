import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseLogs } from '../src/lib/logs.ts';
import { api } from '../src/lib/api.ts';

test('journal accepts genuine structured entries and preserves historical text', () => {
	const value = {
		cursor: 's=abc;i=2',
		entries: [
			{
				time: '2026-10-24T12:00:00Z',
				priority: 6,
				source: 'titand',
				message: 'TitanOS 0.19.0 started',
			},
		],
	};
	assert.deepEqual(parseLogs(value), value);
	assert.deepEqual(parseLogs({ entries: [], cursor: '' }), { entries: [], cursor: '' });
	for (const bad of [
		'Dashboard',
		null,
		{},
		{ entries: [], cursor: 1 },
		{ entries: [{ ...value.entries[0], time: 'invalid' }], cursor: '' },
		{ entries: [{ ...value.entries[0], priority: 10 }], cursor: '' },
		{ entries: [{ ...value.entries[0], message: {} }], cursor: '' },
	])
		assert.throws(() => parseLogs(bad), { code: 'invalid_response' });
});

test('API rejects HTML fallback and malformed JSON with a typed error', async () => {
	const original = globalThis.fetch;
	try {
		for (const [body, type] of [
			['<h1>Dashboard</h1>', 'text/html'],
			['bad json', 'application/json'],
		]) {
			globalThis.fetch = async () => new Response(body, { headers: { 'Content-Type': type } });
			await assert.rejects(api.get('/logs'), { code: 'invalid_response' });
		}
		globalThis.fetch = async () =>
			new Response(JSON.stringify({ entries: [], cursor: '' }), {
				headers: { 'Content-Type': 'application/json' },
			});
		assert.deepEqual(await api.get('/logs'), { entries: [], cursor: '' });
	} finally {
		globalThis.fetch = original;
	}
});
