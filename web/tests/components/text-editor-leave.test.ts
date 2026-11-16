// 1.03 (D102-47): leaving a page with changes in the text editor asks in
// the editor's own dialog, not with window.confirm; "Änderungen verwerfen"
// goes on to where the page was going.
import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import TextEditor from '../../src/routes/files/TextEditor.svelte';
import { forgetGuards, gotoCalls, tryNavigate } from './stubs/app-navigation.ts';
import { reply } from './fetch.ts';
import { i18n } from '#lib/i18n/index.svelte.ts';

afterEach(() => {
	vi.unstubAllGlobals();
	forgetGuards();
});

test('a navigation with changes asks in the dialog and goes on after discarding', async () => {
	vi.stubGlobal(
		'fetch',
		vi.fn(async () => reply(200, { content: 'alt', version: 'v1' })),
	);
	i18n.locale = 'de';
	const confirm = vi.fn(() => true);
	vi.stubGlobal('confirm', confirm);
	const onclose = vi.fn();
	render(TextEditor, { root: 'data', path: 'notiz.txt', onclose });
	const area = (await screen.findByLabelText('notiz.txt')) as HTMLTextAreaElement;
	await waitFor(() => expect(area.value).toBe('alt'));
	// Without changes nothing stops the way.
	expect(tryNavigate('/docker')).toBe(false);
	await fireEvent.input(area, { target: { value: 'neu' } });
	expect(tryNavigate('/docker')).toBe(true);
	expect(confirm).not.toHaveBeenCalled();
	await screen.findByText('Ungespeicherte Änderungen verwerfen?');
	// Weiter bearbeiten: stays, nothing is lost.
	await fireEvent.click(screen.getByRole('button', { name: 'Weiter bearbeiten' }));
	expect(area.value).toBe('neu');
	expect(gotoCalls).toEqual([]);
	// Again, and discard: the navigation goes on, once.
	tryNavigate('/docker');
	await fireEvent.click(await screen.findByRole('button', { name: 'Änderungen verwerfen' }));
	expect(gotoCalls).toEqual(['http://nas.test/docker']);
	// The editor closes too (a way inside the file manager would keep it open).
	expect(onclose).toHaveBeenCalledTimes(1);
	expect(tryNavigate('/docker')).toBe(false);
});
