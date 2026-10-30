import { test, mock } from 'node:test';
import assert from 'node:assert/strict';
import { poll } from '../src/lib/poll.ts';

// Lets pending promise callbacks run.
const settle = async () => {
	for (let i = 0; i < 5; i++) await Promise.resolve();
};

test('poll: runs right away, then once per interval, and stops', async () => {
	mock.timers.enable({ apis: ['setTimeout'] });
	try {
		let calls = 0;
		const stop = poll(() => calls++, { interval: 1000 });
		await settle();
		assert.equal(calls, 1);
		mock.timers.tick(1000);
		await settle();
		assert.equal(calls, 2);
		mock.timers.tick(1000);
		await settle();
		assert.equal(calls, 3);
		stop();
		mock.timers.tick(5000);
		await settle();
		assert.equal(calls, 3);
	} finally {
		mock.timers.reset();
	}
});

test('poll: runs never overlap, a slow answer delays the next run', async () => {
	mock.timers.enable({ apis: ['setTimeout'] });
	try {
		let calls = 0;
		let finish = () => {};
		const stop = poll(
			() => {
				calls++;
				return new Promise((resolve) => (finish = resolve));
			},
			{ interval: 100 },
		);
		for (let i = 0; i < 10; i++) {
			mock.timers.tick(100);
			await settle();
		}
		assert.equal(calls, 1, 'no second run while the first is under way');
		finish();
		await settle();
		mock.timers.tick(100);
		await settle();
		assert.equal(calls, 2);
		stop();
	} finally {
		mock.timers.reset();
	}
});

test('poll: now() during a run queues exactly one more run', async () => {
	let calls = 0;
	let finish = () => {};
	const stop = poll(
		() => {
			calls++;
			return new Promise((resolve) => (finish = resolve));
		},
		{ interval: 60_000 },
	);
	const a = stop.now();
	const b = stop.now();
	assert.equal(calls, 1);
	finish();
	await settle();
	assert.equal(calls, 2, 'one follow-up run for both requests');
	finish();
	await Promise.all([a, b]);
	assert.equal(calls, 2);
	stop();
});

test('poll: skip and immediate are honoured, errors go to onError', async () => {
	mock.timers.enable({ apis: ['setTimeout'] });
	try {
		let calls = 0;
		let busy = true;
		const errors = [];
		const stop = poll(
			() => {
				calls++;
				throw new Error('boom');
			},
			{ interval: 10, immediate: false, skip: () => busy, onError: (e) => errors.push(e) },
		);
		await settle();
		assert.equal(calls, 0);
		mock.timers.tick(10);
		await settle();
		assert.equal(calls, 0, 'skipped while busy');
		busy = false;
		mock.timers.tick(10);
		await settle();
		assert.equal(calls, 1);
		assert.equal(errors.length, 1);
		stop();
	} finally {
		mock.timers.reset();
	}
});

test('poll: the interval function is asked before each wait', async () => {
	mock.timers.enable({ apis: ['setTimeout'] });
	try {
		let calls = 0;
		let fast = false;
		const stop = poll(() => calls++, { interval: () => (fast ? 10 : 1000) });
		await settle();
		fast = true;
		mock.timers.tick(1000);
		await settle();
		assert.equal(calls, 2);
		mock.timers.tick(10);
		await settle();
		assert.equal(calls, 3);
		stop();
	} finally {
		mock.timers.reset();
	}
});

test('poll: stopping aborts the signal of a run under way', async () => {
	let signal;
	const stop = poll(
		(s) => {
			signal = s;
			return new Promise(() => {});
		},
		{ interval: 1000 },
	);
	assert.equal(signal.aborted, false);
	stop();
	assert.equal(signal.aborted, true);
});
