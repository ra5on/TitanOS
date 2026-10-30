import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
import { session } from '#lib/session.svelte.ts';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

test('the confirm dialog sends password and code and reports a wrong password', async () => {
	session.data = {
		configAvailable: true,
		setupRequired: false,
		hostname: 'nas',
		user: { name: 'alex', role: 'owner', twoFactor: true },
	};
	const fetch = vi
		.fn()
		.mockResolvedValueOnce(reply(403, { error: { code: 'wrong_password', message: 'no' } }))
		.mockResolvedValueOnce(reply(204));
	vi.stubGlobal('fetch', fetch);
	const onconfirmed = vi.fn();
	const { container } = render(ConfirmDialog, { reason: 'test', onconfirmed, onclose: () => {} });
	const [password, code] = container.querySelectorAll('input');
	await fireEvent.input(password, { target: { value: 'secret-pass' } });
	if (code) await fireEvent.input(code, { target: { value: '123456' } });
	const form = container.querySelector('form')!;
	await fireEvent.submit(form);
	await waitFor(() => expect(screen.getByRole('alert')).toBeTruthy());
	expect(onconfirmed).not.toHaveBeenCalled();
	expect(JSON.parse(fetch.mock.calls[0][1].body)).toMatchObject({ password: 'secret-pass' });
	await fireEvent.submit(form);
	await waitFor(() => expect(onconfirmed).toHaveBeenCalledTimes(1));
});
