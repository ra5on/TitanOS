import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/svelte';
import Planned from '#lib/components/agent/Planned.svelte';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

// 1.01: switching another task read the list again, and the open form of
// the morning report went back to what was saved – the typed wish was gone.
test('the morning report keeps what was typed when another task is switched', async () => {
	let reads = 0;
	const puts: Record<string, unknown>[] = [];
	const list = () => ({
		schedules: [
			{
				id: 'm1',
				kind: 'morning',
				name: 'Morgenbericht',
				task: 'Morgenbericht',
				every: 'weekdays',
				weekdays: [1, 2, 3, 4, 5],
				at: '07:00',
				enabled: true,
				report: { nas: true },
				next: `2026-11-1${7 + reads}T07:00:00Z`,
			},
			{
				id: 't1',
				name: 'Platten prüfen',
				task: 'Sieh nach',
				every: 'daily',
				at: '09:00',
				enabled: puts.length === 0,
				next: '2026-11-17T09:00:00Z',
			},
		],
	});
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string, init?: RequestInit) => {
			if (url.includes('/agent/schedules/') && init?.method === 'PUT') {
				puts.push(JSON.parse(String(init.body)));
				return reply(200, list());
			}
			if (url.endsWith('/agent/schedules')) {
				reads++;
				return reply(200, list());
			}
			return reply(200, {});
		}),
	);
	const { container } = render(Planned, { props: { onopenchat: () => {} } });
	const open = await waitFor(() => {
		const b = container.querySelector<HTMLButtonElement>('button.m-open');
		if (!b) throw new Error('no morning report yet');
		return b;
	});
	await fireEvent.click(open);
	const wish = container.querySelector<HTMLTextAreaElement>('#morning-body textarea')!;
	await fireEvent.input(wish, { target: { value: 'Und das Wetter' } });
	const time = container.querySelector<HTMLInputElement>('#morning-body input[type=time]')!;
	await fireEvent.input(time, { target: { value: '06:30' } });

	// Another task is paused: the list is read again.
	const other = container.querySelector<HTMLInputElement>('li input[role=switch]')!;
	const before = reads;
	await fireEvent.click(other);
	await waitFor(() => expect(reads).toBeGreaterThan(before));
	await waitFor(() => expect(container.querySelector('li.off')).toBeTruthy());
	expect(puts.at(-1)).toMatchObject({ id: 't1', enabled: false });
	expect(container.querySelector<HTMLTextAreaElement>('#morning-body textarea')!.value).toBe(
		'Und das Wetter',
	);
	expect(container.querySelector<HTMLInputElement>('#morning-body input[type=time]')!.value).toBe(
		'06:30',
	);
});
