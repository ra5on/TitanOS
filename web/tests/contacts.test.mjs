import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	addressLines,
	birthdayOf,
	blankForm,
	fold,
	formOf,
	groupByLetter,
	initialsOf,
	inputOf,
	letterOf,
	markParts,
	namesSomeone,
	parseBirthday,
	sanitizeContact,
	sanitizeItems,
	sanitizeSuggestions,
	soonWhen,
	subline,
	telHref,
	webHref,
} from '../src/lib/pim/contacts.ts';

const item = (name, sortKey = name) => ({ name, sortKey });

test('letters after German folding: Ä under A, ß like ss, digits and others under #', () => {
	assert.equal(fold('Ärztehaus Straße'), 'arztehaus strasse');
	assert.equal(fold('Élodie Çelik'), 'elodie celik');
	assert.equal(letterOf(item('Ärztehaus')), 'A');
	assert.equal(letterOf(item('Ben Muster', 'muster ben')), 'M');
	assert.equal(letterOf(item('Öztürk')), 'O');
	assert.equal(letterOf(item('Über uns')), 'U');
	assert.equal(letterOf(item('112 Notruf')), '#');
	assert.equal(letterOf(item('', '')), '#');
	// titand sorts (DIN 5007-1): the groups follow that order, one per letter run.
	const groups = groupByLetter([
		item('Anna', 'beispiel anna'),
		item('Ärztehaus', 'arztehaus'),
		item('Ben', 'muster ben'),
		item('Müller', 'muller'),
	]);
	assert.deepEqual(
		groups.map((g) => [g.letter, g.items.length]),
		[
			['B', 1],
			['A', 1],
			['M', 2],
		],
	);
	assert.deepEqual(
		groupByLetter([item('Ärztehaus', 'arztehaus'), item('Anna', 'anna')]).map((g) => g.letter),
		['A'],
	);
});

test('initials of a name without titand’s', () => {
	assert.equal(initialsOf('Oma Hilde'), 'OH');
	assert.equal(initialsOf('Grundschule'), 'G');
	assert.equal(initialsOf('carla beispiel-hoffmann'), 'CB');
	assert.equal(initialsOf('Özil, Mesut'), 'ÖM');
	assert.equal(initialsOf('(Büro) Ben'), 'BB');
	assert.equal(initialsOf(''), '?');
});

test('marks are found folded and map back to the original letters', () => {
	assert.deepEqual(markParts('Anna Beispiel', 'beispiel'), [
		['Anna ', false],
		['Beispiel', true],
	]);
	assert.deepEqual(markParts('Eva Müller', 'muller'), [
		['Eva ', false],
		['Müller', true],
	]);
	assert.deepEqual(markParts('Lindenstraße 7', 'strasse'), [
		['Linden', false],
		['straße', true],
		[' 7', false],
	]);
	// ß folds to two letters: half of it still marks the whole ß.
	assert.deepEqual(markParts('Fuß', 'fus'), [['Fuß', true]]);
	assert.deepEqual(markParts('Carla Beispiel-Beispiel', 'BEISPIEL'), [
		['Carla ', false],
		['Beispiel', true],
		['-', false],
		['Beispiel', true],
	]);
	assert.deepEqual(markParts('René', 'rene'), [['René', true]]);
	assert.deepEqual(markParts('Ben', ''), [['Ben', false]]);
	assert.deepEqual(markParts('Ben', 'xyz'), [['Ben', false]]);
});

test('second line: the company unless it is the name, else the e-mail address', () => {
	assert.equal(subline({ name: 'Anna', org: 'Praxis', email: 'a@x' }), 'Praxis');
	assert.equal(subline({ name: 'Grundschule', org: 'Grundschule', email: '' }), '');
	assert.equal(subline({ name: 'Eva', org: '', email: 'eva@x' }), 'eva@x');
});

test('tel links of numbers as people write them; web links http(s) only', () => {
	assert.equal(telHref('+49 151 7654321'), 'tel:+491517654321');
	assert.equal(telHref('(0221) 555-0142'), 'tel:02215550142');
	assert.equal(telHref(' 0049 (0)30 / 12 34 '), 'tel:00490301234');
	assert.equal(telHref('*100#'), 'tel:*100#');
	assert.equal(telHref('keine'), '');
	assert.equal(telHref('javascript:alert(1)'), 'tel:1');
	assert.equal(webHref('https://example.com/a b'), 'https://example.com/a%20b');
	assert.equal(webHref('javascript:alert(1)'), '');
	assert.equal(webHref('ftp://x'), '');
	assert.equal(webHref('kein link'), '');
});

test('address lines as on a letter', () => {
	assert.deepEqual(
		addressLines({
			street: 'Lindenallee 7\nHinterhaus',
			postcode: '50667',
			city: 'Köln',
			region: '',
			country: 'Deutschland',
		}),
		['Lindenallee 7', 'Hinterhaus', '50667 Köln', 'Deutschland'],
	);
	assert.deepEqual(
		addressLines({ street: '', postcode: '', city: 'Paris', region: 'IDF', country: '' }),
		['Paris', 'IDF'],
	);
});

test('birthdays with and without year; this week seen from Tuesday 17 November 2026', () => {
	assert.deepEqual(parseBirthday('1986-11-20'), { year: 1986, month: 11, day: 20 });
	assert.deepEqual(parseBirthday('--02-29'), { year: null, month: 2, day: 29 });
	assert.equal(parseBirthday('20.11.1986'), null);
	const today = '2026-11-17';
	assert.deepEqual(soonWhen({ date: '2026-11-17', age: 40 }, today), {
		when: 'today',
		weekday: 1,
	});
	assert.deepEqual(soonWhen({ date: '2026-11-20' }, today), { when: 'coming', weekday: 4 });
	assert.deepEqual(soonWhen({ date: '2026-11-16' }, today), { when: 'past', weekday: 0 });
	assert.deepEqual(soonWhen({ date: '2026-11-22' }, today), { when: 'coming', weekday: 6 });
});

const contact = {
	id: 'ben.vcf',
	book: 'kontakte',
	etag: '"1"',
	first: 'Ben',
	last: 'Muster',
	name: 'Ben Muster',
	org: '',
	phones: [
		{ type: 'mobile', value: '+49 151 7654321' },
		{ type: 'other', value: '0221 1', label: 'Werkstatt' },
	],
	emails: [{ type: 'home', value: 'ben@mail.example' }],
	addresses: [
		{
			type: 'home',
			street: 'Lindenallee 7\nHinterhaus',
			city: 'Köln',
			postcode: '50667',
			region: 'NRW',
			country: 'Deutschland',
		},
	],
	birthday: '1986-11-20',
	notes: 'Spielt Fußball.',
	hasPhoto: true,
	url: '',
	modified: '',
};

test('the API shapes are checked', () => {
	assert.equal(sanitizeItems({}), null);
	assert.deepEqual(
		sanitizeItems([
			{ id: 'a.vcf', name: 'A', birthdaySoon: { date: '2026-11-20', age: 40 } },
			{ name: 'no id' },
			{ id: 'b.vcf', birthdaySoon: { date: 'soon' } },
		]).map((i) => [i.id, i.birthdaySoon]),
		[
			['a.vcf', { date: '2026-11-20', age: 40 }],
			['b.vcf', null],
		],
	);
	const c = sanitizeContact({ ...contact, url: 'javascript:x', birthday: '1604?' });
	assert.equal(c.url, '');
	assert.equal(c.birthday, '');
	assert.equal(c.phones[1].label, 'Werkstatt');
	assert.equal(sanitizeContact({}), null);
	assert.deepEqual(sanitizeSuggestions([{ addr: 'x@y', name: 'X', count: 3 }, { name: 'n' }]), [
		{ name: 'X', addr: 'x@y', count: 3 },
	]);
});

test('a changed contact sends only the fields that changed, never a label', () => {
	const before = formOf(contact);
	assert.deepEqual(inputOf(structuredClone(before), before), {});
	const after = structuredClone(before);
	after.notes = 'Spielt Handball.';
	assert.deepEqual(inputOf(after, before), { notes: 'Spielt Handball.' });
	// Spaces around values are no change.
	const spaced = structuredClone(before);
	spaced.first = ' Ben ';
	spaced.phones[0].value = '+49 151 7654321 ';
	assert.deepEqual(inputOf(spaced, before), {});
	// A list goes as a whole, without labels, empty rows dropped.
	const phones = structuredClone(before);
	phones.phones[0].type = 'work';
	phones.phones.push({ type: 'home', value: '  ' });
	assert.deepEqual(inputOf(phones, before), {
		phones: [
			{ type: 'work', value: '+49 151 7654321' },
			{ type: 'other', value: '0221 1' },
		],
	});
	// An address keeps its region and the lines of its street.
	const adr = structuredClone(before);
	adr.addresses[0].city = 'Bonn';
	assert.deepEqual(inputOf(adr, before).addresses, [
		{
			type: 'home',
			street: 'Lindenallee 7\nHinterhaus',
			city: 'Bonn',
			postcode: '50667',
			region: 'NRW',
			country: 'Deutschland',
		},
	]);
	// The birthday without year; and cleared.
	const noYear = structuredClone(before);
	noYear.birthdayNoYear = true;
	assert.deepEqual(inputOf(noYear, before), { birthday: '--11-20' });
	const cleared = structuredClone(before);
	cleared.birthdayDate = '';
	assert.deepEqual(inputOf(cleared, before), { birthday: '' });
	// A birthday titand cannot read ('') stays unless it is set.
	const unread = formOf({ ...contact, birthday: '' });
	assert.deepEqual(inputOf(structuredClone(unread), unread), {});
});

test('the form of a birthday without year uses a leap year; a new contact sends what is filled', () => {
	const f = formOf({ ...contact, birthday: '--02-29' });
	assert.equal(f.birthdayDate, '2000-02-29');
	assert.equal(f.birthdayNoYear, true);
	assert.equal(birthdayOf(f), '--02-29');
	assert.equal(birthdayOf({ birthdayDate: '', birthdayNoYear: true }), '');
	const blank = blankForm();
	assert.equal(namesSomeone(blank), false);
	blank.emails.push({ type: 'home', value: ' ' });
	assert.equal(namesSomeone(blank), false);
	blank.first = 'Lena';
	blank.emails.push({ type: 'work', value: 'lena@x' });
	blank.birthdayDate = '1990-05-01';
	assert.deepEqual(inputOf(blank, null), {
		first: 'Lena',
		emails: [{ type: 'work', value: 'lena@x' }],
		birthday: '1990-05-01',
	});
});
