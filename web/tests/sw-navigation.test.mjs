import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { runInNewContext } from 'node:vm';

const source = readFileSync(new URL('../static/sw.js', import.meta.url), 'utf8');
const url = new URL('https://titanos.test/');

function navigate(status, cached) {
	const handler = runInNewContext(source + '\nnavigate;', {
		self: { addEventListener() {} },
		fetch: async () => new Response('network', { status }),
		caches: { open: async () => ({ match: async () => cached }) },
		Response,
		setTimeout,
		clearTimeout,
	});
	return handler({ request: new Request(url), waitUntil() {} }, url);
}

test('an unavailable gateway uses the saved app shell when it exists', async () => {
	for (const status of [502, 503, 504]) {
		const cached = await navigate(status, new Response('saved shell'));
		assert.equal(await cached.text(), 'saved shell');
		assert.equal((await navigate(status)).status, status);
	}
	assert.equal((await navigate(401, new Response('saved shell'))).status, 401);
});
