import { test } from 'node:test';
import assert from 'node:assert/strict';
import { temperatureSelection, withTemperatures } from '../src/lib/dashboard/temperatures.ts';

const sensor = { id: 'a'.repeat(32), chip: 'coretemp', label: 'Package id 0' };
test('temperature selection is separate from layout and survives disappearance and hiding', () => {
	const preferences = { version: 2, tiles: [{ id: 'vm:debian', x: 3, y: 4, w: 4, h: 4 }], futureSetting: true };
	const shown = withTemperatures(preferences, true, [sensor]);
	assert.deepEqual(shown.tiles[0], preferences.tiles[0]);
	assert.equal(shown.tiles[1].id, 'temperatures');
	assert.equal(shown.futureSetting, true);
	const hidden = withTemperatures(shown, false, temperatureSelection(shown));
	assert.deepEqual(hidden.tiles, preferences.tiles);
	assert.deepEqual(temperatureSelection(hidden), [sensor]);
	assert.equal(withTemperatures(shown, true, [sensor]).tiles.length, 2);
});
test('temperature selection rejects malformed IDs without replacing missing sensors', () => {
	assert.deepEqual(temperatureSelection({ temperatureSensors: [sensor, sensor, { ...sensor, id: '../../foo' }, null, 'broken'] }), [sensor]);
	assert.deepEqual(temperatureSelection(null), []);
	assert.equal(withTemperatures(null, true, [sensor]).version, undefined, 'do not bypass accelerator layout migration');
});
