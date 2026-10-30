import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tileOptions } from '../src/lib/dashboard/options.ts';
import { sanitizeLayout, TILE_IDS, ADMIN_TILES } from '../src/lib/dashboard/tiles.ts';
import {
	DEFAULT_NETBIRD_FIELDS,
	netbirdFields,
	visiblePeers,
} from '../src/lib/dashboard/netbird.ts';

test('the NetBird tile is optional, admin-only and survives the layout', () => {
	assert.ok(TILE_IDS.includes('netbird'));
	assert.ok(ADMIN_TILES.includes('netbird'));
	const tiles = sanitizeLayout({
		tiles: [
			{ id: 'netbird', w: 6, h: 3 },
			{ id: 'netbird@1a2b3c4d', w: 6, h: 3 },
		],
	});
	assert.deepEqual(
		tiles.map((t) => t.id),
		['netbird', 'netbird@1a2b3c4d'],
	);
});

test('field choice keeps order, drops unknown names and defaults when missing', () => {
	assert.deepEqual(tileOptions(null, 'netbird').netbirdFields, DEFAULT_NETBIRD_FIELDS);
	assert.deepEqual(netbirdFields(['networks', 'ipv4', 'networks', 'os', 5]), ['networks', 'ipv4']);
	assert.deepEqual(tileOptions({ netbird: { netbirdFields: [] } }, 'netbird').netbirdFields, []);
	// Fields of one peer tile never leak into the other.
	assert.deepEqual(
		tileOptions({ netbird: { tailscaleFields: ['routes'] } }, 'netbird').netbirdFields,
		DEFAULT_NETBIRD_FIELDS,
	);
});

test('peer selection uses public keys and treats unknown state as not connected', () => {
	const peer = (id, online) => ({ id, name: id, online, ipv4: '', ipv6: '', networks: [] });
	const all = [peer('a+/=', true), peer('b', false), peer('c', null)];
	assert.deepEqual(
		visiblePeers(all, null, false).map((p) => p.id),
		['a+/=', 'b', 'c'],
	);
	assert.deepEqual(
		visiblePeers(all, ['c', 'b'], false).map((p) => p.id),
		['b', 'c'],
	);
	assert.deepEqual(
		visiblePeers(all, null, true).map((p) => p.id),
		['a+/='],
	);
	assert.deepEqual(visiblePeers(all, [], false), []);
});
