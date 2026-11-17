// 1.05, T-Mail: the setup assistant – address, servers proposed by
// /providers, the app-password hint, "Verbindung prüfen" with its result in
// words, saving with the password in the request body only (never in the
// browser's storage).
import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import SetupWizard from '#lib/tmail/SetupWizard.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { reply } from './fetch.ts';

i18n.locale = 'de';

afterEach(() => vi.unstubAllGlobals());

const SECRET = 'abcd-efgh-ijkl-mnop';

function stub(opts: { known?: boolean; test?: unknown } = {}) {
	const calls: { method: string; url: URL; body: string }[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string, init?: RequestInit) => {
			const u = new URL(url, 'http://nas.test');
			calls.push({ method: init?.method ?? 'GET', url: u, body: String(init?.body ?? '') });
			if (u.pathname.endsWith('/mail/providers'))
				return Promise.resolve(
					opts.known === false
						? reply(404, { error: { code: 'mail_not_found', message: '' } })
						: reply(200, {
								imap: { host: 'imap.gmail.com', port: 993, security: 'tls' },
								smtp: { host: 'smtp.gmail.com', port: 465, security: 'tls' },
								appPassword: true,
							}),
				);
			if (u.pathname.endsWith('/mail/accounts/test'))
				return Promise.resolve(reply(200, opts.test ?? { imap: { ok: true }, smtp: { ok: true } }));
			if (u.pathname.endsWith('/mail/accounts') && init?.method === 'POST')
				return Promise.resolve(
					reply(200, { id: 'n1', ...JSON.parse(String(init.body)), password: undefined }),
				);
			return Promise.resolve(reply(204));
		}),
	);
	return calls;
}

test('address → proposed servers → app password hint → check → save', async () => {
	const calls = stub();
	const onsaved = vi.fn();
	render(SetupWizard, { onsaved });
	await fireEvent.input(screen.getByLabelText('Adresse'), { target: { value: 'max@gmail.com' } });
	await fireEvent.click(screen.getByRole('button', { name: 'Weiter' }));
	await screen.findByText(/Die Server für gmail.com sind bekannt/);
	expect(calls[0].url.searchParams.get('email')).toBe('max@gmail.com');
	expect(screen.getByText(/imap.gmail.com:993/)).toBeTruthy();
	await fireEvent.click(screen.getByRole('button', { name: 'Weiter' }));

	await screen.findByText(/verlangt ein App-Passwort/);
	expect(screen.getByText(/App-Passwörter/)).toBeTruthy();
	const password = screen.getByLabelText('App-Passwort');
	expect(password.getAttribute('type')).toBe('password');
	await fireEvent.input(password, { target: { value: SECRET } });
	await fireEvent.click(screen.getByRole('button', { name: 'Verbindung prüfen' }));
	const result = await screen.findByRole('list', { name: 'Ergebnis der Prüfung' });
	expect(result.textContent).toContain('IMAP: Anmeldung klappt');
	expect(result.textContent).toContain('SMTP: Anmeldung klappt');

	await fireEvent.click(screen.getByRole('button', { name: 'Speichern' }));
	await waitFor(() => expect(onsaved).toHaveBeenCalled());
	const post = calls.find((c) => c.method === 'POST' && c.url.pathname.endsWith('/mail/accounts'))!;
	expect(JSON.parse(post.body)).toMatchObject({
		email: 'max@gmail.com',
		user: 'max@gmail.com',
		password: SECRET,
		imap: { host: 'imap.gmail.com', port: 993, security: 'tls' },
	});
	// The password went to titand only: nothing in the browser keeps it.
	const stored = [localStorage, sessionStorage]
		.flatMap((s) => Object.keys(s).map((k) => s.getItem(k) ?? ''))
		.join('\n');
	expect(stored).not.toContain(SECRET);
	expect(calls.every((c) => !c.url.href.includes(SECRET))).toBe(true);
});

test('an unknown provider opens the server fields; a failed check names the cause', async () => {
	stub({
		known: false,
		test: {
			imap: { ok: false, code: 'mail_auth_failed' },
			smtp: { ok: false, code: 'mail_unreachable' },
		},
	});
	render(SetupWizard, { onsaved: () => {} });
	await fireEvent.input(screen.getByLabelText('Adresse'), {
		target: { value: 'max@eigene.example' },
	});
	await fireEvent.click(screen.getByRole('button', { name: 'Weiter' }));
	await screen.findByText(/kennt T-Mail keine Server/);
	const host = document.getElementById('mail-setup-imap-host') as HTMLInputElement;
	expect(host.value).toBe('imap.eigene.example');
	await fireEvent.click(screen.getByRole('button', { name: 'Weiter' }));
	await fireEvent.input(screen.getByLabelText('Passwort des Postfachs'), {
		target: { value: 'geheim' },
	});
	await fireEvent.click(screen.getByRole('button', { name: 'Verbindung prüfen' }));
	const result = await screen.findByRole('list', { name: 'Ergebnis der Prüfung' });
	expect(result.textContent).toContain('Benutzername oder Passwort stimmen nicht');
	expect(result.textContent).toContain('nicht erreichbar');
	expect(screen.getByRole('button', { name: 'Trotzdem speichern' })).toBeTruthy();
});
