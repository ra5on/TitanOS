// 1.05, T-Mail: the list of a folder – unread not by colour alone, marks
// with words, sender and subject whole, pages, search and filters asked
// from titand. 1.12: the preview whole (no "…", never clamped), date
// groups, conversations, quick actions, selecting several with the bar,
// deleting in the trash asks, the search over all folders names the folder.
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/svelte';
import MessageList from '#lib/tmail/MessageList.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { mail } from '#lib/tmail/store.svelte.ts';
import { reply } from './fetch.ts';

i18n.locale = 'de';

beforeEach(() => {
	mail.folders = {
		a1: [
			{ path: 'INBOX', name: 'INBOX', role: 'inbox', unread: 0, total: 0 },
			{ path: 'Papierkorb', name: 'Papierkorb', role: 'trash', unread: 0, total: 0 },
			{ path: 'Archiv', name: 'Archiv', role: 'archive', unread: 0, total: 0 },
		],
	};
});
afterEach(() => {
	vi.unstubAllGlobals();
	localStorage.clear();
});

const today = new Date();
today.setHours(8, 0, 0, 0);
const row = (uid: number, over: Record<string, unknown> = {}) => ({
	uid,
	from: { name: 'Anna Beispiel', addr: 'anna@example.org' },
	to: [],
	subject: `Betreff Nummer ${uid} mit einem sehr langen Text, der umbrechen muss`,
	date: today.toISOString(),
	flags: { seen: true, flagged: false, answered: false },
	size: 1000,
	attachments: false,
	...over,
});

type Call = { method: string; url: URL; body: unknown };
function stub(answer: (u: URL, method: string) => unknown) {
	const calls: Call[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string, init?: RequestInit) => {
			const u = new URL(url, 'http://nas.test');
			const method = init?.method ?? 'GET';
			calls.push({ method, url: u, body: init?.body ? JSON.parse(String(init.body)) : null });
			const got = answer(u, method);
			return Promise.resolve(got === undefined ? reply(204) : reply(200, got));
		}),
	);
	return calls;
}
function stubList(pages: Record<string, { messages: unknown[]; next: number | null }>) {
	return stub((u) => {
		const page = pages[u.searchParams.get('before') ?? ''] ?? { messages: [], next: null };
		return { ...page, total: 3 };
	});
}
const base = { accountId: 'a1', folder: 'INBOX', heading: 'Posteingang', onopen: () => {} };

test('unread is bold with a dot and says so; marks are symbols with words; the preview whole', async () => {
	const preview =
		'Hallo Alex, anbei die Abrechnung für November mit allen Belegen und den Zählerständen vom Ende des Monats';
	stubList({
		'': {
			messages: [
				row(3, {
					flags: { seen: false, flagged: true, answered: false, forwarded: true },
					attachments: true,
					preview,
				}),
				row(2),
			],
			next: null,
		},
	});
	const { container } = render(MessageList, base);
	const first = await screen.findByRole('button', { name: /Betreff Nummer 3/ });
	expect(first.classList.contains('unread')).toBe(true);
	expect(first.querySelector('.dot')).not.toBeNull();
	for (const word of ['Ungelesen', 'Markiert', 'Mit Anhang', 'Weitergeleitet'])
		expect(first.textContent).toContain(word);
	const second = screen.getByRole('button', { name: /Betreff Nummer 2/ });
	expect(second.classList.contains('unread')).toBe(false);
	expect(second.textContent).not.toContain('Ungelesen');
	// The whole subject and the whole preview: never shortened, never clamped.
	expect(container.textContent).toContain('mit einem sehr langen Text, der umbrechen muss');
	expect(first.querySelector('.preview')?.textContent).toBe(preview);
	expect(container.textContent).not.toContain('…');
	// Today's group heads the list.
	expect(screen.getByRole('heading', { name: 'Heute' })).toBeTruthy();
});

test('the next page asks with before=next; search and filters go to titand', async () => {
	const calls = stubList({
		'': { messages: [row(3), row(2)], next: 2 },
		'2': { messages: [row(1)], next: null },
	});
	const urls = { at: (i: number) => calls.at(i)?.url };
	const onopen = vi.fn();
	render(MessageList, { ...base, onopen });
	await fireEvent.click(await screen.findByRole('button', { name: 'Weitere Nachrichten laden' }));
	await screen.findByRole('button', { name: /Betreff Nummer 1/ });
	expect(urls.at(-1)?.searchParams.get('before')).toBe('2');
	expect(screen.queryByRole('button', { name: 'Weitere Nachrichten laden' })).toBeNull();

	await fireEvent.click(screen.getByRole('button', { name: 'Ungelesen' }));
	await waitFor(() => expect(urls.at(-1)?.searchParams.get('unread')).toBe('1'));
	expect(urls.at(-1)?.searchParams.get('folder')).toBe('INBOX');

	const search = screen.getByRole('searchbox', { name: 'In diesem Ordner suchen' });
	await fireEvent.input(search, { target: { value: 'Rechnung' } });
	await fireEvent.submit(search.closest('form')!);
	await waitFor(() => expect(urls.at(-1)?.searchParams.get('q')).toBe('Rechnung'));
	expect(urls.at(-1)?.searchParams.get('unread')).toBe('1');

	await fireEvent.click(await screen.findByRole('button', { name: /Betreff Nummer 3/ }));
	expect(onopen).toHaveBeenCalledWith(3, 'INBOX');
});

test('a conversation is one entry with its count and opens', async () => {
	stubList({
		'': {
			messages: [
				row(5, { subject: 'Re: Termin', messageId: '<c@x>', references: '<a@x> <b@x>' }),
				row(4, { subject: 'Anderes', messageId: '<z@x>' }),
				row(3, { subject: 'Re: Termin', messageId: '<b@x>', inReplyTo: '<a@x>' }),
				row(2, { subject: 'Termin', messageId: '<a@x>' }),
			],
			next: null,
		},
	});
	render(MessageList, base);
	const toggle = await screen.findByRole('button', {
		name: /Unterhaltung aufklappen: 3 Nachrichten/,
	});
	expect(toggle.getAttribute('aria-expanded')).toBe('false');
	expect(screen.getAllByRole('button', { name: /Re: Termin|^Termin/ }).length).toBe(1);
	await fireEvent.click(toggle);
	expect(toggle.getAttribute('aria-expanded')).toBe('true');
	expect(screen.getAllByRole('checkbox', { name: /Termin/ }).length).toBe(3);
	// Off per device: one entry each, remembered.
	await fireEvent.click(screen.getByRole('checkbox', { name: 'Unterhaltungen zusammenfassen' }));
	expect(screen.queryByRole('button', { name: /Unterhaltung aufklappen/ })).toBeNull();
	expect(localStorage.getItem('titanos.mail.threads')).toBe('false');
});

test('several selected: the bar archives them in one request; Shift selects a range', async () => {
	const calls = stub((u, method) => {
		if (method === 'POST' && u.pathname.endsWith('/archive')) return { to: 'Archiv' };
		if (u.pathname.endsWith('/messages'))
			return { messages: [row(4), row(3), row(2), row(1)], next: null, total: 4 };
	});
	const onremoved = vi.fn();
	render(MessageList, { ...base, onremoved });
	const boxes = await screen.findAllByRole('checkbox', { name: /Auswählen/ });
	await fireEvent.click(boxes[0]);
	await fireEvent.click(boxes[2], { shiftKey: true });
	const bar = screen.getByRole('toolbar', { name: 'Aktionen für die Auswahl' });
	expect(within(bar).getByRole('status').textContent).toBe('3 ausgewählt');
	await fireEvent.click(within(bar).getByRole('button', { name: 'Archivieren' }));
	await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/archive'))).toBe(true));
	const archive = calls.find((c) => c.url.pathname.endsWith('/archive'))!;
	expect(archive.body).toEqual({ folder: 'INBOX', uids: [4, 3, 2] });
	await screen.findByText('3 archiviert.');
	expect(screen.queryByRole('button', { name: /Betreff Nummer 4/ })).toBeNull();
	expect(onremoved).toHaveBeenCalled();
});

test('in the trash a quick delete asks first; elsewhere it goes to the trash at once', async () => {
	const calls = stub((u) => {
		if (u.pathname.endsWith('/messages')) return { messages: [row(9)], next: null, total: 1 };
	});
	render(MessageList, { ...base, folder: 'Papierkorb', heading: 'Papierkorb' });
	await screen.findByRole('button', { name: /Betreff Nummer 9/ });
	await fireEvent.click(screen.getByRole('button', { name: 'Endgültig löschen' }));
	expect(calls.some((c) => c.url.pathname.endsWith('/delete'))).toBe(false);
	const dialog = await screen.findByRole('dialog');
	expect(dialog.textContent).toContain('Endgültig löschen?');
	await fireEvent.click(within(dialog).getByRole('button', { name: 'Endgültig löschen' }));
	await waitFor(() => expect(calls.some((c) => c.url.pathname.endsWith('/delete'))).toBe(true));
});

test('the search over all folders asks /search and names the folder of each hit', async () => {
	const calls = stub((u) => {
		if (u.pathname.endsWith('/search'))
			return {
				messages: [{ ...row(7, { subject: 'Rechnung Gas' }), folder: 'Archiv' }],
				total: 1,
				folders: 3,
				truncated: true,
			};
		if (u.pathname.endsWith('/messages')) return { messages: [], next: null, total: 0 };
	});
	const onopen = vi.fn();
	render(MessageList, { ...base, onopen });
	await fireEvent.click(await screen.findByRole('button', { name: 'Allen Ordnern' }));
	// Nothing to search for yet: no request over every folder.
	await screen.findByText(/Gib einen Suchbegriff ein/);
	expect(calls.some((c) => c.url.pathname.endsWith('/search'))).toBe(false);
	const search = screen.getByRole('searchbox', { name: 'In allen Ordnern suchen' });
	await fireEvent.input(search, { target: { value: 'rechnung' } });
	await fireEvent.submit(search.closest('form')!);
	const hit = await screen.findByRole('button', { name: /Rechnung Gas/ });
	expect(hit.textContent).toContain('in Archiv');
	expect(calls.find((c) => c.url.pathname.endsWith('/search'))?.url.searchParams.get('q')).toBe(
		'rechnung',
	);
	expect(screen.getByText(/Nicht alle Ordner wurden durchsucht/)).toBeTruthy();
	await fireEvent.click(hit);
	expect(onopen).toHaveBeenCalledWith(7, 'Archiv');
	// The filters go along: sender, attachment, period.
	await fireEvent.click(screen.getByRole('button', { name: 'Weitere Filter' }));
	await fireEvent.click(screen.getByRole('checkbox', { name: 'Mit Anhang' }));
	await waitFor(() =>
		expect(
			calls
				.filter((c) => c.url.pathname.endsWith('/search'))
				.at(-1)
				?.url.searchParams.get('attachment'),
		).toBe('1'),
	);
});
