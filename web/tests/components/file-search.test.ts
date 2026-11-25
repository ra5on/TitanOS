import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import type { FileRoot } from '#lib/api.ts';
import SearchDialog from '../../src/routes/files/SearchDialog.svelte';
import { FilePreferenceStore } from '../../src/routes/files/preferences.svelte.ts';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

test('a restored search rechecks access and offers preview and selection of the same nested hit', async () => {
	const root: FileRoot = {
		id: 'share/docs',
		name: 'Dokumente',
		kind: 'share',
		writable: true,
		recycle: false,
		totalBytes: 0,
		freeBytes: 0,
	};
	const hit = {
		path: '2025/Rechnung.pdf',
		name: 'Rechnung.pdf',
		dir: false,
		size: 1,
		modified: '2025-04-01T00:00:00Z',
	};
	const position = {
		root: root.id,
		dir: '',
		query: 'Rechnung',
		content: false,
		extension: 'pdf',
		after: '',
		before: '',
		scroll: 15,
	};
	const fetch = vi
		.fn()
		.mockResolvedValue(reply(200, { entries: [hit], scanned: 2, truncated: false }));
	vi.stubGlobal('fetch', fetch);
	const onopen = vi.fn(),
		onpreview = vi.fn();
	const prefs = new FilePreferenceStore(() => ({ root: root.id, path: '' }));
	render(SearchDialog, {
		root,
		roots: [root],
		dir: '',
		showHidden: false,
		initial: position,
		prefs,
		onopen,
		onpreview,
		onclose: () => {},
	});
	const preview = await screen.findByRole('button', { name: /Vorschau|Preview/i });
	expect(JSON.parse(fetch.mock.calls[0][1].body)).toMatchObject({
		root: root.id,
		query: 'Rechnung',
		extension: 'pdf',
	});
	await fireEvent.click(preview);
	expect(onpreview).toHaveBeenCalledWith(root.id, hit);
	await fireEvent.click(screen.getByRole('button', { name: /In Dateien zeigen|Show in Files/i }));
	await waitFor(() =>
		expect(onopen).toHaveBeenCalledWith(
			expect.objectContaining({ root: root.id, query: 'Rechnung' }),
			hit.path,
			hit.name,
		),
	);
});
