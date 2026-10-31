import { test } from 'node:test';
import assert from 'node:assert/strict';
import { temperatureSelection, withTemperatures } from '../src/lib/dashboard/temperatures.ts';

const sensor = { id: 'a'.repeat(32), chip: 'coretemp', label: 'Package id 0' };
test('temperature selection is separate from layout and survives disappearance and hiding', () => {
	const preferences = {
		version: 2,
		tiles: [{ id: 'vm:debian', x: 3, y: 4, w: 4, h: 4 }],
		futureSetting: true,
	};
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
	assert.deepEqual(
		temperatureSelection({
			temperatureSensors: [sensor, sensor, { ...sensor, id: '../../foo' }, null, 'broken'],
		}),
		[sensor],
	);
	assert.deepEqual(temperatureSelection(null), []);
	assert.equal(
		withTemperatures(null, true, [sensor]).version,
		undefined,
		'do not bypass accelerator layout migration',
	);
});

test('temperature tiles show their own sensors or the account selection', async () => {
	const { tileTemperatures, cpuTemperature } = await import('../src/lib/dashboard/temperatures.ts');
	const nvme = {
		id: 'b'.repeat(32),
		chip: 'nvme',
		label: 'Composite',
		device: 'nvme0',
		celsius: 41,
		criticalC: 80,
	};
	const cpu = { ...sensor, device: 'coretemp.0', celsius: 52, criticalC: 100 };
	const preferences = { temperatureSensors: [sensor] };
	assert.deepEqual(
		tileTemperatures(preferences, null, [cpu, nvme]).map((s) => s.id),
		[sensor.id],
	);
	assert.deepEqual(
		tileTemperatures(preferences, [nvme.id], [cpu, nvme]).map((s) => s.label),
		['NVMe'], // plain names for the sensors everyone has
	);
	const gone = tileTemperatures(preferences, [sensor.id, 'c'.repeat(32)], []);
	assert.equal(gone[0].label, 'Package id 0', 'last known name stays');
	assert.equal(gone[0].sensor, undefined);
	assert.equal(gone[1].label, 'c'.repeat(32));
	assert.deepEqual(tileTemperatures(preferences, [], [cpu]), []);

	assert.equal(cpuTemperature([nvme, cpu])?.id, cpu.id);
	const amd = { ...nvme, id: 'd'.repeat(32), chip: 'k10temp', label: 'Tctl' };
	assert.equal(cpuTemperature([nvme, amd])?.id, amd.id);
	assert.equal(cpuTemperature([nvme, cpu], nvme.id)?.id, nvme.id);
	assert.equal(
		cpuTemperature([cpu], nvme.id),
		undefined,
		'a chosen sensor that is gone shows nothing else',
	);
	assert.equal(cpuTemperature(undefined), undefined);
});

test('turning the temperature tile off also removes its copies', () => {
	const preferences = { tiles: [{ id: 'temperatures@1a2b3c4d', w: 4, h: 2 }] };
	assert.equal(withTemperatures(preferences, true, [sensor]).tiles.length, 1);
	assert.deepEqual(withTemperatures(preferences, false, [sensor]).tiles, []);
});
