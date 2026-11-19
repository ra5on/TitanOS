import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	skillIn,
	activeSchedules,
	DOT_SIZES,
	areaMode,
	blocks,
	groupChats,
	levelLabel,
	mergeEntries,
	parseEvent,
	presetOf,
	sanitizeLook,
	approvalTitle,
	whereLine,
	accountLook,
	stepDetail,
	toolMode,
	AREAS,
	dataRight,
	presetDataMode,
} from '../src/lib/agent.ts';

test('server-sent events of a running chat', () => {
	assert.deepEqual(parseEvent('event: content\ndata: {"type":"content","text":"Hi"}'), {
		type: 'content',
		text: 'Hi',
	});
	assert.deepEqual(parseEvent('event: end\ndata: {}'), { type: 'end' });
	assert.equal(parseEvent(': keep-alive'), null);
	assert.equal(parseEvent('event: entry\ndata: {broken'), null);
});

test('a snapshot of the run: parsed, and its entries take their place by id (1.01)', () => {
	const snap = parseEvent(
		'event: snapshot\ndata: {"type":"snapshot","entries":[{"id":"a1","role":"assistant"}],"content":"Hal","state":"running"}',
	);
	assert.equal(snap?.type, 'snapshot');
	assert.equal(snap?.content, 'Hal');
	const e = (id, role = 'tool') => ({ id, role, at: '' });
	// The page had the question and an older copy of the first line.
	assert.deepEqual(
		mergeEntries([e('u1', 'user'), e('a1', 'assistant')], [e('a1', 'assistant'), e('t1')]).map(
			(x) => x.id,
		),
		['u1', 'a1', 't1'],
	);
	// A step that waited: its answer stands before the calls not made yet.
	assert.deepEqual(
		mergeEntries([e('u1', 'user'), e('skip1'), e('skip2')], [e('ok'), e('skip1'), e('skip2')]).map(
			(x) => x.id,
		),
		['u1', 'ok', 'skip1', 'skip2'],
	);
	// Nothing new: the chat stays as it is.
	assert.deepEqual(
		mergeEntries([e('u1', 'user')], []).map((x) => x.id),
		['u1'],
	);
});

test('chats are grouped by day, pinned first', () => {
	const now = new Date('2026-11-05T12:00:00');
	const c = (id, updated, pinned = false) => ({
		id,
		title: id,
		updated,
		created: updated,
		state: 'idle',
		pinned,
	});
	const groups = groupChats(
		[
			c('a', '2026-11-05T09:00:00'),
			c('b', '2026-11-04T20:00:00'),
			c('p', '2026-10-01T09:00:00', true),
			c('o', '2026-10-01T09:00:00'),
		],
		now,
	);
	assert.deepEqual(
		groups.map((g) => [g.key, g.chats.map((x) => x.id)]),
		[
			['pinned', ['p']],
			['today', ['a']],
			['yesterday', ['b']],
			['older', ['o']],
		],
	);
});

test('a turn folds its steps into one box before the answer', () => {
	const out = blocks([
		{ id: '1', role: 'user', content: 'Status?' },
		{ id: '2', role: 'assistant', thinking: 'hm', calls: [{}] },
		{ id: '3', role: 'tool', tool: 'docker_list' },
		{ id: '4', role: 'note', note: 'approved' },
		{ id: '5', role: 'assistant', content: 'Läuft.' },
		{ id: '6', role: 'note', note: 'memory', content: 'x' },
	]);
	assert.deepEqual(
		out.map((b) => b.kind),
		['user', 'steps', 'answer', 'note'],
	);
	assert.equal(out[1].steps.length, 2);
	assert.equal(out[1].thinking, 'hm');
});

test('the policy in the UI decides like the backend', () => {
	const tool = (name, area, kind) => ({ name, area, kind });
	const std = { preset: 'standard' };
	assert.equal(toolMode(std, tool('docker_list', 'docker', 'read')), 'allow');
	assert.equal(toolMode(std, tool('file_write', 'files', 'create')), 'allow');
	assert.equal(toolMode(std, tool('vm_action', 'vms', 'change')), 'ask');
	assert.equal(toolMode({ preset: 'read' }, tool('vm_action', 'vms', 'change')), 'deny');
	assert.equal(toolMode({ preset: 'all' }, tool('vm_action', 'vms', 'change')), 'allow');
	const custom = { preset: 'all', areas: { vms: 'ask' }, tools: { vm_list: 'deny' } };
	const vms = [tool('vm_list', 'vms', 'read'), tool('vm_action', 'vms', 'change')];
	assert.equal(toolMode(custom, vms[1]), 'ask');
	assert.equal(areaMode(custom, 'vms', vms), 'mixed');
	assert.equal(areaMode({ preset: 'standard' }, 'vms', [vms[1]]), 'ask');
	assert.equal(presetOf(custom), 'custom');
	assert.equal(presetOf({ preset: 'read' }), 'read');
});

// 1.11 (TECHNIK 6.4): preset × dataRights × tool → mode, the same table
// as backend/internal/agent/policy_test.go reads.
test('calendar and contacts decide like the backend', () => {
	const matrix = JSON.parse(
		readFileSync(
			new URL('../../backend/internal/agent/testdata/data-rights.json', import.meta.url),
			'utf8',
		),
	);
	assert.equal(matrix.tools.length, 9);
	assert.ok(matrix.cases.length >= 20);
	for (const c of matrix.cases)
		for (const tool of matrix.tools)
			assert.equal(toolMode(c.policy, tool), c.modes[tool.name], `${c.name}: ${tool.name}`);
	assert.equal(presetDataMode('standard', 'change'), 'ask');
	assert.equal(presetDataMode('standard', 'create'), 'ask');
	assert.deepEqual(dataRight({ preset: 'read' }, 'contacts'), { read: 'allow', write: 'deny' });
	// A set right counts as touched: the quick choice shows "Eigene".
	assert.equal(
		presetOf({ preset: 'standard', dataRights: { calendar: { read: 'allow', write: 'ask' } } }),
		'custom',
	);
	assert.equal(presetOf({ preset: 'standard', dataRights: {} }), 'standard');
	assert.deepEqual(AREAS.slice(1, 4), ['files', 'calendar', 'contacts']);
});

test('levels, step details and the look', () => {
	assert.equal(levelLabel('medium', 'de'), 'Mittel');
	assert.equal(levelLabel('xhigh', 'en'), 'Extra high');
	assert.equal(levelLabel('turbo', 'de'), 'turbo');
	assert.equal(stepDetail({ root: 'share/Docs', path: 'NAS' }), 'Docs › NAS');
	// PLAN 68: the agent has no look of its own any more. A stored look of
	// an older version reads without error; its visual fields are dropped.
	const old = sanitizeLook({
		accent: 'aurora',
		style: 'clean',
		background: 'ocean',
		glass: true,
		glow: false,
		glassLevel: 400,
		typing: false,
		read: true,
	});
	for (const k of ['accent', 'style', 'background', 'glass', 'glow', 'glassLevel'])
		assert.ok(!(k in old) && !(k in accountLook(old)), k);
	assert.equal(old.typing, false);
	assert.equal(old.read, true);
	assert.equal(sanitizeLook(null).typing, true);
	const app = sanitizeLook({
		swipeLeft: 'menu',
		swipeRight: 'wat',
		glassLevel: 400,
		motion: 'off',
		rate: 9,
	});
	assert.equal(app.swipeLeft, 'menu');
	assert.equal(app.swipeRight, 'pin');
	assert.equal(app.motion, 'off');
	assert.equal(app.rate, 2);
	assert.ok(!('vibrate' in accountLook(app)) && !('fontSize' in accountLook(app)));
	assert.equal(accountLook(app).swipeLeft, 'menu');
});

test('the line under the composer only says what it knows (PLAN 68)', () => {
	const keys = (o) => whereLine(o).map((p) => p.key);
	const base = { cluster: false, web: false, ollama: '', here: 'nas' };
	assert.deepEqual(keys({ ...base, ollama: 'http://127.0.0.1:11434' }), [
		'agent.whereLocal',
		'agent.whereStays',
	]);
	assert.deepEqual(keys({ ...base, ollama: 'http://nas:11434' }), [
		'agent.whereLocal',
		'agent.whereStays',
	]);
	assert.deepEqual(whereLine({ ...base, ollama: 'http://192.168.1.5:11434' }), [
		{ key: 'agent.whereHost', params: { host: '192.168.1.5' } },
		{ key: 'agent.whereStays' },
	]);
	// Somewhere outside the home network: no promise about the network.
	assert.deepEqual(keys({ ...base, ollama: 'https://ollama.example.org' }), [
		'agent.whereHost',
		'agent.whereChats',
	]);
	// Unknown address (accounts that are no administrators), web search, cluster.
	assert.deepEqual(keys(base), ['agent.whereOwn', 'agent.whereChats']);
	assert.deepEqual(keys({ ...base, ollama: 'http://127.0.0.1:11434', web: true }), [
		'agent.whereLocal',
		'agent.whereWeb',
	]);
	assert.deepEqual(keys({ ...base, cluster: true, ollama: 'http://127.0.0.1:1' }), [
		'agent.whereCluster',
		'agent.whereChats',
	]);
});

test('a waiting step gets a plain title (PLAN 68)', () => {
	assert.deepEqual(
		approvalTitle({
			tool: 'vm_action',
			args: { name: 'HAOS', action: 'update' },
			summary: 'vm.update (name=HAOS)',
		}),
		{ key: 'vm.update', name: 'HAOS' },
	);
	// From the tool and its action when the summary names no card.
	assert.deepEqual(
		approvalTitle({
			tool: 'container_action',
			args: { container: 'web', action: 'restart' },
			summary: '',
		}),
		{ key: 'container.restart', name: 'web' },
	);
	assert.deepEqual(
		approvalTitle({
			tool: 'vm_action',
			args: { vm: 'Win', action: 'remove_with_disks' },
			summary: 'x',
		}),
		{ key: 'vm.purge', name: 'Win' },
	);
	assert.deepEqual(
		approvalTitle({
			tool: 'file_write',
			args: {},
			summary: 'file.write (path=NAS/Doku, x.docx, root=share/Documents)',
		}),
		{ key: 'file.write', name: 'Doku, x.docx' },
	);
	assert.deepEqual(
		approvalTitle({ tool: 'power_action', args: { action: 'reboot' }, summary: 'power.reboot' }),
		{
			key: 'power.reboot',
			name: '',
		},
	);
	// A rename names what is renamed, not where it goes.
	assert.deepEqual(
		approvalTitle({
			tool: 'file_rename',
			args: {},
			summary: 'file.rename (from=Scans/a.pdf, root=share/Documents, to=Archiv/Rechnung.pdf)',
		}),
		{ key: 'file.rename', name: 'a.pdf' },
	);
	assert.deepEqual(
		approvalTitle({
			tool: 'files_move',
			args: {},
			summary: 'files.move (count=3, root=share/Documents)',
		}),
		{ key: 'files.move', name: '' },
	);
	// Unknown: the page shows the summary.
	assert.equal(approvalTitle({ tool: 'code_run', args: {}, summary: 'print(1)' }), null);
});

test('the colours of the working line are "#rrggbb" or empty (owner, 16 Nov 2026)', () => {
	const d = sanitizeLook({});
	assert.equal(d.dotColor, '');
	assert.equal(d.textColor, '');
	const ok = sanitizeLook({ dotColor: '#3E63DD', textColor: '#30a46c' });
	assert.equal(ok.dotColor, '#3e63dd');
	assert.equal(ok.textColor, '#30a46c');
	for (const bad of ['red', '#fff', 'url(x)', 'var(--x)', '#12345g', 42, null])
		assert.equal(sanitizeLook({ dotColor: bad, textColor: bad }).dotColor, '', String(bad));
});

test('"Geplant" counts only active tasks (owner, 17 Nov 2026)', () => {
	// "es sind aber nur 2 aktiv … der Morgenbericht wird mitgezählt, obwohl
	// der Morgenbericht deaktiviert ist"
	const list = [
		{ kind: 'morning', every: 'weekdays', enabled: false },
		{ kind: '', every: 'daily', enabled: true },
		{ kind: 'reminder', every: 'weekly', enabled: true, lastRun: '2026-11-10T08:00:00Z' },
		{ kind: '', every: 'trigger', enabled: true },
		{ kind: '', every: 'daily', enabled: false },
		// A one-off task that ran is done; one that never ran still counts.
		{ kind: '', every: 'once', enabled: true, lastRun: '2026-11-16T08:00:00Z' },
		{ kind: 'reminder', every: 'once', enabled: true, lastRun: '0001-01-01T00:00:00Z' },
	];
	assert.equal(activeSchedules(list), 4);
	assert.equal(activeSchedules(list.slice(0, 3)), 2);
	assert.equal(activeSchedules([]), 0);
	assert.equal(activeSchedules(undefined), 0);
	// Every place that shows the number uses it.
	const read = (f) => readFileSync(new URL(`../src/${f}`, import.meta.url), 'utf8');
	assert.match(read('routes/agent/agent-chats.svelte.ts'), /plannedCount = activeSchedules\(/);
	assert.match(
		read('lib/components/agent/Planned.svelte'),
		/oncount\?\.\(activeSchedules\(list\)\)/,
	);
	assert.match(
		read('lib/components/agent/pages/HomePage.svelte'),
		/count: activeSchedules\(planned\)/,
	);
});

test('the size of the working dots: small, medium or large, medium by default (owner, 17 Nov 2026)', () => {
	// "Gut wäre, wenn ich die Größe der Punkte anpassen könnte … die sind für
	// mich viel zu groß": smaller by default than the 26 px before.
	assert.deepEqual(DOT_SIZES, { s: 14, m: 18, l: 26 });
	assert.equal(sanitizeLook({}).dotSize, 'm');
	assert.equal(sanitizeLook(null).dotSize, 'm');
	for (const z of ['s', 'm', 'l']) assert.equal(sanitizeLook({ dotSize: z }).dotSize, z);
	for (const bad of ['xl', 'M', 26, '', null, {}])
		assert.equal(sanitizeLook({ dotSize: bad }).dotSize, 'm', String(bad));
	// Kept for the account like the colours, as a flat value.
	assert.equal(accountLook(sanitizeLook({ dotSize: 's' })).dotSize, 's');
});

// 1.13: the rights per kind of action decide like the backend
// (backend/internal/agent/actions.go, the same table).
test('action rights decide like the backend', async () => {
	const { ACTION_KEYS, actionMode, nasAction, ownChoices, stricterAction, validAction } =
		await import('../src/lib/agent-actions.ts');
	const matrix = JSON.parse(
		readFileSync(
			new URL('../../backend/internal/agent/testdata/data-rights.json', import.meta.url),
			'utf8',
		),
	);
	assert.deepEqual(matrix.actions.keys, [...ACTION_KEYS]);
	assert.ok(matrix.actions.cases.length >= 12);
	for (const c of matrix.actions.cases)
		for (const key of ACTION_KEYS)
			assert.equal(actionMode(c.policy, c.own, key, c.read), c.modes[key], `${c.name}: ${key}`);
	assert.equal(stricterAction('allow', 'suggest'), 'suggest');
	assert.equal(stricterAction('ask', 'weird'), 'deny');
	assert.ok(!validAction('event.delete', 'allow'));
	assert.ok(!validAction('mail.read', 'ask'));
	assert.equal(nasAction({ preset: 'standard' }, 'event.create'), 'suggest');
	assert.deepEqual(ownChoices('event.create', 'allow'), ['', 'suggest', 'ask']);
	assert.deepEqual(ownChoices('event.create', 'suggest'), ['']);
	assert.deepEqual(ownChoices('mail.read', 'allow'), ['', 'deny']);
	// A set action right counts as touched: the quick choice shows "Eigene".
	assert.equal(presetOf({ preset: 'all', actionRights: { 'mail.send': 'allow' } }), 'custom');
});

test('drafts of the agent open in their program', async () => {
	const { parseDraftParam, draftHref, draftAppOf, draftList, DRAFT_TOOLS } =
		await import('../src/lib/agent/drafts.ts');
	assert.deepEqual(parseDraftParam('abc123:def456'), { chat: 'abc123', entry: 'def456' });
	for (const bad of ['', 'abc', ':x', 'a:b/c', '../x:y', 'a:b:c'])
		assert.equal(parseDraftParam(bad), null, bad);
	assert.equal(
		draftHref({ app: 'mail', kind: 'reply', fields: {} }, 'c1', 'e1'),
		'/mail/compose?agentDraft=c1%3Ae1',
	);
	assert.equal(
		draftHref(
			{ app: 'mail', kind: 'move', fields: { a: 'm1', f: 'INBOX', uids: [7], to: 'Archiv' } },
			'c',
			'e',
		),
		'/mail?a=m1&f=INBOX&m=7',
	);
	assert.equal(
		draftHref({ app: 'calendar', kind: 'event', fields: {} }, 'c', 'e'),
		'/calendar/event?agentDraft=c%3Ae',
	);
	assert.equal(
		draftHref({ app: 'contacts', kind: 'contact', fields: {} }, 'c', 'e'),
		'/contacts?agentDraft=c%3Ae',
	);
	assert.equal(
		draftHref({ app: 'tasks', kind: 'task', fields: {} }, 'c', 'e'),
		'/calendar?agentDraft=c%3Ae',
	);
	assert.equal(draftAppOf('tasks_complete'), 'tasks');
	assert.ok(DRAFT_TOOLS.has('mail_send') && !DRAFT_TOOLS.has('calendar_delete'));
	assert.deepEqual(draftList(['a@b', 3, ' ', 'c@d']), ['a@b', 'c@d']);
});

test('a contact draft fills the form of Kontakte', async () => {
	const { contactDraftForm } = await import('../src/lib/agent/drafts.ts');
	const blank = {
		first: '',
		last: '',
		org: 'Alt',
		phones: [],
		emails: [{ type: 'home', value: 'alt@example.com' }],
		addresses: [],
		birthdayDate: '',
		birthdayNoYear: false,
		notes: 'bleibt',
	};
	const f = contactDraftForm(blank, {
		name: 'Bea Maria Muster',
		emails: [{ type: 'work', value: 'bea@example.com' }, { value: ' ' }],
		phones: [{ value: '+49 171 1' }],
		addresses: [{ street: 'Weg 1', city: 'Köln' }],
		birthday: '--11-20',
	});
	assert.equal(f.first, 'Bea Maria');
	assert.equal(f.last, 'Muster');
	assert.deepEqual(f.emails, [{ type: 'work', value: 'bea@example.com' }]);
	assert.deepEqual(f.phones, [{ type: 'mobile', value: '+49 171 1' }]);
	assert.equal(f.addresses[0].city, 'Köln');
	assert.equal(f.birthdayDate, '2000-11-20');
	assert.ok(f.birthdayNoYear);
	// What the draft does not name stays.
	assert.equal(f.org, 'Alt');
	assert.equal(f.notes, 'bleibt');
	assert.equal(blank.first, '', 'the form given is not changed');
});

test('an event draft fills the event window (new and change)', async () => {
	const { eventDraftForm } = await import('../src/lib/agent/drafts.ts');
	const blank = {
		title: '',
		calendar: 'privat',
		allDay: false,
		startDate: '2026-11-18',
		startTime: '09:00',
		endDate: '2026-11-18',
		endTime: '10:00',
		repeat: { kind: 'none', interval: 1, end: 'never', until: '', count: 10 },
		alarms: [],
		location: '',
		notes: '',
	};
	// calendar_create: every field as calweb.View has it.
	const made = eventDraftForm(
		blank,
		{
			title: 'Zahnarzt\nDr. Weiß',
			start: '2026-11-20T08:30:00+01:00',
			end: '2026-11-20T09:15:00+01:00',
			allDay: false,
			timezone: 'Europe/Berlin',
			location: 'Hauptstraße 1',
			notes: 'Karte mitnehmen',
			calendar: 'arbeit',
			alarms: [30, 10, 30, 'x'],
			rrule: { freq: 'weekly', interval: 1, byDay: ['FR'] },
		},
		['privat', 'arbeit'],
	);
	assert.equal(made.form.title, 'Zahnarzt Dr. Weiß');
	assert.deepEqual(
		[made.form.startDate, made.form.startTime, made.form.endDate, made.form.endTime],
		['2026-11-20', '08:30', '2026-11-20', '09:15'],
	);
	assert.equal(made.form.calendar, 'arbeit');
	assert.deepEqual(made.form.alarms, [10, 30]);
	assert.equal(made.form.repeat.kind, 'weekly');
	assert.equal(made.rule, null);
	assert.equal(made.zone, 'Europe/Berlin');
	assert.equal(blank.title, '', 'the form given is not changed');
	// All day: the end is the day after the last day; a foreign calendar stays out.
	const day = eventDraftForm(
		blank,
		{ start: '2026-12-24', end: '2026-12-27', allDay: true, calendar: 'fremd' },
		['privat'],
	);
	assert.ok(day.form.allDay);
	assert.equal(day.form.endDate, '2026-12-26');
	assert.equal(day.form.calendar, 'privat');
	// A rule the form shows only as "custom" is kept as it came.
	const two = eventDraftForm(blank, { rrule: { freq: 'weekly', byDay: ['MO', 'WE'] } }, []);
	assert.equal(two.form.repeat.kind, 'custom');
	assert.deepEqual(two.rule, { freq: 'weekly', interval: 1, byDay: ['MO', 'WE'] });
	// calendar_update: only the start moves, the length stays; scope comes along.
	const moved = eventDraftForm(
		{ ...blank, endTime: '10:30' },
		{ cal: 'privat', id: 'x.ics', start: '2026-11-19T14:00', scope: 'following' },
		[],
	);
	assert.deepEqual(
		[moved.form.startDate, moved.form.startTime, moved.form.endDate, moved.form.endTime],
		['2026-11-19', '14:00', '2026-11-19', '15:30'],
	);
	assert.equal(moved.scope, 'following');
	assert.equal(moved.form.title, '', 'what the change does not name stays');
	// duration in minutes; the repetition removed; bad values left out.
	const long = eventDraftForm(
		{ ...blank, repeat: { ...blank.repeat, kind: 'daily' } },
		{ start: '2026-11-19T23:30', duration: 90, rrule: { freq: 'none' }, timezone: '../x' },
		[],
	);
	assert.deepEqual([long.form.endDate, long.form.endTime], ['2026-11-20', '01:00']);
	assert.equal(long.form.repeat.kind, 'none');
	assert.equal(long.zone, '');
	const bad = eventDraftForm(blank, { start: '2026-02-30T09:00', end: 'morgen' }, []);
	assert.deepEqual(bad.form, blank);
	// An end before the start: an hour after the start.
	const back = eventDraftForm(blank, { start: '2026-11-18T15:00', end: '2026-11-18T14:00' }, []);
	assert.equal(back.form.endTime, '16:00');
});

test('a reminder draft fills the reminder dialog', async () => {
	const { taskDraftForm } = await import('../src/lib/agent/drafts.ts');
	const blank = {
		title: 'Alt',
		list: 'erinnerungen--titanos',
		hasDue: false,
		date: '2026-11-18',
		hasTime: false,
		time: '09:00',
		priority: 0,
		alarm: false,
		notes: '',
	};
	const f = taskDraftForm(
		blank,
		{
			title: 'Reifen wechseln',
			due: '2026-11-21T10:30',
			priority: 1,
			list: 'einkauf',
			notes: 'Termin',
		},
		['erinnerungen--titanos', 'einkauf'],
	);
	assert.equal(f.title, 'Reifen wechseln');
	assert.ok(f.hasDue && f.hasTime);
	assert.deepEqual([f.date, f.time], ['2026-11-21', '10:30']);
	assert.equal(f.priority, 1);
	assert.equal(f.list, 'einkauf');
	const day = taskDraftForm(
		{ ...blank, hasDue: true, hasTime: true },
		{ due: '2026-11-22', list: 'x', priority: 12 },
		[],
	);
	assert.ok(day.hasDue && !day.hasTime);
	assert.equal(day.list, 'erinnerungen--titanos');
	assert.equal(day.priority, 0);
	assert.equal(day.title, 'Alt', 'what the draft does not name stays');
	assert.equal(taskDraftForm({ ...blank, hasDue: true }, { due: null }, []).hasDue, false);
});

test('1.14: built-in skills in the language of the page', () => {
	const built = {
		id: 'builtin-health',
		name: 'Health check',
		when: 'regular report',
		steps: '1. system_status',
		builtIn: true,
		updated: '',
		de: {
			name: 'Gesundheitscheck',
			when: 'regelmäßiger Bericht',
			steps: '1. system_status prüfen',
		},
	};
	assert.equal(skillIn(built, 'de').name, 'Gesundheitscheck');
	assert.equal(skillIn(built, 'de').steps, '1. system_status prüfen');
	assert.equal(skillIn(built, 'de').id, 'builtin-health');
	assert.equal(skillIn(built, 'en').name, 'Health check');
	const own = { id: 'x', name: 'Mein Ablauf', when: '', steps: '1.', updated: '' };
	assert.equal(skillIn(own, 'de'), own);
	assert.equal(skillIn({ ...built, de: undefined }, 'de').name, 'Health check');
});
