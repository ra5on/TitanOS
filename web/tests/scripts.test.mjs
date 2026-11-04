import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	draftBody,
	draftOf,
	newDraft,
	outcomeTone,
	shortDuration,
	whenText,
} from '../src/routes/tools/scripts/scripts.ts';
import { noticeLink } from '../src/lib/notice-link.ts';
import { activityLabel } from '../src/lib/activity.ts';

test('the schedule always goes as cron expression', () => {
	const draft = newDraft();
	draft.name = '  Sichern ';
	assert.deepEqual(draftBody(draft).schedule, undefined);
	assert.equal(draftBody(draft).name, 'Sichern');
	draft.trigger = 'schedule';
	draft.cron = ' 0 3 * * 1,4 ';
	assert.deepEqual(draftBody(draft).schedule, { every: 'custom', cron: '0 3 * * 1,4' });
});

test('a stored script in the older preset form opens as cron', () => {
	const draft = draftOf(
		{
			id: 'a',
			name: 'x',
			description: '',
			trigger: 'schedule',
			schedule: { every: 'monthly', time: '01:30', monthDay: 28 },
			changed: '',
		},
		'echo hi\n',
	);
	assert.equal(draft.cron, '30 1 28 * *');
	assert.equal(draft.content, 'echo hi\n');
});

test('when a script runs, in words', () => {
	const words = {
		'scripts.trigger.start': 'Beim Start',
		'schedule.when.hourly': 'jede Stunde zur Minute {minute}',
	};
	const tr = (key, values = {}) => words[key].replace(/\{(\w+)\}/g, (_, n) => String(values[n]));
	const join = (items) => items.join(', ');
	assert.equal(whenText({ trigger: 'start' }, tr, join), 'Beim Start');
	assert.equal(
		whenText({ trigger: 'schedule', schedule: { every: 'custom', cron: '5 * * * *' } }, tr, join),
		'Jede Stunde zur Minute 05',
	);
});

test('outcomes, durations, links and log labels', () => {
	assert.equal(outcomeTone('ok'), 'ok');
	assert.equal(outcomeTone('timeout'), 'error');
	assert.equal(outcomeTone('canceled'), 'warn');
	assert.equal(shortDuration(450), '450 ms');
	assert.equal(shortDuration(61_000), '61 s');
	assert.equal(shortDuration(185_000), '3 min 5 s');
	assert.equal(noticeLink('script_failed', 'x'), '/tools/scripts');
	assert.equal(noticeLink('script_volume_missing', 'Data'), '/tools/scripts');
	assert.deepEqual(activityLabel('POST /api/v1/scripts/{id}/run'), {
		kind: 'generic',
		area: 'scripts',
		verb: 'run',
	});
});
