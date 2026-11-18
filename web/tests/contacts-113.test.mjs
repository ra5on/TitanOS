// 1.13 (owner 18 Nov 2026): Kontakte's levels in the address, the side
// bar's overview, the rows put in place after a write and the merge plan.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	contactLevelOf,
	contactPlaceOf,
	contactsHref,
	contactsUp,
} from '../src/lib/pim/contact-levels.ts';
import {
	groupsOf,
	mapHref,
	sanitizeDuplicates,
	sanitizeItem,
	sanitizeOverview,
	smsHref,
	upsertItem,
} from '../src/lib/pim/contacts.ts';
import { defaultPick, joinValues, mergeInput, planOf } from '../src/lib/pim/merge.ts';

const url = (s) => new URL('http://x' + s);

test('levels: side bar, list, contact, form; one level up each', () => {
	assert.equal(contactLevelOf(contactPlaceOf(url('/contacts'))), 'list');
	assert.equal(contactLevelOf(contactPlaceOf(url('/contacts?side=1'))), 'side');
	assert.equal(contactLevelOf(contactPlaceOf(url('/contacts?g=f.vcf&id=a.vcf'))), 'content');
	assert.equal(
		contactLevelOf(contactPlaceOf(url('/contacts?view=duplicates&set=a.vcf'))),
		'content',
	);
	assert.equal(contactsUp(url('/contacts?g=f.vcf&id=a.vcf&edit=1')), '/contacts?g=f.vcf&id=a.vcf');
	assert.equal(contactsUp(url('/contacts?g=f.vcf&id=a.vcf')), '/contacts?g=f.vcf');
	assert.equal(contactsUp(url('/contacts?g=f.vcf')), '/contacts?side=1&g=f.vcf');
	assert.equal(contactsUp(url('/contacts?side=1')), '/');
	assert.equal(contactsUp(url('/contacts/iphone')), '/contacts');
	// A group only with the book's list, a set only with the duplicates.
	assert.equal(contactPlaceOf(url('/contacts?view=suggestions&g=f.vcf')).g, '');
	assert.equal(contactPlaceOf(url('/contacts?set=a.vcf')).set, '');
	assert.equal(
		contactsHref({ view: 'contacts', g: 'Oma Hilde+1.vcf', id: 'x y.vcf', edit: true }),
		'/contacts?g=Oma+Hilde%2B1.vcf&id=x+y.vcf&edit=1',
	);
	assert.equal(contactsHref({ isNew: true, g: 'f.vcf' }), '/contacts?g=f.vcf&new=1');
});

test('the overview and duplicates are checked', () => {
	assert.equal(sanitizeOverview({}), null);
	const o = sanitizeOverview({
		books: [{ id: 'kontakte', name: 'Kontakte', count: 3 }, { id: '' }],
		groups: [{ id: 'g.vcf', name: 'Familie', members: ['a.vcf', 7, ''], other: 2 }, { name: 'x' }],
		duplicates: 1.7,
	});
	assert.deepEqual(o, {
		books: [{ id: 'kontakte', name: 'Kontakte', count: 3 }],
		groups: [
			{ id: 'g.vcf', book: 'kontakte', etag: '', name: 'Familie', members: ['a.vcf'], other: 2 },
		],
		duplicates: 1,
	});
	assert.deepEqual(
		groupsOf(o.groups, 'a.vcf').map((g) => g.id),
		['g.vcf'],
	);
	const d = sanitizeDuplicates([
		{
			ids: ['a', 'b'],
			matches: [{ kind: 'email', value: 'x@y', ids: ['a', 'b'] }, { kind: 'evil' }],
		},
		{ ids: ['only'] },
	]);
	assert.equal(d.length, 1);
	assert.deepEqual(
		d[0].matches.map((m) => m.kind),
		['email'],
	);
});

test('a written row goes to its place by the sort key', () => {
	const row = (id, sortKey, name = sortKey) => sanitizeItem({ id, sortKey, name });
	let list = [row('a', 'adler'), row('m', 'mueller'), row('z', 'zander')];
	list = upsertItem(list, row('n', 'neu'));
	assert.deepEqual(
		list.map((i) => i.id),
		['a', 'm', 'n', 'z'],
	);
	// A renamed contact moves.
	list = upsertItem(list, row('a', 'zzz'));
	assert.deepEqual(
		list.map((i) => i.id),
		['m', 'n', 'z', 'a'],
	);
	assert.equal(sanitizeItem(null), null);
	assert.equal(sanitizeItem({ id: 'x', etag: 'e1' }).etag, 'e1');
});

test('phone actions: sms to digits, a map of the address', () => {
	assert.equal(smsHref('+49 170 123-45'), 'sms:+4917012345');
	assert.equal(smsHref('*100#'), '');
	const a = { street: 'Lindenallee 7', postcode: '50667', city: 'Köln', region: '', country: '' };
	assert.equal(
		mapHref(a, true),
		'https://maps.apple.com/?q=Lindenallee%207%2C%2050667%20K%C3%B6ln',
	);
	assert.ok(mapHref(a, false).startsWith('https://www.openstreetmap.org/search?query='));
	assert.equal(mapHref({ ...a, street: '', postcode: '', city: '' }, true), '');
});

const contact = (over) => ({
	id: 'a',
	book: 'kontakte',
	etag: 'e',
	first: '',
	last: '',
	name: '',
	org: '',
	phones: [],
	emails: [],
	addresses: [],
	birthday: '',
	notes: '',
	hasPhoto: false,
	url: '',
	modified: '',
	...over,
});

test('merge plan: single values chosen, lists joined, the empty side never wins', () => {
	const a = contact({
		first: 'Ben',
		last: 'Muster',
		phones: [{ type: 'mobile', value: '+49 170 1234567' }],
		emails: [{ type: 'home', value: 'Ben@Mail.example' }],
		notes: 'A',
		hasPhoto: true,
	});
	const b = contact({
		id: 'b',
		first: 'Benjamin',
		last: 'Muster',
		org: 'Firma',
		phones: [
			{ type: 'work', value: '+49 221 555' },
			{ type: 'mobile', value: '+491701234567' },
		],
		emails: [{ type: 'work', value: 'ben@mail.example' }],
		birthday: '1986-11-20',
		notes: 'B',
		hasPhoto: true,
	});
	const plan = planOf(a, b);
	assert.equal(plan.name.differ, true);
	assert.equal(plan.org.differ, false);
	assert.equal(plan.phones.length, 2);
	assert.equal(plan.phones[0].from, 'both');
	assert.equal(plan.emails.length, 1);
	const pick = defaultPick();
	let { input, photo } = mergeInput(plan, pick);
	assert.equal(input.first, 'Ben');
	assert.equal(input.org, 'Firma'); // a has none: b's
	assert.equal(input.birthday, '1986-11-20');
	assert.equal(photo, 'keep');
	({ input, photo } = mergeInput(plan, {
		...pick,
		name: 'b',
		notes: 'both',
		photo: 'b',
		skipPhones: [1],
	}));
	assert.equal(input.first, 'Benjamin');
	assert.equal(input.notes, 'A\n\nB');
	assert.equal(photo, 'drop');
	assert.deepEqual(input.phones, [{ type: 'mobile', value: '+49 170 1234567' }]);
	// Only the other has a picture: it is taken.
	assert.equal(mergeInput(planOf({ ...a, hasPhoto: false }, b), pick).photo, 'drop');
	assert.deepEqual(
		joinValues([1, 2], [2, 3], (x) => String(x)).map((x) => x.from),
		['a', 'both', 'b'],
	);
});
