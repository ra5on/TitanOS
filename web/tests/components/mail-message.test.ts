// 1.05, T-Mail: one message – the HTML in a frame with the contract's
// sandbox and CSP, external pictures only after "Bilder laden", recipients
// open on request, link targets listed, deleting in the trash asks first.
import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import MessageView from '#lib/tmail/MessageView.svelte';
import { mail } from '#lib/tmail/store.svelte.ts';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { reply } from './fetch.ts';

i18n.locale = 'de';

afterEach(() => {
	vi.unstubAllGlobals();
	mail.folders = {};
});

// A document as titand sends it: its own CSP meta first, pictures held back.
const doc = (images: boolean) =>
	'<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src \'none\'; style-src \'unsafe-inline\'; img-src data:' +
	(images ? ' https:' : '') +
	"; font-src 'none'; form-action 'none'; base-uri 'none'\"><meta name=\"referrer\" content=\"no-referrer\"></head><body><p>Hallo</p>" +
	(images ? '<img src="https://track.example/p.gif">' : '<img src="data:image/svg+xml,x">') +
	'<a href="https://evil.example/x" target="_blank" rel="noopener noreferrer">www.bank.example</a></body></html>';

const message = (over: Record<string, unknown> = {}) => ({
	uid: 7,
	folder: 'INBOX',
	from: { name: 'Bank', addr: 'info@bank.example' },
	to: [
		{ name: 'Ich', addr: 'me@example.org' },
		{ name: 'Ben', addr: 'ben@example.org' },
		{ name: 'Cleo', addr: 'cleo@example.org' },
	],
	cc: [{ name: 'Dora', addr: 'dora@example.org' }],
	replyTo: [],
	subject: 'Ihr Konto',
	date: '2026-11-16T09:00:00Z',
	messageId: '<m7@bank.example>',
	inReplyTo: '',
	references: '',
	flags: { seen: false, flagged: false, answered: false },
	text: '',
	html: doc(false),
	externalImages: true,
	attachments: [
		{
			part: '2',
			name: 'Kontoauszug.pdf',
			type: 'application/pdf',
			size: 52000,
			inline: false,
			cid: '',
		},
	],
	truncated: false,
	...over,
});

function stub(msg = message()) {
	const calls: { method: string; url: URL; body: unknown }[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string, init?: RequestInit) => {
			const u = new URL(url, 'http://nas.test');
			calls.push({ method: init?.method ?? 'GET', url: u, body: init?.body });
			if (u.pathname.endsWith('/message'))
				return Promise.resolve(
					reply(200, { ...msg, html: msg.html && doc(u.searchParams.get('images') === '1') }),
				);
			return Promise.resolve(reply(204));
		}),
	);
	return calls;
}

const props = {
	accountId: 'a1',
	folder: 'INBOX',
	uid: 7,
	oncompose: () => {},
	onflags: () => {},
	onremoved: () => {},
};

test("titand's document goes into the frame unchanged, without scripts or same origin", async () => {
	stub();
	const { container } = render(MessageView, props);
	await screen.findByRole('heading', { name: 'Ihr Konto' });
	const frame = container.querySelector('iframe')!;
	expect(frame.getAttribute('sandbox')).toBe('allow-popups allow-popups-to-escape-sandbox');
	expect(frame.getAttribute('srcdoc')).toBe(doc(false));
});

test('external pictures are blocked until "Bilder laden"', async () => {
	const calls = stub();
	const { container } = render(MessageView, props);
	await screen.findByText(/Externe Bilder blockiert/);
	expect(calls[0].url.searchParams.get('images')).toBe('0');
	await fireEvent.click(screen.getByRole('button', { name: 'Bilder laden' }));
	await waitFor(() => expect(calls.at(-1)?.url.searchParams.get('images')).toBe('1'));
	await waitFor(() =>
		expect(container.querySelector('iframe')!.getAttribute('srcdoc')).toBe(doc(true)),
	);
	expect(screen.queryByText(/Externe Bilder blockiert/)).toBeNull();
});

test('recipients open on request; links and attachments are listed', async () => {
	stub();
	const { container } = render(MessageView, props);
	await screen.findByRole('heading', { name: 'Ihr Konto' });
	expect(container.textContent).toContain('Ich, Ben und 2 weitere');
	expect(screen.queryByText(/dora@example.org/)).toBeNull();
	const toggle = screen.getByRole('button', { name: 'Alle Empfänger zeigen' });
	expect(toggle.getAttribute('aria-expanded')).toBe('false');
	await fireEvent.click(toggle);
	expect(screen.getByText(/Dora <dora@example.org>/)).toBeTruthy();
	await waitFor(() => expect(container.textContent).toContain('Linkziele in dieser Nachricht (1)'));
	expect(container.textContent).toContain('der Link führt zu evil.example');
	const download = screen.getByRole('link', { name: /Herunterladen/ });
	expect(download.getAttribute('href')).toBe(
		'/api/v1/mail/accounts/a1/part?folder=INBOX&uid=7&part=2',
	);
	expect(screen.getByRole('button', { name: /In Dateien speichern/ })).toBeTruthy();
});

test('in the trash, deleting asks first; elsewhere it goes to the trash at once', async () => {
	const calls = stub();
	mail.folders = {
		a1: [
			{ path: 'INBOX', name: 'INBOX', role: 'inbox', unread: 0, total: 1 },
			{ path: 'Trash', name: 'Trash', role: 'trash', unread: 0, total: 1 },
		],
	};
	const onremoved = vi.fn();
	const view = render(MessageView, { ...props, onremoved });
	await fireEvent.click(await screen.findByRole('button', { name: /^Löschen$/ }));
	await waitFor(() => expect(onremoved).toHaveBeenCalledWith(7));
	expect(calls.some((c) => c.method === 'POST' && c.url.pathname.endsWith('/delete'))).toBe(true);
	view.unmount();

	const removedInTrash = vi.fn();
	render(MessageView, { ...props, folder: 'Trash', onremoved: removedInTrash });
	await fireEvent.click(await screen.findByRole('button', { name: /Endgültig löschen/ }));
	const dialog = await screen.findByRole('dialog', { name: 'Endgültig löschen?' });
	expect(removedInTrash).not.toHaveBeenCalled();
	// "Abbrechen" first in the markup (it gets the focus); the danger button only by a click.
	const buttons = dialog.querySelectorAll('.dialog-actions button');
	expect(buttons[0].textContent).toBe('Abbrechen');
	await fireEvent.click(buttons[1]);
	await waitFor(() => expect(removedInTrash).toHaveBeenCalledWith(7));
});
