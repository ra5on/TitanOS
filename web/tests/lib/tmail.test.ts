// 1.05, T-Mail: the plain parts – the frame's document (CSP first, links
// with their real target, nothing that could run), dates and addresses,
// the levels in the address, the live stream's blocks and the draft's turn.
import { afterEach, expect, test, vi } from 'vitest';
import { appAllowed, SYSTEM_APPS, taskPage } from '#lib/desktop/apps.ts';
import { createAutosave } from '#lib/tmail/autosave.ts';
import { cidsIn, MAIL_SANDBOX, mailCsp, prepareHtml } from '#lib/tmail/html.ts';
import { levelOf, mailHref, mailUp, placeOf } from '#lib/tmail/levels.ts';
import { parseBlock, pauseFor } from '#lib/tmail/live.svelte.ts';
import {
	forwardSubject,
	linkify,
	linkMismatch,
	listDate,
	providerHint,
	quote,
	replyRecipients,
	replySubject,
	validAddress,
} from '#lib/tmail/text.ts';

afterEach(() => vi.useRealTimers());

const warn = (host: string) => `WARN ${host}`;

test('the sandbox and the CSP are those of the contract', () => {
	expect(MAIL_SANDBOX).toBe('allow-popups allow-popups-to-escape-sandbox');
	expect(MAIL_SANDBOX).not.toMatch(/allow-scripts|allow-same-origin/);
	expect(mailCsp(false)).toBe(
		"default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src 'none'",
	);
	expect(mailCsp(true)).toContain('img-src data: https:');
});

test('the CSP meta is the first element of the document, before any content of the mail', () => {
	const { srcdoc } = prepareHtml(
		'<style>p{color:red}</style><p>Hallo</p><meta http-equiv="refresh" content="0;url=https://x.test">',
		{ images: false, warnText: warn },
	);
	const doc = new DOMParser().parseFromString(srcdoc, 'text/html');
	const first = doc.head.firstElementChild;
	expect(first?.tagName).toBe('META');
	expect(first?.getAttribute('http-equiv')).toBe('Content-Security-Policy');
	expect(first?.getAttribute('content')).toBe(mailCsp(false));
	// The mail's own <meta> is gone; its style stays after the CSP.
	expect(doc.querySelectorAll('meta[http-equiv="refresh"]').length).toBe(0);
	expect(srcdoc.indexOf('Content-Security-Policy')).toBeLessThan(srcdoc.indexOf('color:red'));
	expect(doc.body.textContent).toContain('Hallo');
});

test('scripts, event attributes and javascript: links never reach the frame', () => {
	const { srcdoc, links } = prepareHtml(
		'<script>parent.x=1</script><img src="x" onerror="alert(1)"><a href="javascript:alert(1)">a</a><iframe src="https://x.test"></iframe><form action="https://x.test"><input></form>',
		{ images: false, warnText: warn },
	);
	expect(srcdoc).not.toMatch(/<script|onerror|javascript:|<iframe|<form/i);
	expect(links).toEqual([]);
});

test('links open a new tab without referrer and show their real target', () => {
	const { srcdoc, links } = prepareHtml(
		'<a href="https://evil.example/login">www.sparkasse.de</a> <a href="https://shop.example/a">Hier</a>',
		{ images: false, warnText: warn },
	);
	const doc = new DOMParser().parseFromString(srcdoc, 'text/html');
	const [a, b] = [...doc.querySelectorAll('a')];
	expect(a.getAttribute('title')).toBe('https://evil.example/login');
	expect(a.getAttribute('target')).toBe('_blank');
	expect(a.getAttribute('rel')).toBe('noopener noreferrer');
	// The text names another domain: a visible warning after the link.
	expect(a.nextElementSibling?.textContent).toBe('WARN evil.example');
	expect(b.getAttribute('title')).toBe('https://shop.example/a');
	expect(links.map((l) => l.warn)).toEqual(['evil.example', '']);
});

test('embedded pictures come in as data: URLs or as a note', () => {
	const html = '<img src="cid:logo@x"><img src="cid:gone@x" alt="Plan">';
	expect(cidsIn(html)).toEqual(['logo@x', 'gone@x']);
	const { srcdoc } = prepareHtml(html, {
		images: false,
		cids: new Map([['logo@x', 'data:image/png;base64,AAAA']]),
		warnText: warn,
	});
	expect(srcdoc).toContain('src="data:image/png;base64,AAAA"');
	expect(srcdoc).not.toContain('cid:');
	expect(srcdoc).toContain('Plan');
});

test('link mismatch only when the text names another domain', () => {
	expect(linkMismatch('https://www.bank.de/x', 'bank.de')).toBe('');
	expect(linkMismatch('https://login.bank.de/x', 'www.bank.de')).toBe('');
	expect(linkMismatch('https://bank.de.evil.example', 'bank.de')).toBe('bank.de.evil.example');
	expect(linkMismatch('https://evil.example', 'Jetzt anmelden')).toBe('');
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
	expect(providerHint('max@gmail.com')).toBe('gmail');
	expect(providerHint('max@example.org')).toBe('');
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
