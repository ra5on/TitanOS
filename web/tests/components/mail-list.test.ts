// 1.05, T-Mail: the list of a folder – unread not by colour alone, marks
// with words, sender and subject whole (no preview line, nothing cut),
// pages, search and filters asked from titand.
import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import MessageList from '#lib/tmail/MessageList.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { reply } from './fetch.ts';

i18n.locale = 'de';

afterEach(() => vi.unstubAllGlobals());

const row = (uid: number, over: Record<string, unknown> = {}) => ({
	uid,
	from: { name: 'Anna Beispiel', addr: 'anna@example.org' },
	to: [],
	subject: `Betreff Nummer ${uid} mit einem sehr langen Text, der umbrechen muss`,
	date: '2026-11-16T09:00:00Z',
	flags: { seen: true, flagged: false, answered: false },
	size: 1000,
	attachments: false,
	...over,
});

function stubList(pages: Record<string, { messages: unknown[]; next: number | null }>) {
	const urls: URL[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string) => {
			const u = new URL(url, 'http://nas.test');
			urls.push(u);
			const page = pages[u.searchParams.get('before') ?? ''] ?? { messages: [], next: null };
			return Promise.resolve(reply(200, { ...page, total: 3 }));
		}),
	);
	return urls;
}

test('unread is bold with a dot and says so; marks are symbols with words; no preview', async () => {
	stubList({
		'': {
			messages: [
				row(3, {
					flags: { seen: false, flagged: true, answered: false },
					attachments: true,
				}),
				row(2),
			],
			next: null,
		},
	});
	const { container } = render(MessageList, {
		accountId: 'a1',
		folder: 'INBOX',
		heading: 'Posteingang',
		onopen: () => {},
	});
	const first = await screen.findByRole('button', { name: /Betreff Nummer 3/ });
	expect(first.classList.contains('unread')).toBe(true);
	expect(first.querySelector('.dot')).not.toBeNull();
	expect(first.textContent).toContain('Ungelesen');
	expect(first.textContent).toContain('Markiert');
	expect(first.textContent).toContain('Mit Anhang');
	const second = screen.getByRole('button', { name: /Betreff Nummer 2/ });
	expect(second.classList.contains('unread')).toBe(false);
	expect(second.textContent).not.toContain('Ungelesen');
	// The whole subject, never shortened, and no line with the text of the mail.
	expect(container.textContent).toContain('mit einem sehr langen Text, der umbrechen muss');
	expect(container.textContent).not.toContain('…');
	expect(container.querySelector('[class*="preview"]')).toBeNull();
});

test('the next page asks with before=next; search and filters go to titand', async () => {
	const urls = stubList({
		'': { messages: [row(3), row(2)], next: 2 },
		'2': { messages: [row(1)], next: null },
	});
	const onopen = vi.fn();
	render(MessageList, { accountId: 'a1', folder: 'INBOX', heading: 'Posteingang', onopen });
	await fireEvent.click(await screen.findByRole('button', { name: 'Weitere Nachrichten laden' }));
	await screen.findByRole('button', { name: /Betreff Nummer 1/ });
	expect(urls.at(-1)?.searchParams.get('before')).toBe('2');
	expect(screen.queryByRole('button', { name: 'Weitere Nachrichten laden' })).toBeNull();

	await fireEvent.click(screen.getByRole('button', { name: 'Ungelesen' }));
	await waitFor(() => expect(urls.at(-1)?.searchParams.get('unread')).toBe('1'));
	expect(urls.at(-1)?.searchParams.get('folder')).toBe('INBOX');

	const search = screen.getByRole('searchbox');
	await fireEvent.input(search, { target: { value: 'Rechnung' } });
	await fireEvent.submit(search.closest('form')!);
	await waitFor(() => expect(urls.at(-1)?.searchParams.get('q')).toBe('Rechnung'));
	expect(urls.at(-1)?.searchParams.get('unread')).toBe('1');

	await fireEvent.click(await screen.findByRole('button', { name: /Betreff Nummer 3/ }));
	expect(onopen).toHaveBeenCalledWith(3);
});
