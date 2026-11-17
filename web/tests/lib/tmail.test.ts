// 1.05, T-Mail: the plain parts – the frame's sandbox, the links of a mail
// with their real target, dates and addresses,
// the levels in the address, the live stream's blocks and the draft's turn.
import { afterEach, expect, test, vi } from 'vitest';
import { appAllowed, SYSTEM_APPS, taskPage } from '#lib/desktop/apps.ts';
import { createAutosave } from '#lib/tmail/autosave.ts';
import { MAIL_SANDBOX, mailLinks } from '#lib/tmail/html.ts';
import { levelOf, mailHref, mailUp, placeOf } from '#lib/tmail/levels.ts';
import { parseBlock, pauseFor } from '#lib/tmail/live.svelte.ts';
import {
	forwardSubject,
	linkify,
	listDate,
	quote,
	replyRecipients,
	replySubject,
	validAddress,
} from '#lib/tmail/text.ts';

afterEach(() => vi.useRealTimers());

test('the sandbox is that of the contract', () => {
	expect(MAIL_SANDBOX).toBe('allow-popups allow-popups-to-escape-sandbox');
	expect(MAIL_SANDBOX).not.toMatch(/allow-scripts|allow-same-origin/);
});

test("links are read from an inert copy, with their real target and titand's mismatch mark", () => {
	const html =
		'<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src \'none\'"></head><body>' +
		'<a href="https://evil.example/login" title="https://evil.example/login" data-tmail-mismatch="1">www.sparkasse.de</a><span>⚠ evil.example</span> ' +
		'<a href="https://shop.example/a">Hier</a> <a href="mailto:a@b.example">Schreiben</a> ' +
		'<a href="javascript:alert(1)">x</a><img src="x" onerror="globalThis.ranInTest=1">' +
		'</body></html>';
	const links = mailLinks(html);
	expect(links.map((l) => [l.href, l.warn])).toEqual([
		['https://evil.example/login', 'evil.example'],
		['https://shop.example/a', ''],
		['mailto:a@b.example', ''],
	]);
	expect(links[0].host).toBe('evil.example');
	// Only titand's mark counts: a text naming another domain without it is no warning here.
	expect(mailLinks('<a href="https://a.example">www.b.example</a>')[0].warn).toBe('');
	expect((globalThis as Record<string, unknown>).ranInTest).toBeUndefined();
});

test('dates, subjects, quotes and recipients', () => {
	const now = new Date('2026-11-17T15:00:00');
	expect(listDate('2026-11-17T09:05:00', 'de', now)).toMatch(/09:05/);
	expect(listDate('2026-11-16T09:05:00', 'de', now)).toMatch(/Mo/);
	expect(listDate('2025-11-16T09:05:00', 'de', now)).toMatch(/2025/);
	expect(replySubject('AW: Termin')).toBe('AW: Termin');
	expect(replySubject('Termin')).toBe('Re: Termin');
	expect(forwardSubject('WG: Rechnung')).toBe('WG: Rechnung');
	expect(forwardSubject('Rechnung')).toBe('Fwd: Rechnung');
	expect(quote('a\n> b\n')).toBe('> a\n>> b');
	const msg = {
		from: { name: 'Anna', addr: 'anna@example.org' },
		to: [
			{ name: 'Ich', addr: 'me@example.org' },
			{ name: '', addr: 'ben@example.org' },
		],
		cc: [{ name: 'Anna', addr: 'ANNA@example.org' }],
		replyTo: [],
	};
	expect(replyRecipients(msg, 'me@example.org', false)).toEqual({
		to: ['Anna <anna@example.org>'],
		cc: [],
	});
	expect(replyRecipients(msg, 'me@example.org', true)).toEqual({
		to: ['Anna <anna@example.org>'],
		cc: ['ben@example.org'],
	});
	expect(validAddress('a@b.de')).toBe(true);
	expect(validAddress('a@b')).toBe(false);
	expect(linkify('siehe https://x.example/a. Danke')).toEqual([
		{ text: 'siehe ' },
		{ text: 'https://x.example/a', href: 'https://x.example/a' },
		{ text: '. Danke' },
	]);
});

test('levels in the address and the way up', () => {
	const url = (s: string) => new URL(s, 'http://nas.test');
	expect(levelOf(placeOf(url('/mail')))).toBe('folders');
	expect(levelOf(placeOf(url('/mail?a=1&f=INBOX')))).toBe('list');
	expect(levelOf(placeOf(url('/mail?a=1&f=INBOX&m=7')))).toBe('message');
	expect(mailUp(url('/mail?a=1&f=INBOX&m=7'))).toBe('/mail?a=1&f=INBOX');
	expect(mailUp(url('/mail?a=1&f=INBOX'))).toBe('/mail');
	expect(mailUp(url('/mail'))).toBe('/');
	expect(mailUp(url('/mail/compose?back=%2Fmail%3Fa%3D1'))).toBe('/mail?a=1');
	expect(mailUp(url('/mail/compose?back=https://x.test'))).toBe('/mail');
	expect(mailHref({ a: 'x', f: 'Arbeit/2026', m: 3 })).toBe('/mail?a=x&f=Arbeit%2F2026&m=3');
});

test('blocks of the live stream', () => {
	expect(parseBlock('event: mail\ndata: {"account":"a","folder":"INBOX","uid":9}')).toEqual({
		type: 'mail',
		account: 'a',
		folder: 'INBOX',
		uid: 9,
	});
	expect(
		parseBlock('event: status\ndata: {"account":"a","status":{"state":"auth_failed","code":"x"}}'),
	).toEqual({
		type: 'status',
		account: 'a',
		status: { state: 'auth_failed', code: 'x', since: '' },
	});
	expect(parseBlock('event: status\ndata: {"account":"a","state":"bogus"}')).toBeNull();
	expect(parseBlock(': keep-alive')).toBeNull();
	expect(parseBlock('event: flags\ndata: {"account":"a","folder":"F","uids":[1,2]}')).toEqual({
		type: 'flags',
		account: 'a',
		folder: 'F',
		uids: [1, 2],
	});
	expect([0, 1, 2, 10].map(pauseFor)).toEqual([1000, 2000, 4000, 30000]);
});

test('the draft saves every 30 s only after a change, never twice at once', async () => {
	vi.useFakeTimers();
	let release: () => void = () => {};
	const save = vi.fn(() => new Promise<void>((r) => (release = r)));
	const auto = createAutosave(save);
	await vi.advanceTimersByTimeAsync(30_000);
	expect(save).not.toHaveBeenCalled();
	auto.touch();
	await vi.advanceTimersByTimeAsync(29_000);
	expect(save).not.toHaveBeenCalled();
	await vi.advanceTimersByTimeAsync(1_000);
	expect(save).toHaveBeenCalledTimes(1);
	auto.touch();
	await vi.advanceTimersByTimeAsync(30_000);
	// The first save still runs: no second one meanwhile.
	expect(save).toHaveBeenCalledTimes(1);
	release();
	await vi.advanceTimersByTimeAsync(30_000);
	expect(save).toHaveBeenCalledTimes(2);
	auto.stop();
});

test('T-Mail shows only with the app release, and a compose window is never taken over', () => {
	const app = SYSTEM_APPS.find((a) => a.id === 'mail')!;
	for (const role of ['owner', 'admin', 'user'] as const) {
		expect(appAllowed(app, role, {})).toBe(false);
		expect(appAllowed(app, role, { mail: false })).toBe(false);
		expect(appAllowed(app, role, { mail: true })).toBe(true);
	}
	expect(taskPage('/mail/compose?a=1')).toBe(true);
	expect(taskPage('/mail/compose')).toBe(true);
	expect(taskPage('/mail')).toBe(false);
	expect(taskPage('/mail/composed')).toBe(false);
});

test('mail_busy waits as titand says and tries once more; then the error stands', async () => {
	const { busyOnce } = await import('#lib/tmail/api.ts');
	const { ApiError } = await import('#lib/api.ts');
	const busy = () => new ApiError(503, 'mail_busy', 'busy', 5);
	const waits: number[] = [];
	const wait = async (ms: number) => void waits.push(ms);
	let n = 0;
	await expect(
		busyOnce(async () => (n++ === 0 ? Promise.reject(busy()) : 'ok'), wait),
	).resolves.toBe('ok');
	expect(waits).toEqual([5000]);
	await expect(busyOnce(() => Promise.reject(busy()), wait)).rejects.toMatchObject({
		code: 'mail_busy',
	});
	await expect(
		busyOnce(() => Promise.reject(new ApiError(502, 'mail_unreachable', 'x')), wait),
	).rejects.toMatchObject({ code: 'mail_unreachable' });
	expect(waits).toEqual([5000, 5000]);
});
