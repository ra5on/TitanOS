import { test } from 'node:test';
import assert from 'node:assert/strict';
import { latestFor, currentStep, downloadPercent } from '../src/lib/plugin-progress.ts';
import { taskItems, needsAttention, isActive } from '../src/lib/operations/tasks.ts';
const op = (over) => ({
	id: 'a',
	plugin: 'gpu',
	action: 'install',
	state: 'running',
	steps: [],
	bytes: 0,
	total: 0,
	speedBytesPerSecond: 0,
	started: '2026-10-27T10:00:00Z',
	...over,
});
test('the running operation of a plugin wins over older finished ones', () => {
	const list = [
		op({ id: 'old', state: 'failed' }),
		op({ id: 'run' }),
		op({ id: 'other', plugin: 'nic', state: 'done' }),
	];
	assert.equal(latestFor(list, 'gpu').id, 'run');
	assert.equal(
		latestFor([op({ id: 'x', state: 'done' }), op({ id: 'y', state: 'failed' })], 'gpu').id,
		'y',
	);
	assert.equal(latestFor(list, 'none'), undefined);
});
test('download percentage only while the download runs with a known size', () => {
	const steps = [
		{ id: 'signature', state: 'done' },
		{ id: 'download', state: 'running' },
	];
	assert.equal(downloadPercent(op({ steps, bytes: 25, total: 100 })), 25);
	assert.equal(downloadPercent(op({ steps, bytes: 25, total: 0 })), undefined);
	assert.equal(
		downloadPercent(op({ steps: [{ id: 'download', state: 'done' }], bytes: 100, total: 100 })),
		undefined,
	);
});
test('a failed middle step is the one shown in the jobs list with its error', () => {
	const failed = op({
		id: 'f',
		title: 'NVIDIA',
		state: 'failed',
		error: 'boom',
		finished: '2026-10-27T10:01:00Z',
		steps: [
			{ id: 'checksum', state: 'done' },
			{ id: 'services', state: 'failed', error: 'unit failed' },
			{ id: 'health', state: 'pending' },
		],
	});
	assert.equal(currentStep(failed).id, 'services');
	const [task] = taskItems({ transfers: [], plugins: [failed] });
	assert.equal(task.kind, 'plugin');
	assert.equal(task.name, 'NVIDIA');
	assert.equal(task.state, 'failed');
	assert.equal(task.detail, 'unit failed');
	assert.equal(task.stepKey, 'plugins.step.services');
	assert.equal(task.at, '2026-10-27T10:01:00Z');
	assert.ok(needsAttention(task));
	const [running] = taskItems({
		transfers: [],
		plugins: [op({ steps: [{ id: 'download', state: 'running' }], bytes: 1, total: 4 })],
	});
	assert.ok(isActive(running));
	assert.equal(running.progress, 25);
	assert.equal(running.stepKey, 'plugins.step.download');
});
