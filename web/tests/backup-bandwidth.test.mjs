import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	bandwidthForm,
	bandwidthPayload,
	bandwidthParts,
	maxBandwidth,
} from '../src/lib/backup-bandwidth.ts';
test('a job without a limit sends none and keeps the field out', () => {
	const form = bandwidthForm(undefined);
	assert.equal(form.on, false);
	assert.equal(bandwidthPayload(form), undefined);
	assert.equal(bandwidthPayload({ ...form, on: true, limit: '', window: false }), undefined);
	assert.equal(bandwidthParts(undefined), null);
});
test('limits round trip with a window across midnight', () => {
	const stored = { limit: 10, windowFrom: '22:00', windowTo: '06:00', windowLimit: 0 };
	const form = bandwidthForm(stored);
	assert.deepEqual(
		[form.on, form.window, form.windowFrom, form.windowTo],
		[true, true, '22:00', '06:00'],
	);
	assert.deepEqual(bandwidthPayload(form), stored);
	assert.deepEqual(bandwidthParts(stored), {
		limit: 10,
		window: { from: '22:00', to: '06:00', limit: 0 },
	});
	// Only working hours limited.
	assert.deepEqual(
		bandwidthPayload({
			...form,
			limit: 0,
			windowFrom: '08:00',
			windowTo: '18:00',
			windowLimit: '5',
		}),
		{ limit: 0, windowFrom: '08:00', windowTo: '18:00', windowLimit: 5 },
	);
});
test('typed values are bounded whole MiB/s', () => {
	const form = { ...bandwidthForm({ limit: 1 }), window: false };
	assert.deepEqual(bandwidthPayload({ ...form, limit: '2.6' }), { limit: 3 });
	assert.deepEqual(bandwidthPayload({ ...form, limit: 99999 }), { limit: maxBandwidth });
	assert.equal(bandwidthPayload({ ...form, limit: -4 }), undefined);
	assert.deepEqual(bandwidthParts({ limit: 7 }), { limit: 7 });
});
