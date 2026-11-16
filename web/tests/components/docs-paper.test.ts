import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/svelte';
import DocsViews from '#lib/components/agent/features/DocsViews.svelte';
import { reply } from './fetch.ts';

// jsdom has no Web Animations: a transition ends at once.
Element.prototype.animate = () => {
	const a = { cancel() {}, finish() {}, onfinish: null as null | (() => void) };
	queueMicrotask(() => a.onfinish?.());
	return a as never;
};

afterEach(() => vi.unstubAllGlobals());

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

const summary = (month: string) => ({
	month,
	months: ['2026-09', '2026-10', '2026-11'],
	total: 0,
	count: 0,
	prevTotal: 0,
	types: [],
	open: [],
	recent: [],
});

// Review 1.02: the view asked twice for every month (the effect followed
// the month the buttons set), and the slower answer to an earlier month
// could stay on screen after a later one was chosen.
test('the paperwork asks once per month and shows the last one chosen', async () => {
	const asked: string[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			const month = new URL(url, 'http://x').searchParams.get('month') ?? '';
			asked.push(month);
			// September answers late.
			if (month === '2026-09') await sleep(400);
			return reply(200, summary(month || '2026-11'));
		}),
	);
	render(DocsViews, { props: { id: 'paper', onclose: () => {} } });
	const buttons = await waitFor(() => {
		const b = document.body.querySelectorAll<HTMLButtonElement>('.seg button');
		if (b.length !== 3) throw new Error('no months yet');
		return b;
	});
	await sleep(100);
	expect(asked).toEqual(['']);
	await fireEvent.click(buttons[0]);
	await fireEvent.click(buttons[1]);
	await sleep(700);
	expect(asked).toEqual(['', '2026-09', '2026-10']);
	const pressed = document.body.querySelector('.seg button[aria-pressed="true"]');
	expect(pressed).toBe(document.body.querySelectorAll('.seg button')[1]);
});
