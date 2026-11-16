import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/svelte';
import DocsFolderPicker from '#lib/components/agent/features/DocsFolderPicker.svelte';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const dir = (name: string) => ({ name, dir: true, size: 0, modified: '2026-11-16T10:00:00Z' });

// Review 1.02: a slow answer for a folder left a moment ago listed its
// subfolders under the folder now shown; picking one gave a path that
// does not exist.
test('the picker lists the subfolders of the folder shown', async () => {
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			if (url.includes('/files/roots'))
				return reply(200, [
					{ id: 'share/A', name: 'A', kind: 'share', writable: true },
					{ id: 'share/B', name: 'B', kind: 'share', writable: true },
				]);
			if (url.includes('root=share%2FA')) {
				await sleep(300);
				return reply(200, [dir('Alt')]);
			}
			return reply(200, [dir('Neu')]);
		}),
	);
	const { container } = render(DocsFolderPicker, {
		props: { title: 'Ordner', onpick: () => {}, onclose: () => {} },
	});
	const roots = await waitFor(() => {
		const b = [...container.querySelectorAll('button')].filter((x) =>
			x.textContent?.includes('📁'),
		);
		if (b.length !== 2) throw new Error('no roots yet');
		return b;
	});
	await fireEvent.click(roots[0]); // A, slow
	const up = [...container.querySelectorAll('button')].find((x) => x.textContent === '‹')!;
	await fireEvent.click(up);
	const again = await waitFor(() => {
		const b = [...container.querySelectorAll('button')].filter((x) =>
			x.textContent?.includes('📁'),
		);
		if (b.length !== 2) throw new Error('no roots again');
		return b;
	});
	await fireEvent.click(again[1]); // B, fast
	await sleep(500);
	const shown = [...container.querySelectorAll('button')].map((x) => x.textContent ?? '');
	expect(shown.some((x) => x.includes('Neu'))).toBe(true);
	expect(shown.some((x) => x.includes('Alt'))).toBe(false);
});
