// 1.03 (owner 16 Nov 2026): what fails on the desktop is said in a short
// notice at the bottom instead of being swallowed (Durchsicht 102,
// D102-40/41/42).
import { afterEach, beforeEach, expect, test, vi } from 'vitest';

const nas = vi.hoisted(() => ({ calls: [] as string[], refuse: '' }));

vi.mock('../../src/lib/api.ts', async (original) => {
	const real = await original<typeof import('../../src/lib/api.ts')>();
	const answer = async (method: string, path: string) => {
		nas.calls.push(`${method} ${path}`);
		if (nas.refuse && path.includes(nas.refuse))
			throw new real.ApiError(409, 'docker_busy', 'busy');
		return {};
	};
	return {
		...real,
		apiFetch: async (path: string, init: RequestInit = {}) => {
			nas.calls.push(`${init.method} ${path}${init.keepalive ? ' keepalive' : ''}`);
			return new Response(null, { status: nas.refuse && path.includes(nas.refuse) ? 409 : 204 });
		},
		api: {
			get: (path: string) => answer('GET', path),
			post: (path: string) => answer('POST', path),
			put: (path: string) => answer('PUT', path),
		},
	};
});

import { dockerAction } from '#lib/desktop/desk-ops.ts';
import { sendFilesToFileManager } from '#lib/desktop/desk-messages.ts';
import { sources } from '#lib/desktop/sources.svelte.ts';
import { desktop, flushPrefs, savePrefs } from '#lib/desktop/wm.svelte.ts';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { dismissToast, toasts } from '#lib/toast.svelte.ts';

beforeEach(() => {
	i18n.locale = 'de';
	nas.calls = [];
	nas.refuse = '';
});
afterEach(() => {
	for (const x of toasts.list) dismissToast(x.id);
	vi.useRealTimers();
});

const container = (id: string, app: string) =>
	({ id, name: id, app, state: 'running' }) as unknown as (typeof sources.containers)[number];

test('a refused start of a Docker app names the app and the reason', async () => {
	sources.containers = [container('c1', 'immich'), container('c2', 'immich')];
	nas.refuse = '/c2/';
	await dockerAction('docker:immich', 'start');
	// Every container is still asked; the refusal of one is said once.
	expect(nas.calls).toEqual([
		'POST /docker/containers/c1/start',
		'POST /docker/containers/c2/start',
	]);
	expect(toasts.list).toHaveLength(1);
	expect(toasts.list[0].tone).toBe('error');
	expect(toasts.list[0].text).toMatch(/^immich ließ sich nicht starten: /);
});

test('a Docker app that does what it is told shows nothing', async () => {
	sources.containers = [container('c1', 'immich')];
	await dockerAction('docker:immich', 'restart');
	expect(toasts.list).toHaveLength(0);
});

test('a desktop setting that could not be saved is said', async () => {
	vi.useFakeTimers();
	desktop.from = 1;
	nas.refuse = '/preferences/desktop';
	savePrefs((p) => (p.lockMinutes = 7));
	expect(toasts.list).toHaveLength(0);
	await vi.advanceTimersByTimeAsync(1600);
	expect(nas.calls).toContain('PUT /preferences/desktop');
	expect(toasts.list.map((x) => x.text)).toEqual([
		expect.stringMatching(/^Die Einstellungen des Arbeitsplatzes ließen sich nicht speichern/),
	]);
	// A second failing change does not pile up a second notice.
	savePrefs((p) => (p.lockMinutes = 8));
	await vi.advanceTimersByTimeAsync(1600);
	expect(toasts.list).toHaveLength(1);
});

test('other tabs get a desktop setting only once the NAS has kept it', async () => {
	vi.useFakeTimers();
	const told = vi.spyOn(BroadcastChannel.prototype, 'postMessage');
	desktop.from = 1;
	nas.refuse = '/preferences/desktop';
	savePrefs((p) => (p.lockMinutes = 9));
	await vi.advanceTimersByTimeAsync(1600);
	expect(nas.calls).toContain('PUT /preferences/desktop');
	expect(told).not.toHaveBeenCalled();
	nas.refuse = '';
	savePrefs((p) => (p.lockMinutes = 10));
	await vi.advanceTimersByTimeAsync(1600);
	expect(told).toHaveBeenCalledTimes(1);
	expect(told.mock.calls[0][0]).toMatchObject({ prefs: { lockMinutes: 10 } });
	told.mockRestore();
});

test('a desktop setting changed just before a reload is still sent', async () => {
	vi.useFakeTimers();
	desktop.from = 1;
	savePrefs((p) => (p.lockMinutes = 11));
	// The page goes before the timer ran out.
	await vi.advanceTimersByTimeAsync(300);
	flushPrefs();
	expect(nas.calls).toEqual(['PUT /preferences/desktop keepalive']);
	// It is sent once, not again by the timer.
	await vi.advanceTimersByTimeAsync(2000);
	expect(nas.calls).toEqual(['PUT /preferences/desktop keepalive']);
	// Nothing waiting: nothing is sent.
	flushPrefs();
	expect(nas.calls).toHaveLength(1);
});

test('files dropped on the desktop that the file manager never takes are said', async () => {
	vi.useFakeTimers();
	sendFilesToFileManager([new File(['x'], 'a.txt'), new File(['y'], 'b.txt')]);
	await vi.advanceTimersByTimeAsync(700 * 20);
	expect(toasts.list).toHaveLength(0);
	await vi.advanceTimersByTimeAsync(700);
	expect(toasts.list.map((x) => x.text)).toEqual([expect.stringContaining('(2)')]);
});

test('files the file manager takes are not reported', async () => {
	vi.useFakeTimers();
	sendFilesToFileManager([new File(['x'], 'a.txt')]);
	await vi.advanceTimersByTimeAsync(700);
	// The answer of the file manager (it names the drop it took).
	window.dispatchEvent(
		new MessageEvent('message', {
			origin: location.origin,
			data: { type: 'titanos-upload-ok' },
		}),
	);
	await vi.advanceTimersByTimeAsync(700 * 25);
	expect(toasts.list).toHaveLength(0);
});
