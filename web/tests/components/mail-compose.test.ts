// 1.05, T-Mail: writing – suggestions from the address book with the
// keyboard, the draft saved every 30 s while it changes (and only then),
// a failed send says why, throwing away asks first.
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import AddressField from '#lib/tmail/AddressField.svelte';
import Compose from '../../src/routes/mail/compose/+page.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { mail } from '#lib/tmail/store.svelte.ts';
import type { MailAccount } from '#lib/tmail/types.ts';
import { page } from '$app/state';
import { reply } from './fetch.ts';

i18n.locale = 'de';

const account: MailAccount = {
	id: 'a1',
	name: 'Privat',
	email: 'me@example.org',
	user: 'me@example.org',
	imap: { host: 'imap.example.org', port: 993, security: 'tls' },
	smtp: { host: 'smtp.example.org', port: 465, security: 'tls' },
	passwordSet: true,
	notify: true,
	signature: 's1',
	status: { state: 'ok' },
	unread: 0,
};

type Call = { method: string; url: URL; body: unknown };
let calls: Call[] = [];
let sendAnswer: () => Response = () => reply(204);
let drafts = 0;
const names = new Map<string, string>();

beforeEach(() => {
	calls = [];
	drafts = 0;
	page.url = new URL('http://nas.test/mail/compose');
	sendAnswer = () => reply(204);
	mail.accounts = [account];
	mail.folders = {
		a1: [{ path: 'Drafts', name: 'Drafts', role: 'drafts', unread: 0, total: 0 }],
	};
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string, init?: RequestInit) => {
			const u = new URL(url, 'http://nas.test');
			calls.push({ method: init?.method ?? 'GET', url: u, body: init?.body });
			if (u.pathname.endsWith('/mail/contacts'))
				return Promise.resolve(
					reply(200, [
						{ name: 'Anna Beispiel', addr: 'anna@example.org', count: 3, last: '' },
						{ name: '', addr: 'anton@example.org', count: 1, last: '' },
					]),
				);
			if (u.pathname.endsWith('/mail/settings'))
				return Promise.resolve(
					reply(200, { signatures: [{ id: 's1', name: 'Kurz', text: 'Gruß, Max' }], rules: [] }),
				);
			if (u.pathname.endsWith('/draft')) {
				const form = init?.body as FormData;
				const sent = form.getAll('file') as File[];
				const meta = JSON.parse(form.get('meta') as string);
				// Like titand: the new draft holds the uploads and what attachFrom names.
				const kept = (meta.attachFrom?.parts ?? []).map((part: string) => ({
					part,
					name: names.get(part) ?? part,
					type: 'text/plain',
					size: 3,
					inline: false,
					cid: '',
				}));
				const fresh = sent.map((f, i) => ({
					part: String(10 + i),
					name: f.name,
					type: 'text/plain',
					size: f.size,
					inline: false,
					cid: '',
				}));
				for (const f of fresh) names.set(f.part, f.name);
				return Promise.resolve(
					reply(200, { uid: 41 + drafts++, attachments: [...kept, ...fresh] }),
				);
			}
			if (u.pathname.endsWith('/message'))
				return Promise.resolve(
					reply(200, {
						uid: 7,
						folder: 'INBOX',
						from: { name: 'Anna', addr: 'anna@example.org' },
						to: [{ name: '', addr: 'me@example.org' }],
						cc: [],
						replyTo: [],
						subject: 'Frage',
						date: '2026-11-16T09:00:00Z',
						messageId: '<m7@example.org>',
						inReplyTo: '',
						references: '<m1@example.org>',
						flags: { seen: true, flagged: false, answered: false },
						text: 'Kommst du?',
						html: '',
						externalImages: false,
						attachments: [],
						truncated: false,
					}),
				);
			if (u.pathname.endsWith('/send')) return Promise.resolve(sendAnswer());
			return Promise.resolve(reply(204));
		}),
	);
});
afterEach(() => {
	vi.useRealTimers();
	vi.unstubAllGlobals();
	mail.accounts = null;
	mail.folders = {};
});

test('suggestions come from the address book and are picked with the keyboard', async () => {
	const onchange = vi.fn();
	const view = render(AddressField, { id: 'to', label: 'An:', values: [], onchange });
	const input = screen.getByRole('combobox');
	await fireEvent.input(input, { target: { value: 'an' } });
	const list = await screen.findByRole('listbox');
	expect(calls.at(-1)?.url.searchParams.get('q')).toBe('an');
	expect(list.textContent).toContain('anna@example.org');
	await fireEvent.keyDown(input, { key: 'ArrowDown' });
	expect(input.getAttribute('aria-activedescendant')).toBe('to-opt-0');
	await fireEvent.keyDown(input, { key: 'Enter' });
	expect(screen.getByText('Anna Beispiel <anna@example.org>')).toBeTruthy();
	expect(onchange).toHaveBeenCalled();
	// A typed address is taken at a comma; an invalid one is marked in words.
	await fireEvent.input(input, { target: { value: 'kaputt,' } });
	expect(screen.getByText('kaputt')).toBeTruthy();
	expect(view.container.textContent).toContain('keine gültige Adresse');
});

test('the draft is saved after 30 s of changes, not before and not without one', async () => {
	vi.useFakeTimers({ shouldAdvanceTime: true });
	render(Compose);
	const subject = await screen.findByLabelText('Betreff');
	await vi.advanceTimersByTimeAsync(31_000);
	expect(calls.some((c) => c.url.pathname.endsWith('/draft'))).toBe(false);

	await fireEvent.input(subject, { target: { value: 'Treffen' } });
	await vi.advanceTimersByTimeAsync(20_000);
	expect(calls.some((c) => c.url.pathname.endsWith('/draft'))).toBe(false);
	await vi.advanceTimersByTimeAsync(11_000);
	const draft = calls.find((c) => c.url.pathname.endsWith('/draft'))!;
	expect(draft.method).toBe('POST');
	const meta = JSON.parse((draft.body as FormData).get('meta') as string);
	expect(meta).toMatchObject({ subject: 'Treffen', draftUid: 0, signature: 's1' });
	await screen.findByText(/Entwurf gespeichert um/);

	// The next save replaces this draft.
	await fireEvent.input(subject, { target: { value: 'Treffen am Freitag' } });
	await vi.advanceTimersByTimeAsync(31_000);
	const drafts = calls.filter((c) => c.url.pathname.endsWith('/draft'));
	expect(drafts.length).toBe(2);
	expect(JSON.parse((drafts[1].body as FormData).get('meta') as string).draftUid).toBe(41);
});

test('sending without a recipient says so; a refused send shows the server answer', async () => {
	render(Compose);
	await fireEvent.click(await screen.findByRole('button', { name: /Senden/ }));
	expect((await screen.findByRole('alert')).textContent).toContain('mindestens einen Empfänger');
	expect(calls.some((c) => c.url.pathname.endsWith('/send'))).toBe(false);

	sendAnswer = () =>
		reply(502, { error: { code: 'mail_send_failed', message: '550 mailbox unavailable' } });
	// The field of the recipients (the sender's choice is a combobox too).
	const to = screen.getByLabelText('An:');
	await fireEvent.input(to, { target: { value: 'ben@example.org,' } });
	await fireEvent.click(screen.getByRole('button', { name: /Senden/ }));

	await waitFor(() =>
		expect(screen.getByRole('alert').textContent).toContain('550 mailbox unavailable'),
	);
	expect(screen.getByRole('alert').textContent).toContain('nicht angenommen');
});

test('throwing away a draft asks first, with Abbrechen first', async () => {
	render(Compose);
	const subject = await screen.findByLabelText('Betreff');
	await fireEvent.input(subject, { target: { value: 'Etwas' } });
	await fireEvent.click(screen.getByRole('button', { name: /Verwerfen/ }));
	const dialog = await screen.findByRole('dialog', { name: 'E-Mail verwerfen?' });
	const buttons = dialog.querySelectorAll('.dialog-actions button');
	expect(buttons[0].textContent).toBe('Abbrechen');
	expect(buttons[1].classList.contains('btn-danger')).toBe(true);
});

test('an uploaded file goes up once; later drafts take it from the previous one', async () => {
	vi.useFakeTimers({ shouldAdvanceTime: true });
	const { container } = render(Compose);
	await screen.findByLabelText('Betreff');
	const input = container.querySelector('input[type=file]') as HTMLInputElement;
	const file = new File(['abc'], 'plan.txt', { type: 'text/plain' });
	Object.defineProperty(input, 'files', { value: [file], configurable: true });
	await fireEvent.change(input);
	await vi.advanceTimersByTimeAsync(31_000);
	await fireEvent.input(screen.getByLabelText('Betreff'), { target: { value: 'Plan' } });
	await vi.advanceTimersByTimeAsync(31_000);
	const saves = calls.filter((c) => c.url.pathname.endsWith('/draft'));
	expect(saves.length).toBe(2);
	const first = saves[0].body as FormData;
	expect((first.getAll('file') as File[]).map((f) => f.name)).toEqual(['plan.txt']);
	const second = saves[1].body as FormData;
	expect(second.getAll('file')).toEqual([]);
	expect(JSON.parse(second.get('meta') as string)).toMatchObject({
		draftUid: 41,
		attachFrom: { parts: ['10'] },
	});
	expect(JSON.parse(second.get('meta') as string).attachFrom.uid).toBeUndefined();
	// Still listed, now as part of the draft.
	expect(container.textContent).toContain('plan.txt');
});

test('a reply names the mail it answers (answers), with references and quote', async () => {
	page.url = new URL('http://nas.test/mail/compose?a=a1&mode=reply&f=INBOX&uid=7');
	render(Compose);
	await screen.findByDisplayValue('Re: Frage');
	await fireEvent.click(screen.getByRole('button', { name: /Senden/ }));
	await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/send'))).toBe(true));
	const send = calls.find((c) => c.url.pathname.endsWith('/send'))!;
	const meta = JSON.parse((send.body as FormData).get('meta') as string);
	expect(meta).toMatchObject({
		to: ['Anna <anna@example.org>'],
		inReplyTo: '<m7@example.org>',
		references: '<m1@example.org> <m7@example.org>',
		answers: { folder: 'INBOX', uid: 7 },
	});
	expect(meta.text).toContain('> Kommst du?');
});
