// 1.12, T-Mail: the folder column – favourites and folding per device,
// spam and trash without a count, a server with the namespace "INBOX."
// (every folder below the inbox): the inbox folds nothing and no folder
// with a role ever folds away.
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import FolderColumn from '#lib/tmail/FolderColumn.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { session } from '#lib/session.svelte.ts';
import { mail } from '#lib/tmail/store.svelte.ts';
import type { MailFolder } from '#lib/tmail/types.ts';

i18n.locale = 'de';

const f = (path: string, role: MailFolder['role'] = '', unread = 0): MailFolder => ({
	path,
	name: path.split('.').at(-1)!,
	role,
	unread,
	total: 9,
	delim: '.',
});

beforeEach(() => {
	mail.accounts = [
		{
			id: 'a1',
			name: 'Privat',
			email: 'alex@example.org',
			senderName: '',
			user: 'alex@example.org',
			imap: { host: 'imap.example.org', port: 993, security: 'tls' },
			smtp: { host: 'smtp.example.org', port: 465, security: 'tls' },
			passwordSet: true,
			notify: true,
			signature: '',
			status: { state: 'ok' },
			unread: 3,
		},
	];
	mail.folders = {
		a1: [
			f('INBOX', 'inbox', 3),
			f('INBOX.Sent', 'sent'),
			f('INBOX.Trash', 'trash', 4),
			f('INBOX.Spam', 'junk', 7),
			f('INBOX.Rechnungen', '', 1),
			f('INBOX.Rechnungen.2026'),
		],
	};
});
afterEach(() => localStorage.clear());

const folderButton = (name: string) =>
	screen
		.getAllByRole('button')
		.find((b) => b.classList.contains('folder') && b.textContent?.includes(name));

test('INBOX namespace: the inbox folds nothing, roles never fold away, levels from below the inbox', async () => {
	render(FolderColumn, { account: 'a1', folder: 'INBOX', onpick: () => {} });
	expect(screen.queryByRole('button', { name: /Unterordner von „Posteingang“/ })).toBeNull();
	const fold = screen.getByRole('button', { name: 'Unterordner von „Rechnungen“ verbergen' });
	expect(folderButton('Rechnungen')!.style.paddingLeft).toContain('0 * 14px');
	expect(folderButton('2026')!.style.paddingLeft).toContain('1 * 14px');
	await fireEvent.click(fold);
	expect(folderButton('2026')).toBeUndefined();
	expect(folderButton('Gesendet')).toBeDefined();
	expect(localStorage.getItem('titanos.mail.folded')).toContain('INBOX.Rechnungen');
});

test('spam and trash show no count; favourites go on top and stay', async () => {
	const first = render(FolderColumn, { account: 'a1', folder: 'INBOX', onpick: () => {} });
	expect(folderButton('Posteingang')!.querySelector('.count')).not.toBeNull();
	expect(folderButton('Papierkorb')!.querySelector('.count')).toBeNull();
	expect(folderButton('Spam')!.querySelector('.count')).toBeNull();
	await fireEvent.click(screen.getByRole('button', { name: '„Rechnungen“ als Favorit anheften' }));
	expect(screen.getByRole('heading', { name: 'Favoriten' })).toBeTruthy();
	first.unmount();
	render(FolderColumn, { account: 'a1', folder: 'INBOX', onpick: () => {} });
	expect(screen.getByRole('heading', { name: 'Favoriten' })).toBeTruthy();
	expect(
		screen
			.getAllByRole('button', { name: '„Rechnungen“ aus den Favoriten lösen' })[0]
			.getAttribute('aria-pressed'),
	).toBe('true');
});

test('1.13: folders sort among their siblings with the keys, saved for the account', async () => {
	vi.useFakeTimers();
	const put = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
	vi.stubGlobal('fetch', put);
	render(FolderColumn, { account: 'a1', folder: 'INBOX', sortable: true, onpick: () => {} });
	const names = () =>
		[...document.querySelectorAll('.folder .name')].map((e) => e.textContent?.trim());
	expect(names()).toEqual(['Posteingang', 'Gesendet', 'Spam', 'Papierkorb', 'Rechnungen', '2026']);
	// A group of one has nothing to sort: grips only for the four folders with a role.
	expect(screen.getAllByRole('button', { name: /verschieben$/ })).toHaveLength(4);
	await fireEvent.keyDown(folderButton('Posteingang')!, { key: 'ArrowDown', altKey: true });
	expect(names()).toEqual(['Gesendet', 'Posteingang', 'Spam', 'Papierkorb', 'Rechnungen', '2026']);
	expect(session.order).toMatchObject({
		mail: { a1: ['INBOX.Sent', 'INBOX', 'INBOX.Spam', 'INBOX.Trash'] },
	});
	await vi.advanceTimersByTimeAsync(700);
	expect(put).toHaveBeenCalledTimes(1);
	const [url, init] = put.mock.calls[0] as unknown as [string, RequestInit];
	expect(url).toContain('/preferences/order');
	expect(JSON.parse(String(init.body))).toEqual({
		mail: { a1: ['INBOX.Sent', 'INBOX', 'INBOX.Spam', 'INBOX.Trash'] },
	});
	vi.useRealTimers();
	vi.unstubAllGlobals();
	session.order = null;
});
