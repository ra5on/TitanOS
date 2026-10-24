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
