// 1.15 (PLAN 115-5): the month's bars – lanes, one bar across the days of
// a multi-day event, "+N" per day, the desktop's split into bars and stacks.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	hiddenOf,
	laneCount,
	packLanes,
	weekBars,
	weekLayout,
	weekSplit,
} from '../src/lib/pim/cal/month-lanes.ts';
import { weekDays } from '../src/lib/pim/caldates.ts';
import { eventsByDay } from '../src/lib/pim/calendar.ts';

const ev = (id, start, end, allDay = true, extra = {}) => ({
	kind: 'event',
	id,
	occurrence: extra.occurrence ?? '',
	calendar: 'privat',
	uid: id,
	title: extra.title ?? id,
	start,
	end,
	allDay,
	timezone: '',
	floating: false,
	location: '',
	clipped: false,
	recurring: false,
	rule: null,
	alarms: [],
	exception: false,
	etag: '',
	tzGuess: false,
	recurUnsupported: false,
	readOnly: false,
});
const week = weekDays('2026-11-16'); // Monday 16 … Sunday 22 November
const byDay = (events) => eventsByDay(events, 'Europe/Berlin', week[0], week[6]);

test('packLanes takes the lowest free lane over all columns', () => {
	assert.deepEqual(
		packLanes([
			{ start: 0, span: 3 },
			{ start: 1, span: 1 },
			{ start: 3, span: 2 },
			{ start: 2, span: 2 },
		]),
		[0, 1, 0, 1],
	);
});

test('a multi-day event is one bar across its days, a longer bar on top', () => {
	const bars = weekBars(
		week,
		byDay([ev('one.ics', '2026-11-19', '2026-11-20'), ev('oma.ics', '2026-11-18', '2026-11-21')]),
	);
	const oma = bars.find((b) => b.item.ev.id === 'oma.ics');
	assert.equal(oma.start, 2);
	assert.equal(oma.span, 3);
	assert.equal(oma.lane, 0);
	assert.equal(oma.before, false);
	assert.equal(oma.after, false);
	assert.equal(bars.find((b) => b.item.ev.id === 'one.ics').lane, 1);
});

test('an event over the week border runs on at both ends', () => {
	const bars = weekBars(week, byDay([ev('trip.ics', '2026-11-12', '2026-11-25')]));
	assert.equal(bars.length, 1);
	assert.equal(bars[0].span, 7);
	assert.equal(bars[0].before, true);
	assert.equal(bars[0].after, true);
});

test('occurrences of a series on following days stay bars of their own', () => {
	const days = ['2026-11-16', '2026-11-17', '2026-11-18'];
	const bars = weekBars(
		week,
		byDay(
			days.map((d) =>
				ev('car.ics', `${d}T07:45:00+01:00`, `${d}T08:15:00+01:00`, false, {
					occurrence: d.replaceAll('-', '') + 'T064500Z',
				}),
			),
		),
	);
	assert.equal(bars.length, 3);
	assert.ok(bars.every((b) => b.span === 1 && b.lane === 0));
});

test('the same event key on two days merges only when its part runs on', () => {
	// A timed event over midnight: two parts, one bar.
	const bars = weekBars(
		week,
		byDay([ev('night.ics', '2026-11-20T22:00:00+01:00', '2026-11-21T02:00:00+01:00', false)]),
	);
	assert.equal(bars.length, 1);
	assert.equal(bars[0].span, 2);
});

test('the phone shows a few lanes and "+N" per day for the rest', () => {
	const events = [1, 2, 3, 4, 5].map((n) => ev(`e${n}.ics`, '2026-11-17', '2026-11-18'));
	const due = new Map([['2026-11-17', [{ list: 'l', id: 't.ics', title: 'T' }]]]);
	const lay = weekLayout(week, byDay(events), due);
	assert.equal(lay.lanes, 6);
	assert.equal(laneCount(lay.bars), 5);
	assert.deepEqual(hiddenOf(lay, 3), [0, 3, 0, 0, 0, 0, 0]);
});

test('the desktop splits a week: bars over days on top, each day its stack', () => {
	const sp = weekSplit(
		week,
		byDay([
			ev('oma.ics', '2026-11-18', '2026-11-21'),
			ev('kino.ics', '2026-11-19T20:00:00+01:00', '2026-11-19T22:15:00+01:00', false),
		]),
	);
	assert.equal(sp.bars.length, 1);
	assert.equal(sp.lanes, 1);
	assert.equal(sp.days[3].items.length, 1);
	assert.equal(sp.days[3].items[0].ev.id, 'kino.ics');
	assert.equal(sp.days[2].items.length, 0);
});
