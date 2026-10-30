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
	const { tileStyle } = await import('../src/lib/dashboard/options.ts');
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
	assert.ok(!tileStyle(custom, id).includes('56px'));
	assert.ok(!tileStyle(custom, id).includes('display:none'));
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

test('independent lanes follow reading order without shared-row gaps or shortest-column reordering',async()=>{
 const {positions}=await import('../src/lib/dashboard/positions.ts');
 const items=['system','docker','cpu','vms','memory','volumes'].map(id=>({id}));
 const heights={system:600,docker:150,cpu:200,vms:100,memory:100,volumes:100};
 const result=positions(items,2,heights);
 assert.equal(result.rects.cpu.top,612);
 assert.equal(result.rects.vms.top,162);
 assert.equal(result.rects.volumes.top,274);
 assert.equal(result.rects.cpu.column,0);
 assert.deepEqual(items.map(p=>p.id),Object.keys(result.rects));
 const mobile=positions(items,1,heights,false);
 assert.equal(mobile.rects.docker.top,612);
 assert.equal(mobile.rects.cpu.top,774);
});
test('explicit columns, intentional offsets and spanning tiles retain non-overlapping positions',async()=>{
 const {positions}=await import('../src/lib/dashboard/positions.ts');
 const items=[{id:'a',column:1},{id:'b',column:0,offset:24},{id:'c',span:2,column:0},{id:'d',column:1,offset:16}];
 const heights={a:100,b:200,c:50,d:80};
 const r=positions(items,2,heights).rects;
 assert.equal(r.b.top,24);
 assert.equal(r.c.top,236);
 assert.equal(r.d.top,314);
 const small=positions(items,1,heights,false).rects;
 assert.equal(small.a.column,0);
 assert.equal(small.b.top,112);
 assert.equal(small.c.span,1);
 const sanitized=sanitizeLayout({tiles:[{id:'cpu',column:50,offset:-4,locked:true,height:Infinity}]})[0];
 assert.equal(sanitized.column,3);assert.equal(sanitized.offset,0);assert.equal(sanitized.locked,true);assert.equal(sanitized.height,80);
});
test('dropping a tile keeps tiles above the drop point in their lane', async () => {
	const { positions, dropLayout } = await import('../src/lib/dashboard/positions.ts');
	const heights = { A: 300, B: 100, C: 300, D: 100, E: 300, F: 100 };
	const items = ['A', 'B', 'C', 'D', 'E', 'F'].map((id) => ({ id, span: 1 }));
	const { rects } = positions(items, 2, heights);
	assert.equal(rects.F.top, 224);
	// A dragged into the right lane below F: F must not jump under A.
	const layout = dropLayout(items, 'A', rects, { column: 1, top: 340 }, 2, true);
	const after = positions(layout, 2, heights).rects;
	assert.equal(after.F.top, 224);
	assert.equal(after.D.top, 112);
	assert.equal(after.A.column, 1);
	assert.equal(after.A.top, 340);
	// Dropped between D and F: F makes room below A, D stays.
	const between = positions(
		dropLayout(items, 'A', rects, { column: 1, top: 200 }, 2, true),
		2,
		heights,
	).rects;
	assert.equal(between.D.top, 112);
	assert.equal(between.A.top, 224);
	assert.equal(between.F.top, 536);
	// With fewer visible columns only the reading order changes.
	const narrow = dropLayout(items, 'F', rects, { column: 0, top: 0 }, 2, false);
	assert.deepEqual(
		narrow.map((p) => p.id),
		['F', 'A', 'B', 'C', 'D', 'E'],
	);
});

test('fewer visible columns read the wished columns top to bottom, one after the other', async () => {
	const { positions, columnOrder, dropLayout } = await import('../src/lib/dashboard/positions.ts');
	// Wished: two columns, A C E left and B D F right.
	const items = ['A', 'B', 'C', 'D', 'E', 'F'].map((id) => ({ id }));
	const narrow = columnOrder(items, 2);
	assert.deepEqual(
		narrow.map((p) => p.id),
		['A', 'C', 'E', 'B', 'D', 'F'],
	);
	// Explicit columns and offsets decide, not the saved order.
	const placed = [
		{ id: 'a', column: 1 },
		{ id: 'b', column: 0, offset: 24 },
		{ id: 'c', column: 1 },
	];
	assert.deepEqual(
		columnOrder(placed, 2).map((p) => [p.id, p.column, p.offset]),
		[
			['b', 0, 24],
			['a', 1, undefined],
			['c', 1, undefined],
		],
	);
	// Dropping F on top of the single column puts it into the first wished column.
	const heights = Object.fromEntries(items.map((p) => [p.id, 100]));
	const { rects } = positions(narrow, 1, heights, false);
	const dropped = dropLayout(narrow, 'F', rects, { column: 0, top: 0 }, 1, false);
	assert.deepEqual(
		columnOrder(dropped, 2).map((p) => p.id),
		['F', 'A', 'C', 'E', 'B', 'D'],
	);
	// Dropped behind C it stays there in the wide layout as well.
	const behind = dropLayout(narrow, 'F', rects, { column: 0, top: 150 }, 1, false);
	assert.deepEqual(
		columnOrder(behind, 2).map((p) => p.id),
		['A', 'C', 'F', 'E', 'B', 'D'],
	);
	assert.equal(positions(behind, 2, heights).rects.F.column, 0);
});
