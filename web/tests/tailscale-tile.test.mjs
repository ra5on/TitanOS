import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tileOptions } from '../src/lib/dashboard/options.ts';
import { sanitizeLayout, TILE_IDS, ADMIN_TILES } from '../src/lib/dashboard/tiles.ts';
import {
	DEFAULT_TAILSCALE_FIELDS,
	tailscaleFields,
	visibleDevices,
} from '../src/lib/dashboard/tailscale.ts';

test('the Tailscale tile is optional, admin-only and survives the layout', () => {
	assert.ok(TILE_IDS.includes('tailscale'));
	assert.ok(ADMIN_TILES.includes('tailscale'));
	const tiles = sanitizeLayout({ tiles: [{ id: 'tailscale', w: 6, h: 3 }, { id: 'tailscale@1a2b3c4d', w: 6, h: 3 }] });
	assert.deepEqual(tiles.map((t) => t.id), ['tailscale', 'tailscale@1a2b3c4d']);
});

test('field choice keeps order, drops unknown names and defaults when missing', () => {
	assert.deepEqual(tileOptions(null, 'tailscale').tailscaleFields, DEFAULT_TAILSCALE_FIELDS);
	assert.deepEqual(tailscaleFields(['routes', 'ipv4', 'routes', 'secret', 5]), ['routes', 'ipv4']);
	assert.deepEqual(tileOptions({ tailscale: { tailscaleFields: [] } }, 'tailscale').tailscaleFields, []);
	// Older layouts without the field keep working for the other tiles.
	assert.deepEqual(tileOptions({ system: { systemFields: ['bios'] } }, 'system').systemFields, ['bios']);
});

test('device selection uses stable IDs and treats unknown online state as not online', () => {
	const device = (id, online) => ({ id, name: id, online, ipv4: [], ipv6: [], routes: [] });
	const all = [device('nA', true), device('nB', false), device('nC', null)];
	assert.deepEqual(visibleDevices(all, null, false).map((d) => d.id), ['nA', 'nB', 'nC']);
	assert.deepEqual(visibleDevices(all, ['nC', 'nB'], false).map((d) => d.id), ['nB', 'nC']);
	assert.deepEqual(visibleDevices(all, null, true).map((d) => d.id), ['nA']);
	assert.deepEqual(visibleDevices(all, [], false), []);
});
