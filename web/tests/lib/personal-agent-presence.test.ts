// 1.39 (owner 28.11.2026): an Agent window in view tells the NAS so; the NAS
// then sends no push. Hidden, it says so once; gone, it says so on unload.
import { afterEach, expect, test, vi } from 'vitest';

const calls = vi.hoisted(() => ({ list: [] as { visible: boolean; keepalive?: boolean }[] }));
vi.mock('#lib/api.ts', () => ({
	api: {
		post: async (_path: string, body: { visible: boolean }) =>
			void calls.list.push({ visible: body.visible }),
	},
	apiFetch: async (_path: string, init: RequestInit) => {
		calls.list.push({ visible: JSON.parse(String(init.body)).visible, keepalive: init.keepalive });
		return new Response(null, { status: 204 });
	},
}));
let hidden = false;
vi.mock('#lib/embedded.ts', () => ({
	pageHidden: () => hidden,
	PAGE_VISIBILITY: 'titanos-visibility',
}));

const { startPresence } = await import('../../src/lib/personal-agent/presence.ts');

afterEach(() => {
	calls.list = [];
	hidden = false;
	vi.useRealTimers();
});

test('a window in view says so now and every 15 seconds', () => {
	vi.useFakeTimers();
	const stop = startPresence();
	expect(calls.list).toEqual([{ visible: true }]);
	vi.advanceTimersByTime(15_000);
	expect(calls.list.length).toBe(2);
	stop();
});

test('a hidden window says so once and then stays quiet', () => {
	vi.useFakeTimers();
	const stop = startPresence();
	hidden = true;
	document.dispatchEvent(new Event('titanos-visibility'));
	vi.advanceTimersByTime(60_000);
	expect(calls.list.map((c) => c.visible)).toEqual([true, false]);
	stop();
});

test('closing the page sends "not in view" as it unloads', () => {
	const stop = startPresence();
	window.dispatchEvent(new Event('pagehide'));
	expect(calls.list.at(-1)).toEqual({ visible: false, keepalive: true });
	stop();
});
