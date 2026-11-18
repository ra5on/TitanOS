import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	addDays,
	addMonths,
	dayParts,
	exclusiveEnd,
	inclusiveEnd,
	isoWeek,
	longEvent,
	monthGrid,
	overlapLanes,
	wallMinutes,
	wallOf,
	wallParts,
	weekDays,
	weekStart,
} from '../src/lib/pim/caldates.ts';
import {
	alarmWords,
	apiTimes,
	calColor,
	fromRule,
	linkPieces,
	repeatKinds,
	sanitizeCalendars,
	sanitizeEvent,
	sanitizeEventList,
	squareOf,
	toSimple,
} from '../src/lib/pim/calendar.ts';

const BERLIN = 'Europe/Berlin';

test('ISO weeks: Monday first, week 1 holds the first Thursday', () => {
	assert.equal(isoWeek('2026-11-17'), 47);
	assert.equal(isoWeek('2026-10-26'), 44);
	assert.equal(isoWeek('2026-12-31'), 53);
	assert.equal(isoWeek('2027-01-01'), 53);
	assert.equal(isoWeek('2027-01-04'), 1);
	assert.equal(isoWeek('2021-01-03'), 53);
	assert.equal(isoWeek('2024-12-30'), 1);
	assert.equal(weekStart('2026-11-22'), '2026-11-16');
	assert.equal(weekStart('2026-11-16'), '2026-11-16');
	assert.deepEqual(weekDays('2026-11-17'), [
		'2026-11-16',
		'2026-11-17',
		'2026-11-18',
		'2026-11-19',
		'2026-11-20',
		'2026-11-21',
		'2026-11-22',
	]);
});

test('month grid: whole weeks from Monday, as many rows as the month needs', () => {
	const nov = monthGrid('2026-11-17');
	assert.equal(nov.length, 6);
	assert.equal(nov[0][0], '2026-10-26');
	assert.equal(nov[5][6], '2026-12-06');
	// February 2027 starts on a Monday and has exactly four weeks.
	const feb = monthGrid('2027-02-10');
	assert.equal(feb.length, 4);
	assert.equal(feb[0][0], '2027-02-01');
	assert.equal(feb[3][6], '2027-02-28');
	assert.ok(monthGrid('2026-03-01').every((w) => w.length === 7));
	assert.equal(addMonths('2026-01-31', 1), '2026-02-01');
	assert.equal(addMonths('2026-01-15', -1), '2025-12-01');
	assert.equal(addDays('2026-10-25', 1), '2026-10-26');
});

test('all-day end is exclusive: a one-day event covers one day, three days three', () => {
	assert.deepEqual(
		dayParts({ start: '2026-11-18', end: '2026-11-19', allDay: true }, BERLIN).map((p) => p.date),
		['2026-11-18'],
	);
	const three = dayParts({ start: '2026-11-18', end: '2026-11-21', allDay: true }, BERLIN);
	assert.deepEqual(
		three.map((p) => p.date),
		['2026-11-18', '2026-11-19', '2026-11-20'],
	);
	assert.equal(three[0].first, true);
	assert.equal(three[2].last, true);
	// A broken end still shows the start day.
	assert.equal(
		dayParts({ start: '2026-11-18', end: '2026-11-18', allDay: true }, BERLIN).length,
		1,
	);
	assert.equal(exclusiveEnd('2026-11-20'), '2026-11-21');
	assert.equal(inclusiveEnd('2026-11-18', '2026-11-21'), '2026-11-20');
	assert.equal(inclusiveEnd('2026-11-18', '2026-11-18'), '2026-11-18');
	assert.deepEqual(
		apiTimes({
			allDay: true,
			startDate: '2026-11-18',
			startTime: '',
			endDate: '2026-11-20',
			endTime: '',
		}),
		{ start: '2026-11-18', end: '2026-11-21', allDay: true },
	);
});

test('timed events in a zone: offsets, over midnight, an end at midnight', () => {
	const ev = {
		start: '2026-11-17T19:00:00+01:00',
		end: '2026-11-17T20:30:00+01:00',
		allDay: false,
	};
	assert.deepEqual(dayParts(ev, BERLIN), [
		{ date: '2026-11-17', allDay: false, from: 1140, to: 1230, first: true, last: true },
	]);
	// The same instant written in New York's offset lands at the same Berlin time.
	const ny = {
		start: '2026-11-17T13:00:00-05:00',
		end: '2026-11-17T14:30:00-05:00',
		allDay: false,
	};
	assert.equal(dayParts(ny, BERLIN)[0].from, 1140);
	const night = {
		start: '2026-11-21T22:00:00+01:00',
		end: '2026-11-22T02:00:00+01:00',
		allDay: false,
	};
	const parts = dayParts(night, BERLIN);
	assert.deepEqual(
		parts.map((p) => [p.date, p.from, p.to]),
		[
			['2026-11-21', 1320, 1440],
			['2026-11-22', 0, 120],
		],
	);
	assert.equal(longEvent(night, BERLIN), true);
	const toMidnight = {
		start: '2026-11-21T22:00:00+01:00',
		end: '2026-11-22T00:00:00+01:00',
		allDay: false,
	};
	assert.deepEqual(
		dayParts(toMidnight, BERLIN).map((p) => [p.date, p.to]),
		[['2026-11-21', 1440]],
	);
	assert.equal(longEvent(toMidnight, BERLIN), false);
	// DST end in Berlin: 25 October 2026, 03:00 → 02:00.
	assert.deepEqual(wallOf(new Date('2026-10-25T01:30:00Z'), BERLIN), {
		date: '2026-10-25',
		minutes: 150,
	});
	assert.deepEqual(wallParts('2026-11-17T19:00:00+01:00'), { date: '2026-11-17', time: '19:00' });
	assert.equal(wallMinutes('2026-11-17', '19:00', '2026-11-17', '20:30'), 90);
	assert.equal(wallMinutes('2026-11-17', '23:00', '2026-11-18', '01:00'), 120);
});

test('overlapping events share the width; separate ones keep it', () => {
	const lanes = overlapLanes([
		{ from: 600, to: 690 }, // 10:00–11:30
		{ from: 750, to: 795 }, // 12:30–13:15
		{ from: 630, to: 660 }, // 10:30–11:00 overlaps the first
		{ from: 645, to: 720 }, // 10:45–12:00 overlaps both
		{ from: 900, to: 930 },
	]);
	assert.deepEqual(lanes[0], { lane: 0, lanes: 3 });
	assert.deepEqual(lanes[2], { lane: 1, lanes: 3 });
	assert.deepEqual(lanes[3], { lane: 2, lanes: 3 });
	assert.deepEqual(lanes[1], { lane: 0, lanes: 1 });
	assert.deepEqual(lanes[4], { lane: 0, lanes: 1 });
	// A short event counts 30 minutes: the next one 10 minutes later sits beside it.
	assert.deepEqual(
		overlapLanes([
			{ from: 600, to: 605 },
			{ from: 610, to: 640 },
		]),
		[
			{ lane: 0, lanes: 2 },
			{ lane: 1, lanes: 2 },
		],
	);
	// Back to back: the same lane.
	assert.deepEqual(
		overlapLanes([
			{ from: 540, to: 600 },
			{ from: 600, to: 660 },
		]).map((l) => l.lanes),
		[1, 1],
	);
});

const R = (kind, more = {}) => ({ kind, interval: 1, end: 'never', until: '', count: 10, ...more });

test('repeat presets become the API rule for the start date', () => {
	const start = '2026-11-17'; // Tuesday, third Tuesday of November
	assert.equal(toSimple(R('none'), start), null);
	assert.deepEqual(toSimple(R('daily'), start), { freq: 'daily', interval: 1 });
	assert.deepEqual(toSimple(R('workdays'), start), {
		freq: 'weekly',
		interval: 1,
		byDay: ['MO', 'TU', 'WE', 'TH', 'FR'],
	});
	assert.deepEqual(toSimple(R('weekly', { interval: 2 }), start), {
		freq: 'weekly',
		interval: 2,
		byDay: ['TU'],
	});
	assert.deepEqual(toSimple(R('monthlyDay'), start), {
		freq: 'monthly',
		interval: 1,
		byMonthDay: [17],
	});
	assert.deepEqual(toSimple(R('monthlyNth', { end: 'date', until: '2027-07-13' }), start), {
		freq: 'monthly',
		interval: 1,
		byDay: ['3TU'],
		until: '2027-07-13',
	});
	assert.deepEqual(toSimple(R('yearly', { end: 'count', count: 4 }), start), {
		freq: 'yearly',
		interval: 1,
		count: 4,
	});
	assert.deepEqual(toSimple(R('monthlyLast'), '2026-11-24'), {
		freq: 'monthly',
		interval: 1,
		byDay: ['-1TU'],
	});
	assert.deepEqual(toSimple(R('custom'), start), { freq: 'daily', interval: 1, custom: true });
	// The fifth Tuesday is only "the last"; the 24th is both the fourth and the last.
	assert.deepEqual(repeatKinds('2026-12-29').includes('monthlyNth'), false);
	assert.ok(repeatKinds('2026-12-29').includes('monthlyLast'));
	assert.ok(repeatKinds('2026-11-24').includes('monthlyNth'));
	assert.ok(repeatKinds('2026-11-24').includes('monthlyLast'));
	assert.ok(!repeatKinds('2026-11-17').includes('monthlyLast'));
});

test('rules of the API map back to the preset, anything else stays custom (text only)', () => {
	const start = '2026-11-17';
	const back = (rule, recurring = true) => fromRule(rule, recurring, start);
	assert.equal(back(null, false).kind, 'none');
	assert.equal(back(null, true).kind, 'custom'); // RDATE only: not shown as a rule
	assert.deepEqual(back({ freq: 'monthly', interval: 1, byDay: ['3TU'], until: '2027-07-13' }), {
		kind: 'monthlyNth',
		interval: 1,
		end: 'date',
		until: '2027-07-13',
		count: 10,
	});
	assert.equal(back({ freq: 'weekly', interval: 1, byDay: ['TU'] }).kind, 'weekly');
	assert.equal(back({ freq: 'weekly', interval: 1 }).kind, 'weekly');
	assert.equal(
		back({ freq: 'weekly', interval: 1, byDay: ['MO', 'TU', 'WE', 'TH', 'FR'] }).kind,
		'workdays',
	);
	assert.equal(
		back({ freq: 'weekly', interval: 2, byDay: ['MO', 'TU', 'WE', 'TH', 'FR'] }).kind,
		'custom',
	);
	assert.equal(back({ freq: 'weekly', interval: 1, byDay: ['MO', 'WE'] }).kind, 'custom');
	assert.equal(back({ freq: 'monthly', interval: 1, byMonthDay: [17] }).kind, 'monthlyDay');
	assert.equal(back({ freq: 'monthly', interval: 1, byMonthDay: [18] }).kind, 'custom');
	assert.equal(back({ freq: 'yearly', interval: 1, count: 4 }).end, 'count');
	assert.equal(back({ freq: 'daily', interval: 3 }).interval, 3);
	assert.equal(back({ freq: 'daily', interval: 1, custom: true }).kind, 'custom');
	assert.equal(
		fromRule({ freq: 'monthly', interval: 1, byDay: ['-1TU'] }, true, '2026-11-24').kind,
		'monthlyLast',
	);
	// Round trip of every preset.
	for (const kind of repeatKinds('2026-11-24').filter((k) => k !== 'none')) {
		const simple = toSimple(R(kind), '2026-11-24');
		assert.equal(fromRule(simple, true, '2026-11-24').kind, kind, kind);
	}
});

test('reminders in parts for the words', () => {
	assert.deepEqual(alarmWords(0, false), { kind: 'start' });
	assert.deepEqual(alarmWords(30, false), { kind: 'minutes', n: 30 });
	assert.deepEqual(alarmWords(60, false), { kind: 'hours', n: 1 });
	assert.deepEqual(alarmWords(90, false), { kind: 'mixed', hours: 1, minutes: 30 });
	assert.deepEqual(alarmWords(2880, false), { kind: 'days', n: 2 });
	assert.deepEqual(alarmWords(10080, false), { kind: 'weeks', n: 1 });
	assert.deepEqual(alarmWords(360, true), { kind: 'dayAt', days: 1, time: '18:00' });
	assert.deepEqual(alarmWords(900, true), { kind: 'dayAt', days: 1, time: '09:00' });
	assert.deepEqual(alarmWords(0, true), { kind: 'dayAt', days: 0, time: '00:00' });
	assert.deepEqual(alarmWords(9540, true), { kind: 'dayAt', days: 7, time: '09:00' });
});

test('calendar colors are always one of the square variables', () => {
	assert.equal(calColor('#0f766e'), 'var(--icon-square-teal)');
	assert.equal(calColor('#be185d'), 'var(--icon-square-pink)');
	// A color an iPhone chose: the closest square.
	assert.equal(squareOf('#ff2d55'), 'red');
	assert.equal(squareOf('#34c759'), 'green');
	assert.equal(calColor('not a color'), 'var(--icon-square-violet)');
});

test('links in notes: only http(s), mailto and tel, the text is the real target', () => {
	const pieces = linkPieces(
		'Plan: https://example.org/a?b=1. Mail mailto:anna@example.org, Tel tel:+49-221-555 – javascript:alert(1) file:///etc',
	);
	assert.deepEqual(
		pieces.filter((p) => p.href).map((p) => p.href),
		['https://example.org/a?b=1', 'mailto:anna@example.org', 'tel:+49-221-555'],
	);
	assert.equal(pieces.map((p) => p.text).join(''), pieces.map((p) => p.text).join(''));
	assert.ok(pieces.every((p) => !p.href || p.href === p.text));
	assert.ok(pieces.some((p) => !p.href && p.text.includes('javascript:alert(1)')));
	assert.deepEqual(linkPieces('kein Link'), [{ text: 'kein Link' }]);
});

test('API answers are checked: text stays text, unknown shapes are dropped', () => {
	const cals = sanitizeCalendars([
		{ id: 'privat', name: 'Privat', color: '#2563EB', order: 0, kind: 'own', count: 3 },
		{
			id: 'geburtstage',
			name: 'Geburtstage',
			color: '#be185d',
			readOnly: true,
			kind: 'birthdays',
			unavailable: true,
		},
		{ name: 'ohne id' },
		null,
	]);
	assert.equal(cals.length, 2);
	assert.equal(cals[0].color, '#2563eb');
	assert.equal(cals[1].kind, 'birthdays');
	assert.equal(cals[1].unavailable, true);
	assert.equal(sanitizeCalendars({}), null);
	const ev = sanitizeEvent({
		kind: 'event',
		id: 'a b+c@x.ics',
		start: '2026-11-17T19:00:00+01:00',
		end: '2026-11-17T20:30:00+01:00',
		title: '<img src=x onerror=alert(1)>',
		rule: {
			freq: 'monthly',
			interval: 1,
			byDay: ['3TU'],
			text: { de: 'jeden Monat', en: 'monthly' },
		},
		alarms: [60, 'x', -5],
		occurrences: 9,
	});
	assert.equal(ev.title, '<img src=x onerror=alert(1)>');
	assert.deepEqual(ev.alarms, [60]);
	assert.equal(ev.rule.text.de, 'jeden Monat');
	assert.equal(ev.occurrences, 9);
	const bday = sanitizeEvent({
		kind: 'birthday',
		id: 'anna.vcf',
		start: '2026-11-12',
		end: '2026-11-13',
		allDay: true,
		birthday: { name: 'Anna', year: 1991, age: 35 },
	});
	assert.equal(bday.readOnly, true);
	assert.equal(bday.birthday.age, 35);
	assert.equal(sanitizeEventList([]), null);
	assert.equal(sanitizeEventList({ events: [{ id: '' }], truncated: true }).events.length, 0);
});

test('events per day: all-day first, then by time; a series shows on each of its days', async () => {
	const { eventsByDay } = await import('../src/lib/pim/calendar.ts');
	const mk = (id, start, end, allDay = false, title = id) =>
		sanitizeEvent({ id, start, end, allDay, title, calendar: 'privat', occurrence: start });
	const map = eventsByDay(
		[
			mk('late', '2026-11-17T19:00:00+01:00', '2026-11-17T20:30:00+01:00'),
			mk('early', '2026-11-17T07:45:00+01:00', '2026-11-17T08:15:00+01:00'),
			mk('oma', '2026-11-16', '2026-11-19', true),
			mk('out', '2026-12-24', '2026-12-25', true),
		],
		BERLIN,
		'2026-11-16',
		'2026-11-22',
	);
	assert.deepEqual(
		map.get('2026-11-17').map((i) => i.ev.id),
		['oma', 'early', 'late'],
	);
	assert.deepEqual([...map.keys()].sort(), ['2026-11-16', '2026-11-17', '2026-11-18']);
	assert.equal(map.get('2026-11-18')[0].part.last, true);
});

test('a timed reminder a day or more before, not whole days: a time of a day before', () => {
	assert.deepEqual(alarmWords(1500, false, 19 * 60), { kind: 'dayAt', days: 1, time: '18:00' });
	assert.deepEqual(alarmWords(1440, false, 19 * 60), { kind: 'days', n: 1 });
	assert.deepEqual(alarmWords(120, false, 60), { kind: 'hours', n: 2 });
	assert.deepEqual(alarmWords(3000, false, 9 * 60), { kind: 'dayAt', days: 2, time: '07:00' });
});

test('the texts of T-Kalender exist in German and English with the same keys', async () => {
	const { readFileSync } = await import('node:fs');
	const keys = (lang) =>
		Object.keys(JSON.parse(readFileSync(`src/lib/i18n/${lang}.json`, 'utf8')))
			.filter((k) => k.startsWith('calendar.'))
			.sort();
	assert.deepEqual(keys('de'), keys('en'));
	assert.ok(keys('de').length > 150);
});
