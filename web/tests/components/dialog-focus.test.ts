import { expect, test } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import DialogProbe from './fixtures/DialogProbe.svelte';

// Durchsicht 102: closing a dialog with its own button left the focus on
// nothing; keyboard users had to start again at the top of the page.
test('closing a dialog gives the focus back to what opened it', async () => {
	render(DialogProbe);
	const opener = screen.getByRole('button', { name: 'open' });
	opener.focus();
	await fireEvent.click(opener);
	const cancel = await screen.findByRole('button', { name: 'cancel' });
	cancel.focus();
	await fireEvent.click(cancel);
	await Promise.resolve();
	await Promise.resolve();
	expect(document.activeElement).toBe(opener);
});
