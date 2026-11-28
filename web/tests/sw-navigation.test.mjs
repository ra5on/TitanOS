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

// 1.39 (owner 28.11.2026: after an update Safari showed nothing until its
// website data was cleared): a page never stands in for a file of the UI.
function assetWorker(network, kept) {
	const store = new Map(kept ? [[kept.url, kept.res]] : []);
	const cache = {
		match: async (req) => store.get(typeof req === 'string' ? req : req.url),
		put: async (req, res) => void store.set(req.url, res),
		delete: async (req) => store.delete(req.url),
	};
	const handler = runInNewContext(source + '\nasset;', {
		self: { addEventListener() {} },
		fetch: async () => network(),
		caches: { open: async () => cache },
		Response,
		setTimeout,
		clearTimeout,
	});
	return { store, run: (url) => handler({ request: new Request(url), waitUntil: (p) => p }) };
}
const chunk = 'https://titanos.test/_app/immutable/nodes/9.abc.js';
// Same-origin answers of fetch are "basic"; a constructed Response is not.
const basic = (res) => Object.defineProperty(res, 'type', { value: 'basic' });
const js = () =>
	basic(
		new Response('export {}', { headers: { 'Content-Type': 'text/javascript; charset=utf-8' } }),
	);
const html = () =>
	basic(new Response('<html></html>', { headers: { 'Content-Type': 'text/html; charset=utf-8' } }));

test('the HTML of a fallback is never kept as a file of the UI', async () => {
	const w = assetWorker(html);
	const res = await w.run(chunk);
	assert.equal(await res.text(), '<html></html>');
	await new Promise((r) => setTimeout(r, 0));
	assert.equal(w.store.size, 0);
});

test('a script is kept and answered from the cache next time', async () => {
	let calls = 0;
	const w = assetWorker(() => (calls++, js()));
	await w.run(chunk);
	await new Promise((r) => setTimeout(r, 0));
	assert.equal(w.store.size, 1);
	await w.run(chunk);
	assert.equal(calls, 1);
});

test('a page kept under a file name before 1.39 is dropped and fetched again', async () => {
	const w = assetWorker(js, { url: chunk, res: html() });
	const res = await w.run(chunk);
	assert.equal(await res.text(), 'export {}');
	await new Promise((r) => setTimeout(r, 0));
	assert.match(w.store.get(chunk).headers.get('Content-Type'), /javascript/);
});

test('the caches of 1.39 have new names so old broken ones are removed', () => {
	assert.match(source, /titanos-assets-v2/);
	assert.match(source, /titanos-shell-v2/);
	assert.doesNotMatch(source, /'titanos-assets-v1'/);
});
