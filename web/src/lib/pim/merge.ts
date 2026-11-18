// 1.13 (owner 18 Nov 2026): merging two contacts – what the preview offers
// field by field, without Svelte or the DOM (tests/contacts-merge.test.ts).
// Single values (name, company, birthday, note, picture) are chosen when
// the two differ; numbers, e-mail and postal addresses are joined (each
// can be left out). titand keeps everything else of both cards
// (cardweb.Merge).
import { addressLines, type Address, type Contact, type Input, type Value } from './contacts.ts';

export type Choice<T> = { a: T; b: T; differ: boolean };

const digits = (s: string) => s.replace(/[^\d+]/g, '').replace(/^00/, '+');
const keyOf = {
	phone: (v: Value) => digits(v.value),
	email: (v: Value) => v.value.trim().toLowerCase(),
	address: (a: Address) => addressLines(a).join('\n').toLowerCase(),
};

/** The values of both, each once (the first one's first), with where they came from. */
export function joinValues<T>(
	a: T[],
	b: T[],
	key: (v: T) => string,
): { value: T; from: 'a' | 'b' | 'both' }[] {
	const out: { value: T; from: 'a' | 'b' | 'both' }[] = [];
	for (const v of a) {
		const k = key(v);
		if (!k || out.some((o) => key(o.value) === k)) continue;
		out.push({ value: v, from: b.some((x) => key(x) === k) ? 'both' : 'a' });
	}
	for (const v of b) {
		const k = key(v);
		if (!k || out.some((o) => key(o.value) === k)) continue;
		out.push({ value: v, from: 'b' });
	}
	return out;
}

export type MergePlan = {
	name: Choice<{ first: string; last: string }>;
	org: Choice<string>;
	birthday: Choice<string>;
	notes: Choice<string>;
	phones: { value: Value; from: 'a' | 'b' | 'both' }[];
	emails: { value: Value; from: 'a' | 'b' | 'both' }[];
	addresses: { value: Address; from: 'a' | 'b' | 'both' }[];
	/** Both have a picture: chosen; else the one there is. */
	photo: { a: boolean; b: boolean };
};

const choice = <T>(
	a: T,
	b: T,
	empty: (v: T) => boolean,
	same: (x: T, y: T) => boolean,
): Choice<T> => ({
	a,
	b,
	differ: !empty(a) && !empty(b) && !same(a, b),
});

/** What the preview shows for two contacts (a is kept). */
export function planOf(a: Contact, b: Contact): MergePlan {
	const text = (s: string) => s.trim() === '';
	const eq = (x: string, y: string) => x.trim() === y.trim();
	return {
		name: choice(
			{ first: a.first, last: a.last },
			{ first: b.first, last: b.last },
			(n) => !n.first.trim() && !n.last.trim(),
			(x, y) => eq(x.first, y.first) && eq(x.last, y.last),
		),
		org: choice(a.org, b.org, text, eq),
		birthday: choice(a.birthday, b.birthday, text, eq),
		notes: choice(a.notes, b.notes, text, eq),
		phones: joinValues(a.phones, b.phones, keyOf.phone),
		emails: joinValues(a.emails, b.emails, keyOf.email),
		addresses: joinValues(a.addresses, b.addresses, keyOf.address),
		photo: { a: a.hasPhoto, b: b.hasPhoto },
	};
}

export type MergePick = {
	name: 'a' | 'b';
	org: 'a' | 'b';
	birthday: 'a' | 'b';
	notes: 'a' | 'b' | 'both';
	photo: 'a' | 'b';
	/** Indexes into the plan's lists that are left out. */
	skipPhones: number[];
	skipEmails: number[];
	skipAddresses: number[];
};

export function defaultPick(): MergePick {
	return {
		name: 'a',
		org: 'a',
		birthday: 'a',
		notes: 'a',
		photo: 'a',
		skipPhones: [],
		skipEmails: [],
		skipAddresses: [],
	};
}

/** The one of a choice: the picked one, or the other when the picked one is empty. */
function pickOf<T>(c: Choice<T>, side: 'a' | 'b', empty: (v: T) => boolean): T {
	const first = side === 'a' ? c.a : c.b;
	const other = side === 'a' ? c.b : c.a;
	return empty(first) ? other : first;
}

/** The fields titand gets (all of them) and what happens to the picture. */
export function mergeInput(
	plan: MergePlan,
	pick: MergePick,
): { input: Input; photo: 'keep' | 'drop' } {
	const text = (s: string) => s.trim() === '';
	const name = pickOf(plan.name, pick.name, (n) => !n.first.trim() && !n.last.trim());
	let notes =
		pick.notes === 'both' && plan.notes.differ
			? `${plan.notes.a.replace(/\s+$/, '')}\n\n${plan.notes.b.trim()}`
			: pickOf(plan.notes, pick.notes === 'both' ? 'a' : pick.notes, text);
	notes = notes.replace(/\s+$/, '');
	const keep = <T>(list: { value: T }[], skip: number[]) =>
		list.filter((_, i) => !skip.includes(i)).map((x) => x.value);
	const input: Input = {
		first: name.first.trim(),
		last: name.last.trim(),
		org: pickOf(plan.org, pick.org, text).trim(),
		phones: keep(plan.phones, pick.skipPhones).map((v) => ({
			type: v.type,
			value: v.value.trim(),
		})),
		emails: keep(plan.emails, pick.skipEmails).map((v) => ({
			type: v.type,
			value: v.value.trim(),
		})),
		addresses: keep(plan.addresses, pick.skipAddresses).map((a) => ({
			type: a.type,
			street: a.street,
			city: a.city,
			postcode: a.postcode,
			region: a.region,
			country: a.country,
		})),
		birthday: pickOf(plan.birthday, pick.birthday, text),
		notes,
	};
	const photo =
		plan.photo.a && plan.photo.b
			? pick.photo === 'b'
				? 'drop'
				: 'keep'
			: plan.photo.b
				? 'drop'
				: 'keep';
	return { input, photo };
}
