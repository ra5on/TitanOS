import test from 'node:test';
import assert from 'node:assert/strict';
import {
	areaMode,
	blocks,
	groupChats,
	levelLabel,
	parseEvent,
	presetOf,
	sanitizeLook,
	accountLook,
	stepDetail,
	toolMode,
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

test('levels, step details and the look', () => {
	assert.equal(levelLabel('medium', 'de'), 'Mittel');
	assert.equal(levelLabel('xhigh', 'en'), 'Extra high');
	assert.equal(levelLabel('turbo', 'de'), 'turbo');
	assert.equal(stepDetail({ root: 'share/Docs', path: 'NAS' }), 'Docs › NAS');
	assert.deepEqual(sanitizeLook({ accent: 'aurora', style: 'nope', glow: false }).style, 'bubbles');
	assert.equal(sanitizeLook({ accent: 'aurora', glow: false }).glow, false);
	// PLAN 53: the agent has its own look by default and the app settings.
	assert.equal(sanitizeLook(null).accent, 'aurora');
	const app = sanitizeLook({
		swipeLeft: 'menu',
		swipeRight: 'wat',
		glassLevel: 400,
		motion: 'off',
		rate: 9,
	});
	assert.equal(app.swipeLeft, 'menu');
	assert.equal(app.swipeRight, 'pin');
	assert.equal(app.glassLevel, 100);
	assert.equal(app.motion, 'off');
	assert.equal(app.rate, 2);
	assert.ok(!('vibrate' in accountLook(app)) && !('fontSize' in accountLook(app)));
	assert.equal(accountLook(app).swipeLeft, 'menu');
});
