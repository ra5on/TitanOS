// 1.05, T-Mail: the setup assistant – address, servers proposed by
// /providers, the app-password hint, "Verbindung prüfen" with its result in
// words, saving with the password in the request body only (never in the
// browser's storage).
import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import SetupWizard from '#lib/tmail/SetupWizard.svelte';
import { lockedSeconds } from '#lib/tmail/checklock.svelte.ts';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { reply } from './fetch.ts';

i18n.locale = 'de';

afterEach(() => vi.unstubAllGlobals());

const SECRET = 'abcd-efgh-ijkl-mnop';

function stub(opts: { known?: boolean; test?: unknown; limited?: boolean } = {}) {
	const calls: { method: string; url: URL; body: string }[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string, init?: RequestInit) => {
			const u = new URL(url, 'http://nas.test');
			calls.push({ method: init?.method ?? 'GET', url: u, body: String(init?.body ?? '') });
			if (u.pathname.endsWith('/mail/providers'))
				return Promise.resolve(
					opts.known === false
						? reply(200, {
								known: false,
								id: '',
								name: '',
								domains: [],
								imap: { host: '', port: 0, security: '' },
								smtp: { host: '', port: 0, security: '' },
								appPassword: false,
								supported: false,
								hint: '',
							})
						: reply(200, {
								known: true,
								id: 'gmail',
								name: 'Gmail',
								domains: ['gmail.com'],
								imap: { host: 'imap.gmail.com', port: 993, security: 'tls' },
								smtp: { host: 'smtp.gmail.com', port: 465, security: 'tls' },
								appPassword: true,
								supported: true,
								hint: 'app_password_2fa',
							}),
				);
			if (u.pathname.endsWith('/mail/accounts/test'))
				return Promise.resolve(
					opts.limited
						? new Response(
								JSON.stringify({ error: { code: 'mail_rate_limited', message: 'slow down' } }),
								{
									status: 429,
									headers: { 'Content-Type': 'application/json', 'Retry-After': '60' },
								},
							)
						: reply(200, opts.test ?? { imap: { ok: true }, smtp: { ok: true } }),
				);
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
	await screen.findByText(/Gmail ist bekannt/);
	expect(calls[0].url.searchParams.get('email')).toBe('max@gmail.com');
	expect(screen.getByText(/imap.gmail.com:993/)).toBeTruthy();
	await fireEvent.click(screen.getByRole('button', { name: 'Weiter' }));

	await screen.findByText(/verlangt ein App-Passwort/);
	expect(screen.getByText(/Gmail: Mit Anmeldung in zwei Schritten/)).toBeTruthy();
	const password = screen.getByLabelText('App-Passwort');
	expect(password.getAttribute('type')).toBe('password');
	await fireEvent.input(password, { target: { value: SECRET } });
	await fireEvent.click(screen.getByRole('button', { name: 'Verbindung prüfen' }));
	const result = await screen.findByRole('list', { name: 'Ergebnis der Prüfung' });
	expect(result.textContent).toContain('Eingang (IMAP): Anmeldung klappt');
	expect(result.textContent).toContain('Ausgang (SMTP): Anmeldung klappt');

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
	expect(result.textContent).toContain('Keine Verbindung zum Mailserver');
	expect(screen.getByRole('button', { name: 'Trotzdem speichern' })).toBeTruthy();
});

test('too many checks lock "Verbindung prüfen" for the time titand names', async () => {
	stub({ limited: true });
	render(SetupWizard, { onsaved: () => {} });
	await fireEvent.input(screen.getByLabelText('Adresse'), { target: { value: 'max@gmail.com' } });
	await fireEvent.click(screen.getByRole('button', { name: 'Weiter' }));
	await screen.findByText(/Gmail ist bekannt/);
	await fireEvent.click(screen.getByRole('button', { name: 'Weiter' }));
	await fireEvent.input(await screen.findByLabelText('App-Passwort'), { target: { value: 'x' } });
	await fireEvent.click(screen.getByRole('button', { name: 'Verbindung prüfen' }));
	// The reason stands at the top of every page of T-Mail (routes/mail/+layout.svelte).
	await waitFor(() => expect(lockedSeconds()).toBe(60));
	expect(
		(screen.getByRole('button', { name: 'Verbindung prüfen' }) as HTMLButtonElement).disabled,
	).toBe(true);
});
