// 1.13: the order of the side bars (preference kind "order"), Mail's
// folders in that order and the folders' state asked for once things rest.
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import {
	applyOrder,
	mergeGroup,
	moveKey,
	orderOf,
	sanitizeOrder,
	setOrder,
	sideOrderState,
} from '#lib/side-order.svelte.ts';
import { session } from '#lib/session.svelte.ts';
import {
	adjustFolders,
	arrangeFolders,
	loadFolders,
	mail,
	refreshFolders,
	ROLES,
} from '#lib/tmail/store.svelte.ts';
import type { MailFolder } from '#lib/tmail/types.ts';
import { reply } from '../components/fetch.ts';

type Call = { method: string; path: string; body: unknown };
let calls: Call[] = [];
beforeEach(() => {
	calls = [];
	vi.stubGlobal(
		'fetch',
		vi.fn((url: string, init?: RequestInit) => {
			const u = new URL(url, 'http://nas.test');
			calls.push({
				method: init?.method ?? 'GET',
				path: u.pathname.replace('/api/v1', ''),
				body: init?.body ? JSON.parse(String(init.body)) : null,
			});
			if (u.pathname.endsWith('/folders'))
				return Promise.resolve(reply(200, [f('INBOX', 'inbox', 1)]));
			return Promise.resolve(reply(204));
		}),
	);
});
afterEach(() => {
	vi.useRealTimers();
	vi.unstubAllGlobals();
	session.order = null;
	sideOrderState.error = '';
});

const f = (path: string, role: MailFolder['role'] = '', unread = 0, total = 10): MailFolder => ({
	path,
	name: path.split(/[/.]/).at(-1)!,
	role,
	unread,
	total,
	delim: '/',
});

test('what the NAS sends is kept within its bounds, never throws', () => {
	expect(sanitizeOrder(null)).toEqual({ mail: {}, contacts: [], calendar: [], mailAccounts: [], mailHidden: {} });
	expect(sanitizeOrder('x')).toEqual({ mail: {}, contacts: [], calendar: [], mailAccounts: [], mailHidden: {} });
	const o = sanitizeOrder({
		mail: { 'acc-privat': ['INBOX', 'INBOX', 3, '', 'A\nB', 'Rechnungen'], '../x': ['INBOX'] },
		contacts: ['kontakte', '../x', 'kontakte', 'Arbeit'],
		calendar: 'arbeit',
		old: [1],
	});
	expect({ ...o.mail }).toEqual({ 'acc-privat': ['INBOX', 'Rechnungen'] });
	expect(o.contacts).toEqual(['kontakte']);
	expect(o.calendar).toEqual([]);
	expect(
		sanitizeOrder({ calendar: ['3f2504e0-4f89-11d3-9a0c-0305e82c3301'] }).calendar,
	).toHaveLength(1);
	expect(
		sanitizeOrder({ contacts: Array.from({ length: 150 }, (_, i) => `b${i}`) }).contacts,
	).toHaveLength(100);
});

test('the order: named ones first, new ones after in their own order, gone ones ignored', () => {
	const items = ['a', 'b', 'c', 'd'];
	expect(applyOrder(items, [], (x) => x)).toEqual(items);
	expect(applyOrder(items, ['c', 'gone', 'a'], (x) => x)).toEqual(['c', 'a', 'b', 'd']);
	expect(moveKey(['a', 'b', 'c'], 'c', 0)).toEqual(['c', 'a', 'b']);
	expect(moveKey(['a', 'b', 'c'], 'a', 9)).toEqual(['b', 'c', 'a']);
	const same = ['a', 'b'];
	expect(moveKey(same, 'a', -3)).toBe(same);
	expect(moveKey(same, 'x', 1)).toBe(same);
	// A group sorted: its names first, the other groups' names keep theirs.
	expect(mergeGroup(['x', 'b', 'y', 'a'], ['a', 'b', 'c'])).toEqual(['a', 'b', 'c', 'x', 'y']);
});

test('saving sends only the program’s key, once after a few steps; a failure is said', async () => {
	vi.useFakeTimers();
	session.order = { calendar: ['arbeit'] };
	setOrder('mail', ['Rechnungen', 'INBOX'], { mailbox: 'acc-privat', mailboxes: ['acc-privat'] });
	setOrder('mail', ['INBOX', 'Rechnungen'], { mailbox: 'acc-privat', mailboxes: ['acc-privat'] });
	expect(orderOf('mail', 'acc-privat')).toEqual(['INBOX', 'Rechnungen']);
	expect(orderOf('calendar')).toEqual(['arbeit']);
	expect(calls).toHaveLength(0);
	await vi.advanceTimersByTimeAsync(700);
	expect(calls).toEqual([
		{
			method: 'PUT',
			path: '/preferences/order',
			body: { mail: { 'acc-privat': ['INBOX', 'Rechnungen'] } },
		},
	]);
	// A removed mailbox's order goes with the next save.
	session.order = { mail: { gone: ['INBOX'], 'acc-privat': ['INBOX'] } };
	setOrder('mail', ['Rechnungen'], { mailbox: 'acc-privat', mailboxes: ['acc-privat'] });
	await vi.advanceTimersByTimeAsync(700);
	expect(calls[1].body).toEqual({ mail: { 'acc-privat': ['Rechnungen'] } });

	vi.stubGlobal(
		'fetch',
		vi.fn(() =>
			Promise.resolve(reply(503, { error: { code: 'config_unavailable', message: 'x' } })),
		),
	);
	setOrder('contacts', ['kontakte']);
	await vi.advanceTimersByTimeAsync(700);
	expect(sideOrderState.error).toMatch(/Reihenfolge|order/);
});

test('folders: roles first, then the others with their subfolders, each group in the account’s order', () => {
	const list = [
		f('INBOX', 'inbox'),
		f('Gesendet', 'sent'),
		f('Papierkorb', 'trash'),
		f('Archiv'),
		f('Rechnungen'),
		f('Rechnungen/2025'),
		f('Rechnungen/2026'),
		f('Zeug'),
	];
	const path = (order: string[]) => arrangeFolders(list, order).map((x) => x.f.path);
	expect(path([])).toEqual([
		'INBOX',
		'Gesendet',
		'Papierkorb',
		'Archiv',
		'Rechnungen',
		'Rechnungen/2025',
		'Rechnungen/2026',
		'Zeug',
	]);
	// Rechnungen moves to the top with its subfolders; 2026 before 2025; the trash before the sent.
	expect(path(['Papierkorb', 'Rechnungen', 'Zeug', 'Rechnungen/2026', 'gone'])).toEqual([
		'Papierkorb',
		'INBOX',
		'Gesendet',
		'Rechnungen',
		'Rechnungen/2026',
		'Rechnungen/2025',
		'Zeug',
		'Archiv',
	]);
	const groups = arrangeFolders(list, []);
	expect(groups.find((x) => x.f.path === 'INBOX')!.group).toBe(ROLES);
	expect(groups.find((x) => x.f.path === 'Zeug')!.group).toBe('');
	expect(groups.find((x) => x.f.path === 'Rechnungen/2026')!.group).toBe('Rechnungen');
	// Servers with "INBOX.": below the inbox counts as the top.
	const dotted = [
		f('INBOX', 'inbox'),
		f('INBOX/Rechnungen'),
		f('INBOX/Rechnungen/2026'),
		f('INBOX/Sent', 'sent'),
	];
	const arranged = arrangeFolders(dotted, ['INBOX/Sent', 'INBOX']);
	expect(arranged.map((x) => [x.f.path, x.group])).toEqual([
		['INBOX/Sent', ROLES],
		['INBOX', ROLES],
		['INBOX/Rechnungen', ''],
		['INBOX/Rechnungen/2026', 'INBOX/Rechnungen'],
	]);
});

test('counts change at once; the NAS is asked once when things rest, not once per change', async () => {
	vi.useFakeTimers();
	mail.accounts = [
		{
			id: 'a1',
			name: 'Privat',
			email: 'alex@example.org',
			senderName: '',
			user: 'alex@example.org',
			imap: { host: 'h', port: 993, security: 'tls' },
			smtp: { host: 'h', port: 465, security: 'tls' },
			passwordSet: true,
			notify: true,
			signature: '',
			status: { state: 'ok' },
			unread: 3,
		},
	];
	mail.folders = { a1: [f('INBOX', 'inbox', 3, 10), f('Rechnungen', '', 0, 2)] };
	adjustFolders('a1', [
		{ folder: 'INBOX', unread: -1, total: -2 },
		{ folder: 'Rechnungen', unread: 1, total: 2 },
		{ folder: 'gone', unread: 5 },
	]);
	expect(mail.folders.a1!.map((x) => [x.unread, x.total])).toEqual([
		[2, 8],
		[1, 4],
	]);
	expect(mail.accounts[0].unread).toBe(2);
	adjustFolders('a1', [{ folder: 'INBOX', unread: -9 }]);
	expect(mail.folders.a1![0].unread).toBe(0);

	for (let i = 0; i < 5; i++) {
		refreshFolders('a1');
		await vi.advanceTimersByTimeAsync(1000);
	}
	expect(calls.filter((c) => c.path.endsWith('/folders'))).toHaveLength(0);
	await vi.advanceTimersByTimeAsync(3000);
	expect(calls.filter((c) => c.path.endsWith('/folders'))).toHaveLength(1);
	// Changes that never rest are read after 10 s at the latest.
	calls = [];
	for (let i = 0; i < 12; i++) {
		refreshFolders('a1');
		await vi.advanceTimersByTimeAsync(1000);
	}
	expect(calls.filter((c) => c.path.endsWith('/folders'))).toHaveLength(1);
	// Asked while a read runs: once more afterwards, not once per asking.
	calls = [];
	const first = loadFolders('a1');
	void loadFolders('a1');
	void loadFolders('a1');
	await first;
	await vi.advanceTimersByTimeAsync(10);
	expect(calls.filter((c) => c.path.endsWith('/folders'))).toHaveLength(2);
	expect(mail.folders.a1).toEqual([f('INBOX', 'inbox', 1)]);
});
