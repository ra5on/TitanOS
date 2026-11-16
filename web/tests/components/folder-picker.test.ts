import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import FolderPicker from '#lib/components/FolderPicker.svelte';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

// Durchsicht 102: a slow answer for a folder left meanwhile must not list its
// children under the folder now shown (the pick would get a wrong path).
test('a late answer for a folder left meanwhile is dropped', async () => {
	let slow: (r: Response) => void = () => {};
	const fetch = vi.fn((url: string) => {
		const dir = new URL(url, 'http://nas').searchParams.get('dir');
		if (dir === 'Fotos') return new Promise<Response>((r) => (slow = r));
		return Promise.resolve(reply(200, { folders: dir ? [] : ['Fotos', 'Musik'] }));
	});
	vi.stubGlobal('fetch', fetch);
	const onpick = vi.fn();
	render(FolderPicker, {
		volumes: ['data'],
		shares: [],
		start: { volume: 'data', path: '' },
		onpick,
		onclose: () => {},
	});
	await fireEvent.click(await screen.findByRole('button', { name: /Fotos/ }));
	// Back to the top before the slow folder answers.
	await fireEvent.click(screen.getByRole('button', { name: 'data' }));
	await screen.findByRole('button', { name: /Musik/ });
	slow(reply(200, { folders: ['2024'] }));
	await new Promise((r) => setTimeout(r, 20));
	expect(screen.queryByRole('button', { name: /2024/ })).toBeNull();
	await waitFor(() => expect(screen.getByRole('button', { name: /Musik/ })).toBeTruthy());
});
