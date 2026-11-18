// 1.12, T-Mail: the folder column – favourites and folding per device,
// spam and trash without a count, a server with the namespace "INBOX."
// (every folder below the inbox): the inbox folds nothing and no folder
// with a role ever folds away.
import { afterEach, beforeEach, expect, test } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import FolderColumn from '#lib/tmail/FolderColumn.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
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
