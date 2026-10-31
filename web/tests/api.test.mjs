import { test, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { api, apiFetch, ApiError, dispositionName, responseError } from '../src/lib/api.ts';

// Every test answers fetch itself; the calls are recorded.
const realFetch = globalThis.fetch;
let calls = [];
function answer(respond) {
	calls = [];
	globalThis.fetch = async (url, init) => {
		calls.push({ url, init });
		return respond(url, init);
	};
}
const json = (status, body) =>
	new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' },
	});
afterEach(() => {
	globalThis.fetch = realFetch;
	delete globalThis.document;
});

test('api: GET reads JSON from /api/v1 with the session cookie', async () => {
	answer(() => json(200, { ok: 1 }));
	assert.deepEqual(await api.get('/shares'), { ok: 1 });
	assert.equal(calls[0].url, '/api/v1/shares');
	assert.equal(calls[0].init.method, 'GET');
	assert.equal(calls[0].init.credentials, 'same-origin');
});

test('api: an object body is sent as JSON, a Blob as it is', async () => {
	answer(() => new Response(null, { status: 204 }));
	assert.equal(await api.post('/x', { a: 1 }), undefined);
	assert.equal(calls[0].init.body, '{"a":1}');
	assert.equal(calls[0].init.headers['Content-Type'], 'application/json');
	const blob = new Blob(['raw']);
	await api.put('/y', blob);
	assert.equal(calls[1].init.method, 'PUT');
	assert.equal(calls[1].init.body, blob);
	assert.equal(calls[1].init.headers, undefined);
});

test('api: error answers become ApiError with code, message and retryAfter', async () => {
	answer(() => json(429, { error: { code: 'too_many', message: 'slow down', retryAfter: 30 } }));
	await assert.rejects(api.delete('/z'), (err) => {
		assert.ok(err instanceof ApiError);
		assert.equal(err.status, 429);
		assert.equal(err.code, 'too_many');
		assert.equal(err.message, 'slow down');
		assert.equal(err.retryAfter, 30);
		return true;
	});
});

test('api: an error page without JSON is "internal" with the status', async () => {
	answer(() => new Response('<html>Bad Gateway</html>', { status: 502 }));
	await assert.rejects(api.get('/z'), { code: 'internal', message: 'HTTP 502', status: 502 });
});

test('api: a lost connection is "unreachable"', async () => {
	answer(() => {
		throw new TypeError('Failed to fetch');
	});
	await assert.rejects(api.get('/z'), { code: 'unreachable', status: 0 });
});

test('api: a body that is not JSON is an invalid response', async () => {
	answer(() => new Response('hello', { status: 200, headers: { 'Content-Type': 'text/plain' } }));
	await assert.rejects(api.get('/z'), { code: 'invalid_response' });
	answer(() => new Response('{', { status: 200, headers: { 'Content-Type': 'application/json' } }));
	await assert.rejects(api.get('/z'), { code: 'invalid_response' });
});

test('apiFetch: passes options through and leaves the answer alone', async () => {
	answer(() => new Response('x', { status: 500 }));
	const res = await apiFetch('/health', { cache: 'no-store' });
	assert.equal(res.status, 500);
	assert.equal(calls[0].url, '/api/v1/health');
	assert.equal(calls[0].init.cache, 'no-store');
	assert.equal(calls[0].init.credentials, 'same-origin');
	answer(() => {
		throw new TypeError('Failed to fetch');
	});
	// Uploads tell a lost connection from an error answer: no ApiError here.
	await assert.rejects(apiFetch('/files/uploads/1'), TypeError);
});

test('responseError: reads the code of a JSON error body', async () => {
	const err = await responseError(json(409, { error: { code: 'upload_mismatch' } }));
	assert.equal(err.code, 'upload_mismatch');
	assert.equal(err.message, 'HTTP 409');
});

test('dispositionName: the quoted file name or nothing', () => {
	assert.equal(dispositionName('attachment; filename="diag.zip"'), 'diag.zip');
	assert.equal(dispositionName('attachment'), '');
	assert.equal(dispositionName(null), '');
});

// A minimal document: download() creates a link and clicks it.
function fakeDocument() {
	const clicked = [];
	globalThis.document = {
		createElement: () => {
			const link = { href: '', download: '', click: () => clicked.push(link) };
			return link;
		},
	};
	return clicked;
}

test('api.download: saves the blob under the given name', async () => {
	const clicked = fakeDocument();
	answer(
		() =>
			new Response('a,b', {
				status: 200,
				headers: { 'Content-Disposition': 'attachment; filename="server.csv"' },
			}),
	);
	await api.download('/activity?format=csv', 'own.csv');
	assert.equal(calls[0].url, '/api/v1/activity?format=csv');
	assert.equal(clicked.length, 1);
	assert.equal(clicked[0].download, 'own.csv');
	assert.match(clicked[0].href, /^blob:/);
	URL.revokeObjectURL(clicked[0].href);
});

test('api.download: serverName prefers the name titand sends', async () => {
	const clicked = fakeDocument();
	answer(
		() =>
			new Response('zip', {
				status: 200,
				headers: { 'Content-Disposition': 'attachment; filename="titan-2026.zip"' },
			}),
	);
	await api.download('/diagnostics/bundle', 'fallback.zip', { serverName: true });
	assert.equal(clicked[0].download, 'titan-2026.zip');
	answer(() => new Response('zip', { status: 200 }));
	await api.download('/diagnostics/bundle', 'fallback.zip', { serverName: true });
	assert.equal(clicked[1].download, 'fallback.zip');
	for (const link of clicked) URL.revokeObjectURL(link.href);
});

test('api.download: a failure saves nothing and throws an ApiError', async () => {
	const clicked = fakeDocument();
	answer(() => json(404, { error: { code: 'not_found', message: 'gone' } }));
	await assert.rejects(api.download('/logs/export?boot=previous', 'x.txt'), {
		code: 'not_found',
	});
	answer(() => {
		throw new TypeError('Failed to fetch');
	});
	await assert.rejects(api.download('/x', 'x.txt'), { code: 'unreachable' });
	assert.equal(clicked.length, 0);
});
