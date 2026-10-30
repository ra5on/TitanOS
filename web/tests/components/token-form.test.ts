import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import ApiPage from '../../src/routes/settings/api/+page.svelte';
import { session } from '#lib/session.svelte.ts';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

function nas(twoFactor: boolean) {
	session.data = {
		configAvailable: true,
		setupRequired: false,
		hostname: 'nas',
		user: { name: 'alex', role: 'owner', twoFactor },
	};
	const posts: Record<string, unknown>[] = [];
	const fetch = vi.fn(async (url: string, init?: RequestInit) => {
		if (url.endsWith('/account/tokens') && init?.method === 'POST') {
			const body = JSON.parse(String(init.body));
			posts.push(body);
			if (body.code !== '654321')
				return reply(403, { error: { code: 'invalid_code', message: 'wrong code' } });
			return reply(201, { secret: 'tos_secret' });
		}
		if (url.endsWith('/account/tokens'))
			return reply(200, { tokens: [], scopes: ['system:read', 'files:read'] });
		if (url.endsWith('/system/mcp')) return reply(200, { enabled: false });
		return reply(404, { error: { code: 'not_found', message: 'none' } });
	});
	vi.stubGlobal('fetch', fetch);
	return posts;
}

async function openCreate() {
	render(ApiPage);
	const create = await screen.findAllByRole('button', { name: /create token|token erstellen/i });
	await fireEvent.click(create[0]);
	return document.querySelector('dialog form') as HTMLFormElement;
}

test('a token needs the password and, with 2FA, a code; a wrong code is cleared', async () => {
	const posts = nas(true);
	const form = await openCreate();
	const name = form.querySelector('input:not([type=checkbox])') as HTMLInputElement;
	await fireEvent.input(name, { target: { value: 'backup script' } });
	const submit = form.querySelector('.dialog-actions .btn-primary') as HTMLButtonElement;
	expect(submit.disabled).toBe(true);
	const password = form.querySelector('input[type=password]') as HTMLInputElement;
	const code = form.querySelector('input[autocomplete=one-time-code]') as HTMLInputElement;
	expect(code).toBeTruthy();
	await fireEvent.input(password, { target: { value: 'my password' } });
	expect(submit.disabled).toBe(true);
	await fireEvent.input(code, { target: { value: '111111' } });
	expect(submit.disabled).toBe(false);

	await fireEvent.submit(form);
	await waitFor(() => expect(form.querySelector('[role=alert]')).toBeTruthy());
	expect(posts[0]).toMatchObject({
		name: 'backup script',
		password: 'my password',
		code: '111111',
	});
	expect(code.value).toBe('');

	await fireEvent.input(code, { target: { value: '654321' } });
	await fireEvent.submit(form);
	await waitFor(() => expect(posts).toHaveLength(2));
	await waitFor(() =>
		expect(
			[...document.querySelectorAll('textarea')].some((area) => area.value === 'tos_secret'),
		).toBe(true),
	);
});

test('without 2FA only the password is asked for', async () => {
	nas(false);
	const form = await openCreate();
	expect(form.querySelector('input[type=password]')).toBeTruthy();
	expect(form.querySelector('input[autocomplete=one-time-code]')).toBeNull();
});
