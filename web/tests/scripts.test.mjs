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

test('the body carries only the fields of the chosen frequency', () => {
	const draft = newDraft();
	draft.name = '  Sichern ';
	assert.deepEqual(draftBody(draft).schedule, undefined);
	assert.equal(draftBody(draft).name, 'Sichern');
	draft.trigger = 'schedule';
	draft.schedule.every = 'weekly';
	draft.schedule.weekday = 5;
	draft.schedule.time = '22:00';
	assert.deepEqual(draftBody(draft).schedule, { every: 'weekly', time: '22:00', weekday: 5 });
	draft.schedule.every = 'custom';
	draft.schedule.cron = ' */5 * * * * ';
	assert.deepEqual(draftBody(draft).schedule, { every: 'custom', cron: '*/5 * * * *' });
	draft.schedule.every = 'hourly';
	draft.schedule.minute = 7;
	assert.deepEqual(draftBody(draft).schedule, { every: 'hourly', minute: 7 });
});

test('a stored script fills the editor and keeps defaults for other frequencies', () => {
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
	assert.equal(draft.schedule.monthDay, 28);
	assert.equal(draft.schedule.weekday, 1);
	assert.equal(draft.content, 'echo hi\n');
});

test('when a script runs, as text keys', () => {
	assert.deepEqual(whenText({ trigger: 'start' }), { key: 'scripts.trigger.start' });
	assert.deepEqual(whenText({ trigger: 'schedule', schedule: { every: 'hourly', minute: 5 } }), {
		key: 'scripts.when.hourly',
		values: { minute: '05' },
	});
	assert.deepEqual(
		whenText(
			{ trigger: 'schedule', schedule: { every: 'weekly', time: '22:00', weekday: 0 } },
			(d) => (d === 0 ? 'Sonntag' : '?'),
		),
		{ key: 'scripts.when.weekly', values: { time: '22:00', day: 'Sonntag' } },
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
