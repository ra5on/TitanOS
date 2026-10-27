import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	newRule,
	nextScheduled,
	scheduleDraft,
	schedulePayload,
	scheduleProblems,
	timeoutChoices,
} from '../src/lib/vm-schedule.ts';

test('a stored schedule becomes an independent draft with defaults', () => {
	const stored = { enabled: true, rules: [{ action: 'start', days: [5, 1], time: '07:00' }] };
	const draft = scheduleDraft(stored);
	assert.equal(draft.stopTimeout, 300);
	assert.equal(draft.forceOff, false);
	draft.rules[0].days.push(3);
	assert.deepEqual(stored.rules[0].days, [5, 1]);
	assert.deepEqual(scheduleDraft(undefined), {
		enabled: false,
		rules: [],
		stopTimeout: 300,
		forceOff: false,
	});
	assert.deepEqual(schedulePayload(draft).rules[0].days, [1, 3, 5]);
});

test('new rules alternate start and stop', () => {
	const first = newRule([]);
	assert.equal(first.action, 'start');
	const second = newRule([first]);
	assert.equal(second.action, 'stop');
	assert.deepEqual(second.days, first.days);
});

test('problems: no rule, missing days, bad time and start/stop clash', () => {
	assert.deepEqual(scheduleProblems(scheduleDraft({ enabled: true, rules: [] })), [
		{ key: 'noRule' },
	]);
	assert.deepEqual(scheduleProblems(scheduleDraft({ enabled: false, rules: [] })), []);
	const draft = scheduleDraft({
		enabled: true,
		rules: [
			{ action: 'start', days: [1], time: '07:00' },
			{ action: 'stop', days: [], time: '24:00' },
			{ action: 'stop', days: [1, 2], time: '07:00' },
		],
	});
	assert.deepEqual(scheduleProblems(draft), [
		{ key: 'time', n: 2 },
		{ key: 'days', n: 2 },
		{ key: 'clash' },
	]);
});

test('the next scheduled action is the earlier one', () => {
	assert.equal(nextScheduled(null), null);
	assert.deepEqual(
		nextScheduled({
			nextStart: '2026-10-28T07:00:00+01:00',
			nextStop: '2026-10-27T22:00:00+01:00',
		}),
		{ action: 'stop', at: '2026-10-27T22:00:00+01:00' },
	);
	assert.deepEqual(timeoutChoices(45).slice(0, 2), [45, 60]);
});
