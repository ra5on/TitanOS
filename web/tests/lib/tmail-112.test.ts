// 1.12, T-Mail overhaul: the plain parts – date groups in the browser's
// zone with the language's week, conversations, keys, sender colours and
// what is kept per device.
import { afterEach, expect, test, vi } from 'vitest';
import { avatarColor, avatarInitials, AVATAR_COLORS } from '#lib/tmail/avatar.ts';
import { dayGroup, groupByDay, weekStart } from '#lib/tmail/groups.ts';
import { folderRef, isStrings, rangeOf, readPref, writePref } from '#lib/tmail/prefs.ts';
import { mailKey, typingIn } from '#lib/tmail/shortcuts.ts';
import { baseSubject, isReply, rowKey, threadRows, type Row } from '#lib/tmail/threads.ts';

afterEach(() => {
	vi.restoreAllMocks();
	localStorage.clear();
});

// Wednesday 18 Nov 2026, 10:00 local time.
const NOW = new Date(2026, 10, 18, 10, 0);
const at = (d: number, h = 9) => new Date(2026, 10, d, h, 0).toISOString();

test('the week starts on the language’s first day', () => {
	expect(weekStart('de')).toBe(1);
	expect(weekStart('de-DE')).toBe(1);
	expect(weekStart('en-US')).toBe(7);
	expect(weekStart('xx-invalid-!!')).toBe(1);
});

test('dates fall into today, yesterday, this week and older', () => {
	expect(dayGroup(at(18, 0), NOW, 'de')).toBe('today');
	expect(dayGroup(at(18, 23), NOW, 'de')).toBe('today'); // later today
	expect(dayGroup(at(25), NOW, 'de')).toBe('today'); // a wrong clock in the future
	expect(dayGroup(at(17, 23), NOW, 'de')).toBe('yesterday');
	expect(dayGroup(at(16, 0), NOW, 'de')).toBe('week'); // Monday
	expect(dayGroup(at(15, 12), NOW, 'de')).toBe('older'); // Sunday: the week before in German
	expect(dayGroup(at(15, 12), NOW, 'en-US')).toBe('week'); // … but this week in US English
	expect(dayGroup(at(14, 12), NOW, 'en-US')).toBe('older');
	expect(dayGroup('not a date', NOW, 'de')).toBe('older');
	// Monday morning: yesterday was Sunday, nothing else is "this week".
	const monday = new Date(2026, 10, 16, 8, 0);
	expect(dayGroup(at(15, 20), monday, 'de')).toBe('yesterday');
	expect(dayGroup(at(14, 20), monday, 'de')).toBe('older');
});

test('groups stand in their order and sort by date, whatever the uid order', () => {
	// A mail moved in (uid 9) is old; the list comes newest uid first.
	const items = [
		{ uid: 9, date: at(2) },
		{ uid: 8, date: at(18, 8) },
		{ uid: 7, date: at(17) },
		{ uid: 6, date: at(18, 9) },
		{ uid: 5, date: at(3) },
	];
	const groups = groupByDay(items, (i) => i.date, NOW, 'de');
	expect(groups.map((g) => [g.group, g.items.map((i) => i.uid)])).toEqual([
		['today', [6, 8]],
		['yesterday', [7]],
		['older', [5, 9]],
	]);
});

const row = (uid: number, over: Partial<Row> = {}): Row => ({
	uid,
	from: { name: '', addr: 'a@example.org' },
	to: [],
	subject: 'Frage',
	date: at(10 + uid),
	flags: { seen: true, flagged: false, answered: false },
	size: 1,
	attachments: false,
	...over,
});

test('conversations by Message-ID, In-Reply-To and References, siblings by a common earlier mail', () => {
	const rows = [
		row(5, { subject: 'Re: Frage', messageId: '<e@x>', inReplyTo: '<d@x>', references: '<a@x> <d@x>' }),
		row(4, { subject: 'Anderes', messageId: '<z@x>' }),
		// Answers the first mail, which is not in the list (an older page).
		row(3, { subject: 'AW: Frage', messageId: '<c@x>', references: '<a@x>' }),
		row(2, { subject: 'Re: Frage', messageId: '<d@x>', inReplyTo: '<A@X>' }),
	];
	const threads = threadRows(rows);
	expect(threads.map((t) => t.rows.map((r) => r.uid))).toEqual([[5, 3, 2], [4]]);
	expect(threads[0].key).toBe(rowKey(rows[0]));
	// Off: one entry each.
	expect(threadRows(rows, false).map((t) => t.rows.length)).toEqual([1, 1, 1, 1]);
});

test('the subject joins only an answer without heads, within 30 days', () => {
	const rows = [
		row(4, { subject: 'Ihr Paket kommt heute', date: at(17) }),
		row(3, { subject: 'Ihr Paket kommt heute', date: at(16) }),
		row(2, { subject: 'AW: Rechnung', date: at(15) }),
		row(1, { subject: 'Rechnung', date: at(14) }),
	];
	expect(threadRows(rows).map((t) => t.rows.map((r) => r.uid))).toEqual([[4], [3], [2, 1]]);
	const late = [
		row(2, { subject: 'Re: Rechnung', date: new Date(2026, 11, 30).toISOString() }),
		row(1, { subject: 'Rechnung', date: at(1) }),
	];
	expect(threadRows(late).map((t) => t.rows.length)).toEqual([1, 1]);
	// An answer with its own heads never joins by subject.
	const headed = [
		row(2, { subject: 'Re: Rechnung', inReplyTo: '<other@x>' }),
		row(1, { subject: 'Rechnung' }),
	];
	expect(threadRows(headed).map((t) => t.rows.length)).toEqual([1, 1]);
});

test('subjects lose their prefixes', () => {
	expect(baseSubject('Re: AW: Fwd: [Liste] WG:  Rechnung  März')).toBe('rechnung märz');
	expect(baseSubject('Re[2]: Hallo')).toBe('hallo');
	expect(isReply('AW: x')).toBe(true);
	expect(isReply('Rechnung: AW')).toBe(false);
});

test('search results with the same uid in two folders stay apart', () => {
	const rows = [row(1, { folder: 'INBOX' }), row(1, { folder: 'Archiv' })];
	expect(threadRows(rows).length).toBe(2);
});

const key = (k: string, over: Partial<KeyboardEvent> = {}, target: EventTarget | null = document.body) =>
	mailKey({
		key: k,
		ctrlKey: false,
		metaKey: false,
		altKey: false,
		defaultPrevented: false,
		target,
		...over,
	});

test('keys act in the list, never while typing or with a modifier', () => {
	expect(key('j')).toBe('next');
	expect(key('k')).toBe('prev');
	expect(key('e')).toBe('archive');
	expect(key('r')).toBe('reply');
	expect(key('Delete')).toBe('delete');
	expect(key('/')).toBe('search');
	expect(key('?')).toBe('help');
	expect(key('x')).toBeNull();
	expect(key('r', { ctrlKey: true })).toBeNull();
	expect(key('e', { metaKey: true })).toBeNull();
	expect(key('j', { altKey: true })).toBeNull();
	expect(key('j', { defaultPrevented: true })).toBeNull();
	const input = document.createElement('input');
	const area = document.createElement('textarea');
	const box = document.createElement('input');
	box.type = 'checkbox';
	const editable = document.createElement('div');
	editable.setAttribute('contenteditable', 'true');
	const inner = document.createElement('span');
	editable.append(inner);
	document.body.append(input, area, box, editable);
	expect(key('j', {}, input)).toBeNull();
	expect(key('Delete', {}, area)).toBeNull();
	expect(key('/', {}, inner)).toBeNull();
	expect(typingIn(box)).toBe(false);
	expect(key('j', {}, box)).toBe('next');
	document.body.replaceChildren();
});

test('a sender keeps one colour of the theme; initials from the name or the address', () => {
	const c = avatarColor('Anna@Example.org');
	expect(c).toBe(avatarColor(' anna@example.org '));
	expect(c).toMatch(/^var\(--icon-square-[a-z]+\)$/);
	const used = new Set(Array.from({ length: 200 }, (_, i) => avatarColor(`p${i}@x.org`)));
	expect(used.size).toBe(AVATAR_COLORS.length);
	expect(avatarInitials('Anna Beispiel', 'x@y')).toBe('AB');
	expect(avatarInitials('', 'noreply@paket.example')).toBe('N');
	expect(avatarInitials('', 'max.muster@firma.example')).toBe('MM');
	expect(avatarInitials('', '')).toBe('?');
});

test('per device: read with a check, defaults leave nothing behind, a refusing storage is no error', () => {
	writePref('favourites', [folderRef('a', 'INBOX')], []);
	expect(readPref('favourites', [], isStrings)).toEqual(['a\u0000INBOX']);
	writePref('favourites', [], []);
	expect(localStorage.getItem('titanos.mail.favourites')).toBeNull();
	localStorage.setItem('titanos.mail.favourites', '{"not":"a list"}');
	expect(readPref('favourites', ['x'], isStrings)).toEqual(['x']);
	localStorage.setItem('titanos.mail.favourites', 'not json');
	expect(readPref('favourites', [], isStrings)).toEqual([]);
	vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
		throw new Error('blocked');
	});
	vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
		throw new Error('blocked');
	});
	expect(readPref('favourites', ['d'], isStrings)).toEqual(['d']);
	expect(() => writePref('favourites', ['y'], [])).not.toThrow();
});

test('Shift ranges follow the shown order', () => {
	const order = ['a', 'b', 'c', 'd'];
	expect(rangeOf(order, 'b', 'd')).toEqual(['b', 'c', 'd']);
	expect(rangeOf(order, 'd', 'b')).toEqual(['b', 'c', 'd']);
	expect(rangeOf(order, 'x', 'c')).toEqual(['c']);
	expect(rangeOf(order, 'a', 'x')).toEqual([]);
});
