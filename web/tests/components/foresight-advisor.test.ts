import { afterEach, expect, test, vi } from 'vitest';
import { render, waitFor } from '@testing-library/svelte';
import ForesightAdvisor from '#lib/components/agent/features/ForesightAdvisor.svelte';
import { reply } from './fetch.ts';

// jsdom has no Web Animations: a transition ends at once.
Element.prototype.animate = () => {
	const a = { cancel() {}, finish() {}, onfinish: null as null | (() => void) };
	queueMicrotask(() => a.onfinish?.());
	return a as never;
};

afterEach(() => vi.unstubAllGlobals());

// Review 1.02: two graphics cards of the same model have the same name;
// the report keyed its rows by area and name, and Svelte stopped the page
// with a duplicate key.
test('the report shows two cards of the same model', async () => {
	const gpu = {
		area: 'gpu',
		subject: 'NVIDIA GeForce RTX 3060',
		level: 'ok',
		code: 'gpuOK',
		bytes: 12884901888,
	};
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			if (url.includes('/agent/advisor'))
				return reply(200, {
					settings: { enabled: true, prices: false },
					running: false,
					web: false,
					saved: {
						lastRun: '2026-11-16T03:00:00Z',
						history: true,
						report: { at: '2026-11-16T03:00:00Z', findings: [gpu, { ...gpu }] },
					},
				});
			return reply(200, { enabled: false, levels: [] });
		}),
	);
	render(ForesightAdvisor, { props: { onclose: () => {} } });
	await waitFor(() =>
		expect(document.body.querySelectorAll('[data-testid="advisor-rows"] .frow2').length).toBe(2),
	);
});

// "Prüft gerade" of a run of the night ended only when the page was opened
// again.
test('a run that was going when the page opened ends on the page', async () => {
	let reads = 0;
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			if (url.includes('/agent/advisor')) {
				reads++;
				return reply(200, {
					settings: { enabled: true, prices: false },
					running: reads === 1,
					web: false,
				});
			}
			return reply(200, { enabled: false, levels: [] });
		}),
	);
	render(ForesightAdvisor, { props: { onclose: () => {} } });
	await waitFor(() => expect(reads).toBe(1));
	await waitFor(() => expect(reads).toBe(2), { timeout: 5000 });
}, 10000);
