import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import AccountDetail from '../../src/routes/users/AccountDetail.svelte';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

const account = (name: string) =>
	({ name, role: 'user', created: '2026-11-01T00:00:00Z', twoFactor: false }) as never;
const props = (name: string) => ({
	account: account(name),
	me: 'owner',
	shares: [],
	manageable: true,
	mayLock: true,
	offeredRoles: ['user'] as never,
	working: false,
	created: '1.11.2026',
	lockedTitle: '',
	onrole: () => {},
	onpassword: () => {},
	onlock: () => {},
	ondelete: () => {},
	onreset2fa: () => {},
	onshares: () => {},
});
const apps = (app: string, allowed: boolean) => ({
	admin: false,
	newApps: false,
	apps: [{ app, icon: '', allowed }],
});

// Durchsicht 102: the answer to a change for one account must not show (and
// later be saved) under another account selected meanwhile.
test('a late answer to a change of rights stays with its account', async () => {
	let slow: (r: Response) => void = () => {};
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string, init?: RequestInit) => {
			const path = new URL(url, 'http://nas').pathname;
			if (init?.method === 'PUT') return new Promise<Response>((r) => (slow = r));
			if (path.endsWith('/anna/apps')) return Promise.resolve(reply(200, apps('immich', false)));
			if (path.endsWith('/ben/apps')) return Promise.resolve(reply(200, apps('jellyfin', false)));
			return Promise.resolve(reply(404, { error: { code: 'not_found', message: '' } }));
		}),
	);
	const view = render(AccountDetail, props('anna'));
	const box = await screen.findByRole('checkbox', { name: /immich/ });
	await fireEvent.click(box);
	await view.rerender(props('ben'));
	await screen.findByText('jellyfin');
	slow(reply(200, apps('immich', true)));
	await new Promise((r) => setTimeout(r, 20));
	expect(screen.queryByText('immich')).toBeNull();
	await waitFor(() => expect(screen.getByText('jellyfin')).toBeTruthy());
});
