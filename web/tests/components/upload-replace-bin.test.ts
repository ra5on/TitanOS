import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import UploadReview from '../../src/routes/files/UploadReview.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

// S-43 (owner 17.11.2026): an upload that replaces a file says, like the
// copy dialog, whether the old file goes to the recycle bin or is gone.
async function replaceIn(recycle: boolean) {
	i18n.locale = 'de';
	const root = {
		id: 'share/Docs',
		name: 'Docs',
		kind: 'share' as const,
		writable: true,
		recycle,
		totalBytes: 100,
		freeBytes: 50,
	};
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string) =>
			Promise.resolve(
				url.includes('/files/roots')
					? reply(200, [root])
					: reply(200, [{ name: 'a.txt', dir: false, size: 1, modified: '2026-11-17T08:00:00Z' }]),
			),
		),
	);
	render(UploadReview, {
		root,
		dir: '',
		files: [{ file: new File(['neu'], 'a.txt'), path: 'a.txt' }],
		onclose: () => {},
		onconfirm: async () => {},
	});
	const select = await screen.findByRole('combobox');
	await fireEvent.change(select, { target: { value: 'replace' } });
}

test('with a recycle bin the replaced file goes there', async () => {
	await replaceIn(true);
	expect(await screen.findByText(/kommen in den Papierkorb der Freigabe/)).toBeTruthy();
});

test('without a recycle bin the dialog says the old file is gone', async () => {
	await replaceIn(false);
	expect(await screen.findByText(/endgültig weg/)).toBeTruthy();
});
