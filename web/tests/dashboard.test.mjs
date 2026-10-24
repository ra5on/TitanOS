import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sanitizeLayout, defaultLayout } from '../src/lib/dashboard/tiles.ts';
import { tileOptions } from '../src/lib/dashboard/options.ts';
test('compact layouts retain chosen dimensions, with bounds for malformed preferences', () => {
	const tiles = [
		{ id: 'vm:home-assistant', w: 2, h: 2, x: 10, y: 20 },
		{ id: 'network', w: 3, h: 2, x: 0, y: 0 },
	];
	assert.deepEqual(sanitizeLayout({ tiles }), tiles);
	assert.equal(sanitizeLayout({ tiles: [{ id: 'cpu', w: Infinity, h: -1 }] })[0].h, 2);
	assert.ok(defaultLayout().some((t) => t.id === 'cpu'));
	assert.ok(defaultLayout().some((t) => t.id === 'memory'));
});
test('tile options retain deliberate hiding and reject invalid interface paths', () => {
	const value = tileOptions(
		{ network: { ipv6: false, details: false, rates: false, interface: 'br0' } },
		'network',
	);
	assert.equal(value.ipv6, false);
	assert.equal(value.interface, 'br0');
	assert.equal(value.rates, false);
	assert.equal(tileOptions({ network: { interface: '../../sys' } }, 'network').interface, '');
	assert.equal(tileOptions({ network: { ipv6: 'false' } }, 'network').ipv6, true);
});

test('independent instances preserve their source, filters and appearance safely', async () => {
	const { sourceId, workload, defaultWithHardware } = await import('../src/lib/dashboard/tiles.ts');
	const { tileStyle, transferStyle } = await import('../src/lib/dashboard/options.ts');
	const id = 'network@1a2b3c4d';
	assert.equal(sourceId(id), 'network');
	assert.deepEqual(workload('vm:my-server@1a2b3c4d'), { kind: 'vm', name: 'my-server' });
	assert.deepEqual(workload('app:photos'), { kind: 'app', name: 'photos' });
	assert.equal(
		sanitizeLayout({
			tiles: [
				{ id, w: 3, h: 2 },
				{ id: 'network@bad', w: 3, h: 2 },
			],
		}).length,
		1,
	);
	const custom = tileOptions(
		{
			[id]: {
				interface: 'br0',
				valueSize: 56,
				titleColor: '#123456',
				background: 'red;display:none',
				hiddenFields: ['driver', '<bad>'],
				items: [],
				parts: ['details', 'value', 'details'],
			},
		},
		id,
	);
	assert.equal(custom.valueSize, 56);
	assert.equal(custom.background, '');
	assert.deepEqual(custom.items, []);
	assert.deepEqual(custom.hiddenFields, ['driver']);
	assert.deepEqual(custom.parts.slice(0, 2), ['details', 'value']);
	assert.ok(tileStyle(custom).includes('--tile-value-size:56px'));
	assert.ok(!tileStyle(custom).includes('display:none'));
	const target = tileOptions(
		{ network: { interface: 'eth0', title: 'My interface', items: ['a'] } },
		'network',
	);
	const copy = transferStyle(custom, target);
	assert.equal(copy.valueSize, 56);
	assert.equal(copy.interface, 'eth0');
	assert.equal(copy.title, 'My interface');
	assert.deepEqual(copy.items, ['a']);
	const hw = defaultWithHardware(['igpu', 'npu']);
	assert.equal(hw.find((p) => p.id === 'igpu').w, 3);
	assert.equal(hw.find((p) => p.id === 'npu').x, 3);
});

test('flow migration reads positions once and never reorders saved reading order on resize', async () => {
	const { flowLayout, visibleColumns, legacyPositions, columnCount, columnWidth } =
		await import('../src/lib/dashboard/flow.ts');
	const old = {
		version: 4,
		tiles: [
			{ id: 'vms', x: 6, y: 2, w: 6, h: 3 },
			{ id: 'cpu', x: 0, y: 0, w: 3, h: 2 },
			{ id: 'system', x: 6, y: 0, w: 6, h: 3 },
		],
	};
	const migrated = flowLayout(old);
	assert.deepEqual(
		migrated.map((p) => p.id),
		['cpu', 'system', 'vms'],
	);
	const saved = { version: 5, tiles: [migrated[2], migrated[0], migrated[1]] };
	assert.deepEqual(
		flowLayout(saved).map((p) => p.id),
		['vms', 'cpu', 'system'],
	);
	assert.equal(visibleColumns(1600, 4, 350), 4);
	assert.equal(visibleColumns(740, 4, 350), 2);
	assert.equal(visibleColumns(320, 4, 350), 1);
	const roundtrip = legacyPositions(saved.tiles, 3);
	assert.deepEqual(
		flowLayout({ version: 5, tiles: roundtrip }).map((p) => p.id),
		['vms', 'cpu', 'system'],
	);
	assert.equal(columnCount(Infinity), 2);
	assert.equal(columnWidth(NaN), 350);
});
test('system information selection keeps deliberate empty selection and ordered unique fields', () => {
	const settings = tileOptions(
		{
			system: {
				systemFields: ['bios', 'hostname', 'bios', 'secret'],
				showLabels: false,
				systemLayout: 'stacked',
			},
		},
		'system',
	);
	assert.deepEqual(settings.systemFields, ['bios', 'hostname']);
	assert.equal(settings.showLabels, false);
	assert.equal(settings.systemLayout, 'stacked');
	assert.deepEqual(tileOptions({ system: { systemFields: [] } }, 'system').systemFields, []);
});
