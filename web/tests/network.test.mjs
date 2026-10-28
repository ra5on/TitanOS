import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import {
	blankBond,
	blankNetwork,
	bondCandidates,
	bondUsers,
	liveState,
	looksLikeIP,
	networkPayload,
	parseDns,
	portUsers,
} from '../src/lib/network.ts';

const net = (id, name, parent, extra = {}) => ({
	id,
	name,
	purpose: 'custom',
	parent,
	vlan: 0,
	bridge: false,
	management: false,
	ipv4: 'dhcp',
	address: '',
	gateway: '',
	ipv6: 'auto',
	address6: '',
	gateway6: '',
	dns: [],
	routes: [],
	...extra,
});
const config = () => ({
	hostname: 'nas',
	ports: [],
	bondMode: '',
	vlan: 0,
	bridge: false,
	dhcp: true,
	address: '',
	gateway: '',
	dns: [],
	bonds: [{ id: 'uplink', ports: ['enp2s0', 'enp3s0'], mode: 'active-backup' }],
	networks: [
		net('admin', 'LAN', 'enp1s0', { management: true, bridge: true }),
		net('storage', 'Speicher', 'bond:uplink', { vlan: 20 }),
	],
});
const ports = ['enp1s0', 'enp2s0', 'enp3s0', 'enp4s0'].map((name) => ({
	name,
	mac: '',
	carrier: true,
	speedMbps: 1000,
}));

test('name servers are split at spaces, commas and semicolons', () => {
	assert.deepEqual(parseDns(' 1.1.1.1, 9.9.9.9;fd00::1\n1.1.1.1 '), [
		'1.1.1.1',
		'9.9.9.9',
		'fd00::1',
	]);
	assert.deepEqual(parseDns(''), []);
	assert.ok(looksLikeIP('192.168.1.1') && looksLikeIP('fd00::1') && !looksLikeIP('dns.example'));
	assert.ok(!looksLikeIP('256.1.1.1'));
});

test('the payload sends numbers and clears unused addresses', () => {
	const cfg = config();
	Object.assign(cfg.networks[1], {
		vlan: '20',
		ipv4: 'dhcp',
		address: '10.0.0.2/24',
		dns: ['1.1.1.1 9.9.9.9'],
	});
	cfg.networks[1].routes = [{ destination: '10.1.0.0/16', gateway: '10.0.0.1', metric: '50' }];
	const out = networkPayload(cfg);
	assert.equal(out.networks[1].vlan, 20);
	assert.equal(out.networks[1].address, '');
	assert.deepEqual(out.networks[1].dns, ['1.1.1.1', '9.9.9.9']);
	assert.equal(out.networks[1].routes[0].metric, 50);
	assert.equal(cfg.networks[1].vlan, '20', 'the draft itself is not changed');
});

test('ports show what uses them; bonds in use cannot go', () => {
	const cfg = config();
	assert.deepEqual(portUsers(cfg, 'enp1s0'), [{ kind: 'network', name: 'LAN' }]);
	assert.deepEqual(portUsers(cfg, 'enp2s0'), [{ kind: 'bond', name: 'uplink' }]);
	assert.deepEqual(portUsers(cfg, 'enp4s0'), []);
	assert.deepEqual(bondUsers(cfg, 'uplink'), ['Speicher']);
	const fresh = blankBond(cfg);
	assert.equal(fresh.id, 'bond1');
	assert.deepEqual(
		bondCandidates(cfg, ports, fresh).map((p) => p.name),
		['enp1s0', 'enp4s0'],
	);
	assert.deepEqual(
		bondCandidates(cfg, ports, cfg.bonds[0]).map((p) => p.name),
		['enp1s0', 'enp2s0', 'enp3s0', 'enp4s0'],
	);
});

test('a new network takes a free port and a short unused ID', () => {
	const cfg = config();
	const n = blankNetwork(cfg, ports);
	assert.equal(n.parent, 'enp1s0');
	assert.equal(n.id, 'net1');
	assert.ok(/^[a-z][a-z0-9-]{0,9}$/.test(n.id));
	assert.equal(n.management, false);
	assert.equal(blankNetwork({ ...cfg, networks: [] }, ports).management, true);
});

test('live state comes from the NAS, drafts have none', () => {
	const cfg = config();
	const status = {
		live: {
			admin: { device: 'br0', up: true, addresses: ['192.168.1.20/24'] },
			storage: { device: 'tvstorage', up: true, addresses: [] },
		},
	};
	assert.equal(liveState(status, cfg.networks[0], true), 'up');
	assert.equal(liveState(status, cfg.networks[1], true), 'noAddress');
	assert.equal(
		liveState({ live: { admin: { up: false, addresses: [] } } }, cfg.networks[0], true),
		'down',
	);
	assert.equal(liveState({}, cfg.networks[0], true), 'unknown');
	assert.equal(liveState(status, cfg.networks[0], false), 'draft');
});

test('the network page has no single-network view and every text in both languages', () => {
	assert.ok(
		!existsSync(new URL('../src/routes/settings/network/LegacyNetwork.svelte', import.meta.url)),
	);
	const de = JSON.parse(readFileSync(new URL('../src/lib/i18n/de.json', import.meta.url), 'utf8'));
	const en = JSON.parse(readFileSync(new URL('../src/lib/i18n/en.json', import.meta.url), 'utf8'));
	for (const file of [
		'+page',
		'SystemCard',
		'NetworkCard',
		'InterfacesTab',
		'DiagnosticsTab',
		'RemoteTab',
		'NetworkEditor',
		'BondEditor',
		'DnsChips',
	]) {
		const source = readFileSync(
			new URL(`../src/routes/settings/network/${file}.svelte`, import.meta.url),
			'utf8',
		);
		for (const [, key] of source.matchAll(/\bt\('([a-zA-Z0-9_.-]+)'/g)) {
			assert.ok(key in de && key in en, `${file}: ${key}`);
		}
	}
	for (const key of [
		'netx.tab.overview',
		'netx.tab.interfaces',
		'netx.tab.diagnostics',
		'netx.tab.remote',
		'netx.live.up',
		'netx.live.down',
		'netx.live.noAddress',
		'netx.live.draft',
		'netcfg.bond.active-backup',
		'netcfg.bond.802.3ad',
		'netcfg.bond.balance-alb',
	]) {
		assert.ok(key in de && key in en, key);
	}
});
