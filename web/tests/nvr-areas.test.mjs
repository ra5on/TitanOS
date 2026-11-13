import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	closable,
	coordinate,
	nudge,
	point,
	polygonArea,
	roomFor,
	zoneId,
	MAX_ZONES,
} from '../src/lib/nvr/areas.ts';

// PLAN 88: what the zone editor keeps of a point: 0 to 1, three places –
// as the NAS checks it (areas.go) and Frigate reads it as relative.
test('points stay inside the picture with three places', () => {
	assert.equal(coordinate(0.12345), 0.123);
	assert.equal(coordinate(-0.2), 0);
	assert.equal(coordinate(1.4), 1);
	assert.deepEqual(point(0.9996, 0.00001), [1, 0]);
	assert.deepEqual(nudge([0.5, 0.5], 'ArrowLeft', 0.01), [0.49, 0.5]);
	assert.deepEqual(nudge([0.995, 0], 'ArrowRight', 0.05), [1, 0]);
	assert.deepEqual(nudge([0.5, 0.005], 'ArrowUp', 0.01), [0.5, 0]);
	assert.equal(nudge([0.5, 0.5], 'Enter', 0.01), null);
});

test('an area needs three points and a surface', () => {
	assert.equal(
		closable([
			[0, 0],
			[1, 0],
		]),
		false,
	);
	assert.equal(
		closable([
			[0, 0],
			[0.5, 0.5],
			[1, 1],
		]),
		false,
	);
	assert.equal(
		closable([
			[0, 0],
			[1, 0],
			[1, 1],
		]),
		true,
	);
	assert.equal(
		polygonArea([
			[0, 0],
			[1, 0],
			[1, 1],
			[0, 1],
		]),
		1,
	);
});

test('a zone gets an ID like the NAS makes it, never one that is taken', () => {
	assert.equal(zoneId('Tor & Weg', []), 'tor_weg');
	assert.equal(zoneId('Haustür vorn', []), 'haustuer_vorn');
	assert.equal(zoneId('Zone 1', ['zone_1']), 'zone_1_2');
	assert.equal(zoneId('1. Beet', []), 'zone_1_beet');
	assert.equal(zoneId('', []), 'zone');
	assert.match(
		zoneId('Ein sehr langer Name für eine Zone im Garten', []),
		/^[a-z][a-z0-9_]{0,31}$/,
	);
	assert.equal(zoneId('Kamera sub', []).endsWith('_sub'), false);
	const zones = Array.from({ length: MAX_ZONES }, (_, i) => ({ name: `Z${i}`, points: [] }));
	assert.equal(roomFor('zone', zones, { motion: [], objects: [] }), false);
	assert.equal(roomFor('motion', zones, { motion: [], objects: [] }), true);
});
