// 1.11 (step 16): what the page of Kontakte reckons with, without Svelte or
// the DOM (unit tests in tests/contacts.test.mjs). The shapes are those of
// the contacts API (Entwuerfe/108 BAU-API "Schritt 14b"): the list has
// `Item`s, one contact is a `Contact`, changes go as `Input` – and only the
// fields the form changed, since titand keeps everything of a card the page
// does not show (unknown vCard lines, labels, parameters) and a field that
// is sent counts as changed.

export const PHONE_TYPES = ['mobile', 'home', 'work', 'other'] as const;
export const MAIL_TYPES = ['home', 'work', 'other'] as const;
export const ADDRESS_TYPES = MAIL_TYPES;

export type Value = { type: string; value: string; label?: string };
export type Address = {
	type: string;
	label?: string;
	street: string;
	city: string;
	postcode: string;
	region: string;
	country: string;
};
export type Contact = {
	id: string;
	book: string;
	etag: string;
	first: string;
	last: string;
	name: string;
	org: string;
	phones: Value[];
	emails: Value[];
	addresses: Address[];
	/** 'YYYY-MM-DD', '--MM-DD' (without year) or '' (none or one TitanOS does not read). */
	birthday: string;
	notes: string;
	hasPhoto: boolean;
	url: string;
	modified: string;
};
/** A birthday in this week (Monday to Sunday in the NAS zone, past days included). */
export type Soon = { date: string; age?: number };
export type Item = {
	id: string;
	book: string;
	name: string;
	sortKey: string;
	org: string;
	initials: string;
	email: string;
	hasPhoto: boolean;
	/** The card's etag (1.13): the picture's address names it, the browser keeps it for good. */
	etag: string;
	birthdaySoon: Soon | null;
};
export type Suggestion = { name: string; addr: string; count: number };

const rec = (v: unknown): Record<string, unknown> =>
	v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const DATE = /^\d{4}-\d{2}-\d{2}$/;
const NO_YEAR = /^--\d{2}-\d{2}$/;

function soonOf(v: unknown): Soon | null {
	const s = rec(v);
	if (!DATE.test(str(s.date))) return null;
	// 115-7: a card born this week has age 0 ("ist am Mittwoch 0 geworden"): no age then.
	return typeof s.age === 'number' && s.age >= 1
		? { date: str(s.date), age: Math.floor(s.age) }
		: { date: str(s.date) };
}

/** GET /contacts: the list, checked; null for anything that is not a list. */
export function sanitizeItems(raw: unknown): Item[] | null {
	if (!Array.isArray(raw)) return null;
	return raw
		.map(rec)
		.filter((i) => str(i.id) !== '')
		.map((i) => ({
			id: str(i.id),
			book: str(i.book) || 'kontakte',
			name: str(i.name),
			sortKey: str(i.sortKey),
			org: str(i.org),
			initials: str(i.initials),
			email: str(i.email),
			hasPhoto: i.hasPhoto === true,
			etag: str(i.etag),
			birthdaySoon: soonOf(i.birthdaySoon),
		}));
}

/** One row of a write's answer (`item`), checked; null without one. */
export function sanitizeItem(raw: unknown): Item | null {
	return raw && typeof raw === 'object' ? (sanitizeItems([raw])?.[0] ?? null) : null;
}

// ---- 1.13: address books, groups, duplicates (GET /contacts/overview) ----

export type Book = { id: string; name: string; count: number };
/** A group (iOS's group vCard): its members as contact ids; `other` members TitanOS cannot show. */
export type Group = {
	id: string;
	book: string;
	etag: string;
	name: string;
	members: string[];
	other: number;
};
export type Overview = { books: Book[]; groups: Group[]; duplicates: number };
export type DuplicateMatch = { kind: 'email' | 'phone' | 'name'; value: string; ids: string[] };
export type DuplicateSet = { book: string; ids: string[]; matches: DuplicateMatch[] };

const strings = (v: unknown): string[] =>
	Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string' && x !== '') : [];
const count = (v: unknown): number => (typeof v === 'number' && v > 0 ? Math.floor(v) : 0);

export function sanitizeGroup(raw: unknown): Group | null {
	const g = rec(raw);
	if (!str(g.id)) return null;
	return {
		id: str(g.id),
		book: str(g.book) || 'kontakte',
		etag: str(g.etag),
		name: str(g.name),
		members: strings(g.members),
		other: count(g.other),
	};
}

/** GET /contacts/overview, checked; null for anything else. */
export function sanitizeOverview(raw: unknown): Overview | null {
	const o = rec(raw);
	if (!Array.isArray(o.books) || !Array.isArray(o.groups)) return null;
	return {
		books: o.books
			.map(rec)
			.filter((b) => str(b.id) !== '')
			.map((b) => ({ id: str(b.id), name: str(b.name), count: count(b.count) })),
		groups: o.groups.map(sanitizeGroup).filter((g): g is Group => g !== null),
		duplicates: count(o.duplicates),
	};
}

/** GET /contacts/duplicates, checked. */
export function sanitizeDuplicates(raw: unknown): DuplicateSet[] | null {
	if (!Array.isArray(raw)) return null;
	return raw
		.map(rec)
		.map((s) => ({
			book: str(s.book) || 'kontakte',
			ids: strings(s.ids),
			matches: (Array.isArray(s.matches) ? s.matches : [])
				.map(rec)
				.filter((m) => m.kind === 'email' || m.kind === 'phone' || m.kind === 'name')
				.map((m) => ({
					kind: m.kind as DuplicateMatch['kind'],
					value: str(m.value),
					ids: strings(m.ids),
				})),
		}))
		.filter((s) => s.ids.length >= 2);
}

/** The groups a contact is in, by name. */
export function groupsOf(groups: Group[], id: string): Group[] {
	return groups.filter((g) => g.members.includes(id));
}

/** The list with a row put in its place (by the sort key, then name and id), or replaced. */
export function upsertItem(list: Item[], item: Item): Item[] {
	const rest = list.filter((i) => i.id !== item.id);
	const key = (i: Item) => [fold(i.sortKey), fold(i.name), i.id] as const;
	const k = key(item);
	const at = rest.findIndex((i) => {
		const o = key(i);
		return o[0] > k[0] || (o[0] === k[0] && (o[1] > k[1] || (o[1] === k[1] && o[2] > k[2])));
	});
	return at < 0 ? [...rest, item] : [...rest.slice(0, at), item, ...rest.slice(at)];
}

function valuesOf(v: unknown): Value[] {
	if (!Array.isArray(v)) return [];
	return v.map(rec).map((x) => {
		const out: Value = { type: str(x.type) || 'other', value: str(x.value) };
		if (str(x.label)) out.label = str(x.label);
		return out;
	});
}
function addressesOf(v: unknown): Address[] {
	if (!Array.isArray(v)) return [];
	return v.map(rec).map((x) => {
		const out: Address = {
			type: str(x.type) || 'other',
			street: str(x.street),
			city: str(x.city),
			postcode: str(x.postcode),
			region: str(x.region),
			country: str(x.country),
		};
		if (str(x.label)) out.label = str(x.label);
		return out;
	});
}

/** GET /contacts/{book}/{id}: one contact, checked; null without an id. */
export function sanitizeContact(raw: unknown): Contact | null {
	const c = rec(raw);
	if (!str(c.id)) return null;
	const birthday = str(c.birthday);
	return {
		id: str(c.id),
		book: str(c.book) || 'kontakte',
		etag: str(c.etag),
		first: str(c.first),
		last: str(c.last),
		name: str(c.name),
		org: str(c.org),
		phones: valuesOf(c.phones),
		emails: valuesOf(c.emails),
		addresses: addressesOf(c.addresses),
		birthday: DATE.test(birthday) || NO_YEAR.test(birthday) ? birthday : '',
		notes: str(c.notes),
		hasPhoto: c.hasPhoto === true,
		url: /^https?:\/\//i.test(str(c.url)) ? str(c.url) : '',
		modified: str(c.modified),
	};
}

/** GET /contacts/suggestions: learned addresses of T-Mail. */
export function sanitizeSuggestions(raw: unknown): Suggestion[] | null {
	if (!Array.isArray(raw)) return null;
	return raw
		.map(rec)
		.filter((s) => str(s.addr) !== '')
		.map((s) => ({
			name: str(s.name),
			addr: str(s.addr),
			count: typeof s.count === 'number' && s.count > 0 ? Math.floor(s.count) : 0,
		}));
}

// ---- Folding, letters, initials, marks ----

/** Folds like titand's cardweb.Fold (DIN 5007-1): lower case, accents gone, ß → ss. */
export function fold(s: string): string {
	return s.toLowerCase().normalize('NFD').replace(/\p{M}/gu, '').replace(/ß/g, 'ss');
}

/** The letter a contact stands under: A–Z after folding (Ä under A), anything else '#'. */
export function letterOf(item: Pick<Item, 'sortKey' | 'name'>): string {
	const first = fold((item.sortKey || item.name).trim()).charAt(0);
	return /[a-z]/.test(first) ? first.toUpperCase() : '#';
}

/**
 * The list in groups by letter: exactly one group per letter, A–Z, then
 * "#" once and last (like iOS). Inside a letter the order titand sorted
 * (115-7: titand sorts byte-wise, so names without a letter – digits,
 * "1&1 Hotline", an empty key – come before A and folded letters outside
 * a–z – Ø, Ł, Cyrillic, Chinese – after Z; one run per letter gave "#"
 * twice and the list's keyed each failed).
 */
export function groupByLetter<T extends Pick<Item, 'sortKey' | 'name'>>(
	items: T[],
): { letter: string; items: T[] }[] {
	const by = new Map<string, T[]>();
	for (const item of items) {
		const letter = letterOf(item);
		const list = by.get(letter);
		if (list) list.push(item);
		else by.set(letter, [item]);
	}
	return [...by.keys()]
		.sort((a, b) => (a === '#' ? 1 : b === '#' ? -1 : a < b ? -1 : a > b ? 1 : 0))
		.map((letter) => ({ letter, items: by.get(letter) ?? [] }));
}

/** The letters of the register at the list's edge: A–Z and "#". */
export const INDEX_LETTERS = [...'ABCDEFGHIJKLMNOPQRSTUVWXYZ', '#'];

/**
 * The name of a row in three parts, the family name in the middle (shown
 * bold like Apple's Kontakte). titand's sort key is "family given" (lower
 * case) or the FN: a split of the name's words whose folded "last first"
 * is the key names the family part; a key equal to the whole name (a
 * company, a card without N) makes it all bold; anything else stays plain.
 */
export function nameParts(name: string, sortKey: string): [string, string, string] {
	const key = fold(sortKey.trim()).replace(/\s+/g, ' ');
	const words = name.trim().split(/\s+/).filter(Boolean);
	if (!key || !words.length) return [name, '', ''];
	if (fold(words.join(' ')) === key) return ['', name.trim(), ''];
	// The family name is the run of words at the end ("Anna Maria Beispiel", "Ute von Berg").
	for (let cut = 1; cut < words.length; cut++) {
		const first = words.slice(0, cut).join(' ');
		const last = words.slice(cut).join(' ');
		if (fold(`${last} ${first}`) === key) return [first + ' ', last, ''];
	}
	return [name, '', ''];
}

/**
 * Initials when titand sent none, like its cardweb.Initials: first letters
 * of the first and last word ("Oma Hilde" → "OH"), one for a company.
 */
export function initialsOf(name: string, org = ''): string {
	const words = name
		.split(/[\s,]+/)
		.map((w) => w.replace(/^[^\p{L}\p{N}]+/u, ''))
		.filter(Boolean);
	if (!words.length) return '?';
	const first = [...words[0]][0] ?? '';
	const one = words.length === 1 || name.trim() === org.trim();
	const last = one ? '' : ([...words[words.length - 1]][0] ?? '');
	return (first + last).toLocaleUpperCase('de');
}

/**
 * A text in pieces with the search marked, found the way titand searches
 * (folded: "muller" marks "Müller", "strasse" marks "Straße").
 */
export function markParts(text: string, query: string): [string, boolean][] {
	const q = fold(query.trim());
	if (!q || !text) return [[text, false]];
	// The folded text with, for every folded character, where it came from.
	let folded = '';
	const from: number[] = [];
	const to: number[] = [];
	let i = 0;
	for (const ch of text) {
		const f = fold(ch);
		for (const c of f) {
			folded += c;
			from.push(i);
			to.push(i + ch.length);
		}
		i += ch.length;
	}
	const ranges: [number, number][] = [];
	for (let at = folded.indexOf(q); at >= 0; at = folded.indexOf(q, at + q.length)) {
		const start = from[at];
		const end = to[at + q.length - 1];
		const last = ranges.at(-1);
		if (last && start <= last[1]) last[1] = Math.max(last[1], end);
		else ranges.push([start, end]);
	}
	if (!ranges.length) return [[text, false]];
	const out: [string, boolean][] = [];
	let pos = 0;
	for (const [s, e] of ranges) {
		if (s > pos) out.push([text.slice(pos, s), false]);
		out.push([text.slice(s, e), true]);
		pos = e;
	}
	if (pos < text.length) out.push([text.slice(pos), false]);
	return out;
}

/** The second line of a row: the company (if the name is not it), else the e-mail address. */
export function subline(item: Pick<Item, 'name' | 'org' | 'email'>): string {
	if (item.org && item.org !== item.name) return item.org;
	return item.email && item.email !== item.name ? item.email : '';
}

// ---- Links ----

/** A tel: link of a number as people write it ("+49 151 7654321" → "tel:+491517654321"); '' without digits. */
export function telHref(value: string): string {
	const v = value.trim();
	const body = v.replace(/[^\d*#]/g, '');
	if (!/\d/.test(body)) return '';
	return 'tel:' + (v.startsWith('+') ? '+' : '') + body;
}

/** A link of the card's web address: http(s) only, else ''. */
export function webHref(value: string): string {
	try {
		const u = new URL(value.trim());
		return u.protocol === 'http:' || u.protocol === 'https:' ? u.href : '';
	} catch {
		return '';
	}
}

/** The lines of an address as written on a letter. */
/** A text message to a number (sms:), '' without digits. */
export function smsHref(value: string): string {
	const tel = telHref(value);
	return tel && !/[*#]/.test(tel) ? 'sms:' + tel.slice(4) : '';
}

/**
 * A map of an address (1.13, the contact's "Karte"): Apple Maps on Apple
 * devices (it opens the Maps app), OpenStreetMap elsewhere. Only on a tap,
 * the address leaves the NAS then.
 */
export function mapHref(a: Omit<Address, 'type' | 'label'>, apple: boolean): string {
	const q = addressLines(a).join(', ');
	if (!q) return '';
	return apple
		? `https://maps.apple.com/?q=${encodeURIComponent(q)}`
		: `https://www.openstreetmap.org/search?query=${encodeURIComponent(q)}`;
}

export function addressLines(a: Omit<Address, 'type' | 'label'>): string[] {
	return [
		...a.street.split('\n'),
		[a.postcode, a.city].filter((x) => x.trim()).join(' '),
		a.region,
		a.country,
	]
		.map((l) => l.trim())
		.filter(Boolean);
}

// ---- 1.15: a contact shared as a card ("Teilen") ----

const vEsc = (v: string) =>
	v
		.replace(/\\/g, '\\\\')
		.replace(/\r?\n/g, '\\n')
		.replace(/([,;])/g, '\\$1');
const TEL_TYPE: Record<string, string> = { mobile: 'CELL', home: 'HOME', work: 'WORK' };
const KIND_TYPE: Record<string, string> = { home: 'HOME', work: 'WORK' };

/**
 * The shown fields of a contact as a vCard 3.0 (CRLF lines) for "Teilen"
 * (115-6): name, company, numbers, e-mail and postal addresses (each with
 * its kind, an own label of the card as X-ABLabel like the iPhone),
 * birthday (without year as the iPhone writes it), web address, note.
 * The picture and lines TitanOS does not show stay out.
 */
export function vcardOf(c: Contact): string {
	const lines = ['BEGIN:VCARD', 'VERSION:3.0'];
	let item = 0;
	const labelled = (line: string, label?: string) => {
		if (!label) return lines.push(line);
		item++;
		lines.push(`item${item}.${line}`, `item${item}.X-ABLabel:${vEsc(label)}`);
	};
	lines.push(`N:${vEsc(c.last)};${vEsc(c.first)};;;`);
	lines.push(`FN:${vEsc(c.name || [c.first, c.last].filter(Boolean).join(' ') || c.org)}`);
	if (c.org) lines.push(`ORG:${vEsc(c.org)}`);
	for (const p of c.phones) {
		if (!p.value.trim()) continue;
		const type = TEL_TYPE[p.type] ?? 'VOICE';
		labelled(`TEL;TYPE=${type}:${vEsc(p.value.trim())}`, p.label);
	}
	for (const e of c.emails) {
		if (!e.value.trim()) continue;
		const type = KIND_TYPE[e.type] ? `INTERNET,${KIND_TYPE[e.type]}` : 'INTERNET';
		labelled(`EMAIL;TYPE=${type}:${vEsc(e.value.trim())}`, e.label);
	}
	for (const a of c.addresses) {
		const parts = [a.street, a.city, a.region, a.postcode, a.country].map((x) => vEsc(x.trim()));
		if (!parts.some(Boolean)) continue;
		const type = KIND_TYPE[a.type] ? `;TYPE=${KIND_TYPE[a.type]}` : '';
		labelled(`ADR${type}:;;${parts.join(';')}`, a.label);
	}
	if (/^\d{4}-\d{2}-\d{2}$/.test(c.birthday)) lines.push(`BDAY:${c.birthday}`);
	else if (/^--\d{2}-\d{2}$/.test(c.birthday))
		lines.push(`BDAY;X-APPLE-OMIT-YEAR=1604:1604${c.birthday.slice(1)}`);
	if (c.url) lines.push(`URL:${vEsc(c.url)}`);
	if (c.notes.trim()) lines.push(`NOTE:${vEsc(c.notes.trim())}`);
	lines.push('END:VCARD');
	return lines.join('\r\n') + '\r\n';
}

/** The file name of a shared card: the name without characters file systems refuse. */
export function vcardName(c: Pick<Contact, 'name' | 'org'>): string {
	const base = (c.name || c.org || 'Kontakt')
		.replace(/[\\/:*?"<>|\u0000-\u001f]/g, ' ')
		.replace(/\s+/g, ' ')
		.trim()
		.slice(0, 80);
	return (base || 'Kontakt') + '.vcf';
}

// ---- Birthdays ----

export type Birthday = { year: number | null; month: number; day: number };

/** '1986-11-20' or '--11-20' (without year); null for anything else. */
export function parseBirthday(value: string): Birthday | null {
	let m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
	if (m) return { year: Number(m[1]), month: Number(m[2]), day: Number(m[3]) };
	m = /^--(\d{2})-(\d{2})$/.exec(value);
	if (m) return { year: null, month: Number(m[1]), day: Number(m[2]) };
	return null;
}

/**
 * When a birthday of this week falls, seen from today: 'today', 'coming'
 * (later this week) or 'past' (earlier this week), with the weekday of the
 * date (0 = Monday).
 */
export function soonWhen(
	soon: Soon,
	today: string,
): { when: 'today' | 'coming' | 'past'; weekday: number } {
	const weekday = (new Date(`${soon.date}T12:00:00Z`).getUTCDay() + 6) % 7;
	const when = soon.date === today ? 'today' : soon.date > today ? 'coming' : 'past';
	return { when, weekday };
}

// ---- The form ----

export type Row = { type: string; value: string; label?: string };
export type AddressRow = {
	type: string;
	label?: string;
	street: string;
	postcode: string;
	city: string;
	/** Not in the form of the drafts, but kept: sending an address without it would clear it. */
	region: string;
	country: string;
};
export type Form = {
	first: string;
	last: string;
	org: string;
	phones: Row[];
	emails: Row[];
	addresses: AddressRow[];
	/** The value of the date field: YYYY-MM-DD; without year the year is 2000 (so 29 February works). */
	birthdayDate: string;
	birthdayNoYear: boolean;
	notes: string;
};

export function blankForm(): Form {
	return {
		first: '',
		last: '',
		org: '',
		phones: [],
		emails: [],
		addresses: [],
		birthdayDate: '',
		birthdayNoYear: false,
		notes: '',
	};
}

/** The form of a contact. */
export function formOf(c: Contact): Form {
	const b = parseBirthday(c.birthday);
	return {
		first: c.first,
		last: c.last,
		org: c.org,
		phones: c.phones.map((p) => ({ ...p })),
		emails: c.emails.map((e) => ({ ...e })),
		addresses: c.addresses.map((a) => ({
			type: a.type,
			...(a.label ? { label: a.label } : {}),
			street: a.street,
			postcode: a.postcode,
			city: a.city,
			region: a.region,
			country: a.country,
		})),
		birthdayDate: b ? c.birthday.replace(/^--/, '2000-') : '',
		birthdayNoYear: !!b && b.year === null,
		notes: c.notes,
	};
}

/** The birthday the form says: 'YYYY-MM-DD', '--MM-DD' or ''. */
export function birthdayOf(form: Pick<Form, 'birthdayDate' | 'birthdayNoYear'>): string {
	if (!DATE.test(form.birthdayDate)) return '';
	return form.birthdayNoYear ? '--' + form.birthdayDate.slice(5) : form.birthdayDate;
}

export type Input = {
	first?: string;
	last?: string;
	org?: string;
	phones?: { type: string; value: string }[];
	emails?: { type: string; value: string }[];
	addresses?: {
		type: string;
		street: string;
		city: string;
		postcode: string;
		region: string;
		country: string;
	}[];
	birthday?: string;
	notes?: string;
};

const values = (rows: Row[]) =>
	rows.map((r) => ({ type: r.type, value: r.value.trim() })).filter((r) => r.value !== '');
const addresses = (rows: AddressRow[]) =>
	rows
		.map((r) => ({
			type: r.type,
			street: r.street
				.split('\n')
				.map((l) => l.trim())
				.filter(Boolean)
				.join('\n'),
			city: r.city.trim(),
			postcode: r.postcode.trim(),
			region: r.region.trim(),
			country: r.country.trim(),
		}))
		.filter((r) => r.street || r.city || r.postcode || r.region || r.country);
const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/**
 * What a save sends: for a new contact (`before` null) every filled field,
 * for a changed one only the fields that differ from `before` – lists as a
 * whole, never a label, the birthday only when it was changed (titand
 * answers '' also for one it cannot read, which must stay).
 */
export function inputOf(form: Form, before: Form | null): Input {
	const out: Input = {};
	const text = (k: 'first' | 'last' | 'org' | 'notes') => {
		const now = k === 'notes' ? form.notes.replace(/\s+$/, '') : form[k].trim();
		const was = before ? (k === 'notes' ? before.notes.replace(/\s+$/, '') : before[k].trim()) : '';
		if (before ? now !== was : now !== '') out[k] = now;
	};
	text('first');
	text('last');
	text('org');
	const phones = values(form.phones);
	if (before ? !same(phones, values(before.phones)) : phones.length) out.phones = phones;
	const emails = values(form.emails);
	if (before ? !same(emails, values(before.emails)) : emails.length) out.emails = emails;
	const adr = addresses(form.addresses);
	if (before ? !same(adr, addresses(before.addresses)) : adr.length) out.addresses = adr;
	const birthday = birthdayOf(form);
	if (before ? birthday !== birthdayOf(before) : birthday) out.birthday = birthday;
	text('notes');
	return out;
}

/** Whether the form names the contact at all (a name, company, e-mail or number; titand refuses one without). */
export function namesSomeone(form: Form): boolean {
	return !!(
		form.first.trim() ||
		form.last.trim() ||
		form.org.trim() ||
		values(form.emails).length ||
		values(form.phones).length
	);
}

/** What a save does with the picture (applied after the card is saved). */
export type PhotoChoice =
	| { kind: 'keep' }
	| { kind: 'file'; file: File; preview: string }
	| { kind: 'nas'; root: string; path: string; preview: string }
	| { kind: 'remove' };

/** A file of the NAS the picture may come from (titand reads JPEG, PNG, WebP). */
export function pictureName(name: string): boolean {
	return /\.(jpe?g|png|webp)$/i.test(name);
}
