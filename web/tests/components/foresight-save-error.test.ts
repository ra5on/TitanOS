// Durchsicht 102 (A-32): a save that fails while a page of "Vorausschau" is
// open was said only in the panel's error line, which lies behind the
// sheet. The sheet now says it itself.
import { expect, test } from 'vitest';
import { fireEvent, render, waitFor, within } from '@testing-library/svelte';
import { ApiError } from '#lib/api.ts';
import type { Features } from '#lib/agent-features.ts';
import ForesightGroup from '#lib/components/agent/features/ForesightGroup.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';

const nas = {
	docs: {},
	guard: {},
	handbook: {},
	emergency: {},
	whatIf: { enabled: false },
	advisor: { enabled: false },
	buddy: {},
	catalog: {},
} as unknown as Features;

test('a failed save shows its reason in the open sheet', async () => {
	i18n.locale = 'de';
	const view = render(ForesightGroup, {
		props: {
			admin: true,
			nas,
			pub: null,
			mine: null,
			calm: false,
			savenas: async () => {
				throw new ApiError(503, 'unavailable', 'Stick nicht erreichbar');
			},
			savemine: async () => {},
		},
	});
	await fireEvent.click(view.getAllByRole('button', { name: /Was-wäre-wenn/ })[0]);
	const sheet = await waitFor(() => {
		const d = document.querySelector('dialog.fsheet');
		if (!d) throw new Error('no sheet');
		return d as HTMLElement;
	});
	await fireEvent.click(within(sheet).getByRole('switch'));
	await waitFor(() => expect(within(sheet).getByRole('alert').textContent).toBeTruthy());
	// Closing the sheet forgets it.
	await fireEvent.click(within(sheet).getByRole('button', { name: 'Zurück' }));
	await fireEvent.click(view.getAllByRole('button', { name: /Was-wäre-wenn/ })[0]);
	const again = await waitFor(() => document.querySelector('dialog.fsheet') as HTMLElement);
	expect(within(again).queryByRole('alert')).toBeNull();
});
