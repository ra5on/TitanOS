import { beforeEach, expect, test, vi } from 'vitest';

// The queue talks to titand only through api.ts; a small fake NAS stands in.
const nas = vi.hoisted(() => ({
	calls: [] as string[],
	exists: false,
	received: 0,
	fail: '' as string,
}));

vi.mock('../../src/lib/api.ts', async (original) => {
	const real = await original<typeof import('../../src/lib/api.ts')>();
	const json = (body: unknown, status = 200) =>
		new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
	return {
		...real,
		api: {
			post: async (path: string, body?: { conflict?: string; size?: number }) => {
				nas.calls.push(`POST ${path}${body?.conflict ? ` ${body.conflict}` : ''}`);
				if (path === '/files/uploads') {
					if (nas.exists && !body?.conflict) throw new real.ApiError(409, 'file_exists', '');
					return { id: 'u1', name: body?.conflict === 'rename' ? 'a (1).txt' : 'a.txt', offset: 0 };
				}
				if (path.endsWith('/finish')) return { id: 'u1', name: 'a.txt', offset: nas.received };
				throw new Error(path);
			},
			get: async (path: string) => {
				nas.calls.push(`GET ${path}`);
				return { id: 'u1', offset: nas.received, busy: false };
			},
			delete: async (path: string) => {
				nas.calls.push(`DELETE ${path}`);
			},
		},
		apiFetch: async (path: string, init: { body: Blob }) => {
			nas.calls.push(`PATCH ${path}`);
			if (nas.fail) return json({ error: { code: nas.fail } }, 400);
			nas.received += init.body.size;
			return json({ id: 'u1', offset: nas.received });
		},
	};
});

const queue = await import('#lib/uploads.svelte.ts');
const root = { id: 'share/data', name: 'data' };
const file = () => new File(['hello'], 'a.txt');
const settled = () => vi.waitFor(() => expect(queue.uploads.items.every((i) => !i.busy)).toBe(true));

beforeEach(() => {
	Object.assign(nas, { calls: [], exists: false, received: 0, fail: '' });
	queue.uploads.items = [];
	queue.uploads.finished = 0;
	queue.uploads.paused = false;
});

test('an upload is created, sent and finished', async () => {
	queue.enqueue([{ file: file(), dir: 'docs' }], root);
	await vi.waitFor(() => expect(queue.uploads.items[0].state).toBe('done'));
	expect(nas.calls).toEqual([
		'POST /files/uploads',
		'PATCH /files/uploads/u1?offset=0',
		'POST /files/uploads/u1/finish',
	]);
	expect(queue.uploads.items[0].sent).toBe(5);
	expect(queue.uploads.finished).toBe(1);
	queue.clearFinished();
	expect(queue.uploads.items).toEqual([]);
});

test('an existing name waits for a decision, "keep both" sends rename', async () => {
	nas.exists = true;
	queue.enqueue([{ file: file(), dir: '' }], root);
	await vi.waitFor(() => expect(queue.uploads.items[0].state).toBe('conflict'));
	queue.resolve(queue.uploads.items[0], 'rename');
	await vi.waitFor(() => expect(queue.uploads.items[0].state).toBe('done'));
	expect(nas.calls).toContain('POST /files/uploads rename');
});

test('skipping a conflict removes the file from the queue', async () => {
	nas.exists = true;
	queue.enqueue([{ file: file(), dir: '' }], root);
	await vi.waitFor(() => expect(queue.uploads.items[0]?.state).toBe('conflict'));
	queue.resolve(queue.uploads.items[0], 'skip');
	expect(queue.uploads.items).toEqual([]);
});

test('an error from titand fails the upload; retry asks where it stands', async () => {
	nas.fail = 'file_no_space';
	queue.enqueue([{ file: file(), dir: '' }], root);
	await vi.waitFor(() => expect(queue.uploads.items[0].state).toBe('failed'));
	expect(queue.uploads.items[0].error).toBe('file_no_space');
	nas.fail = '';
	nas.calls = [];
	queue.retryFailed();
	await vi.waitFor(() => expect(queue.uploads.items[0].state).toBe('done'));
	expect(nas.calls[0]).toMatch(/^GET \/files\/uploads\?root=share%2Fdata/);
	expect(nas.calls).toContain('GET /files/uploads/u1');
});

test('a paused queue holds new files until it is resumed', async () => {
	queue.pauseAll();
	queue.enqueue([{ file: file(), dir: '' }], root);
	expect(queue.uploads.items[0].state).toBe('paused');
	expect(nas.calls).toEqual([]);
	queue.resumeAll();
	await vi.waitFor(() => expect(queue.uploads.items[0].state).toBe('done'));
});

test('cancel removes what arrived', async () => {
	nas.fail = 'internal';
	queue.enqueue([{ file: file(), dir: '' }], root);
	await vi.waitFor(() => expect(queue.uploads.items[0].state).toBe('failed'));
	await settled();
	await queue.cancel(queue.uploads.items[0]);
	expect(nas.calls).toContain('DELETE /files/uploads/u1');
	expect(queue.uploads.items).toEqual([]);
});

test('recovering a reselected file checks its size first', async () => {
	await expect(
		queue.recover(file(), root, '', { id: 'u9', name: 'a.txt', size: 99, offset: 0 } as never),
	).rejects.toMatchObject({ code: 'upload_mismatch' });
});
