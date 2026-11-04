import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	cronOfPreset,
	describe,
	fromCron,
	nextRuns,
	parseCron,
	simpleOf,
	toCron,
} from '../src/lib/schedule.ts';

test('every simple schedule goes to cron and back unchanged', () => {
	const cases = [
		[{ kind: 'minutes', every: 15 }, '*/15 * * * *'],
		[{ kind: 'minutes', every: 5 }, '*/5 * * * *'],
		[{ kind: 'hourly', minute: 7 }, '7 * * * *'],
		[{ kind: 'daily', time: '03:00' }, '0 3 * * *'],
		[{ kind: 'daily', time: '23:59' }, '59 23 * * *'],
		[{ kind: 'weekdays', days: [1, 4], time: '03:00' }, '0 3 * * 1,4'],
		[{ kind: 'weekdays', days: [1, 6, 0], time: '22:30' }, '30 22 * * 0,1,6'],
		[{ kind: 'monthly', day: 1, time: '01:15' }, '15 1 1 * *'],
		[{ kind: 'monthly', day: 28, time: '04:00' }, '0 4 28 * *'],
		[{ kind: 'monthly', day: 'last', time: '04:00' }, '0 4 L * *'],
	];
	for (const [simple, cron] of cases) {
		assert.equal(toCron(simple), cron, JSON.stringify(simple));
		assert.deepEqual(fromCron(cron), simple, cron);
	}
});

test('equivalent cron spellings land in the simple choice', () => {
	assert.deepEqual(fromCron(' 0  3 * * 1-5 '), {
		kind: 'weekdays',
		days: [1, 2, 3, 4, 5],
		time: '03:00',
	});
	assert.deepEqual(fromCron('0 3 * * 7'), { kind: 'weekdays', days: [0], time: '03:00' });
	assert.deepEqual(fromCron('0 3 * * 0-6'), { kind: 'daily', time: '03:00' });
	assert.deepEqual(
		toCron({ kind: 'weekdays', days: [0, 1, 2, 3, 4, 5, 6], time: '03:00' }),
		'0 3 * * *',
	);
});

test('what the simple choice cannot show stays advanced', () => {
	for (const cron of [
		'*/7 * * * *',
		'0 */2 * * *',
		'0 3 * 1 *',
		'0 3 29 * *',
		'0 3 1 * 1',
		'0,30 3 * * *',
		'0 3 1,15 * *',
		'x',
		'',
	])
		assert.equal(fromCron(cron), null, cron);
});

test('incomplete choices give no expression', () => {
	assert.equal(toCron({ kind: 'weekdays', days: [], time: '03:00' }), null);
	assert.equal(toCron({ kind: 'daily', time: '' }), null);
	assert.equal(toCron({ kind: 'minutes', every: 7 }), null);
	assert.equal(toCron({ kind: 'monthly', day: 30, time: '03:00' }), null);
	assert.deepEqual(simpleOf('weekdays', { kind: 'daily', time: '05:30' }), {
		kind: 'weekdays',
		days: [1],
		time: '05:30',
	});
});

test('parsing follows titand, including L and the OR of the day fields', () => {
	assert.ok(parseCron('0 3 L * *')?.lastDay);
	assert.ok(parseCron('0 3 1,L * *'));
	assert.equal(parseCron('60 * * * *'), null);
	assert.equal(parseCron('* * * *'), null);
	assert.equal(parseCron('0 3 * * L'), null);
});

test('next runs in the time zone of the NAS', () => {
	const from = Date.UTC(2026, 10, 4, 10, 0); // Wed 4 Nov 2026, 11:00 in Berlin
	const iso = (dates) => dates.map((d) => d.toISOString());
	assert.deepEqual(iso(nextRuns('0 3 * * 1,4', 3, 'Europe/Berlin', from)), [
		'2026-11-05T02:00:00.000Z',
		'2026-11-09T02:00:00.000Z',
		'2026-11-12T02:00:00.000Z',
	]);
	assert.deepEqual(iso(nextRuns('*/15 * * * *', 3, 'UTC', from)), [
		'2026-11-04T10:15:00.000Z',
		'2026-11-04T10:30:00.000Z',
		'2026-11-04T10:45:00.000Z',
	]);
	assert.deepEqual(iso(nextRuns('0 4 L * *', 2, 'UTC', from)), [
		'2026-11-30T04:00:00.000Z',
		'2026-12-31T04:00:00.000Z',
	]);
	// 2:30 does not exist on 29 March 2026 in Berlin: that night is left out.
	const spring = Date.UTC(2026, 2, 28, 12, 0);
	assert.deepEqual(iso(nextRuns('30 2 * * *', 2, 'Europe/Berlin', spring)), [
		'2026-03-30T00:30:00.000Z',
		'2026-03-31T00:30:00.000Z',
	]);
	assert.deepEqual(nextRuns('0 0 30 2 *', 3, 'UTC', from), []);
	assert.deepEqual(nextRuns('nonsense', 3, 'UTC', from), []);
});

test('the schedule in words', () => {
	const words = {
		'schedule.say.weekdays': 'Läuft {days} um {time} Uhr.',
		'schedule.say.monthlyLast': 'Läuft am letzten Tag jedes Monats um {time} Uhr.',
		'schedule.say.custom': 'Läuft nach {cron}.',
		'schedule.when.weekdays': '{days} um {time} Uhr',
		'schedule.when.minutes': 'Alle {n} Minuten',
		'schedule.days.1': 'montags',
		'schedule.days.4': 'donnerstags',
		'schedule.days.workdays': 'werktags',
	};
	const tr = (key, values = {}) => words[key].replace(/\{(\w+)\}/g, (_, n) => String(values[n]));
	const join = (items) => new Intl.ListFormat('de', { type: 'conjunction' }).format(items);
	assert.equal(
		describe('0 3 * * 1,4', tr, join, 'say'),
		'Läuft montags und donnerstags um 03:00 Uhr.',
	);
	assert.equal(describe('0 3 * * 1,4', tr, join), 'Montags und donnerstags um 03:00 Uhr');
	assert.equal(describe('0 6 * * 1-5', tr, join, 'say'), 'Läuft werktags um 06:00 Uhr.');
	assert.equal(
		describe('0 4 L * *', tr, join, 'say'),
		'Läuft am letzten Tag jedes Monats um 04:00 Uhr.',
	);
	assert.equal(describe('*/10 * * * *', tr, join), 'Alle 10 Minuten');
	assert.equal(describe('0 */2 * * *', tr, join, 'say'), 'Läuft nach 0 */2 * * *.');
});

test('schedules stored in the older preset form become cron', () => {
	assert.equal(cronOfPreset({ every: 'weekly', time: '22:00', weekday: 5 }), '0 22 * * 5');
	assert.equal(cronOfPreset({ every: 'monthly', time: '01:30', monthDay: 28 }), '30 1 28 * *');
	assert.equal(cronOfPreset({ every: 'hourly', minute: 7 }), '7 * * * *');
	assert.equal(cronOfPreset({ every: 'custom', cron: ' 0 4 * * 0 ' }), '0 4 * * 0');
});
