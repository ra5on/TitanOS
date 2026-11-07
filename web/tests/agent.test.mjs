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
	approvalTitle,
	whereLine,
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
