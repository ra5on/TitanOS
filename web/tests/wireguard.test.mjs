import { test } from 'node:test';
import assert from 'node:assert/strict';
import { configFileName, connected, validEndpoint } from '../src/lib/wireguard.ts';

test('a device is connected while its handshakes are fresh', () => {
	const now = Date.parse('2026-10-28T20:00:00Z');
	assert.ok(connected({ lastHandshake: '2026-10-28T19:58:30Z' }, now));
	assert.ok(!connected({ lastHandshake: '2026-10-28T19:50:00Z' }, now));
	assert.ok(!connected({}, now));
});

test('configuration files get names wg-quick accepts', () => {
	assert.equal(configFileName('iPhone von Alex'), 'iPhone-von-Alex.conf');
	assert.equal(configFileName('Ärger/../x'), 'Arger-..-x.conf');
	assert.equal(configFileName('???'), 'titanos.conf');
});

test('endpoints are names or addresses without port', () => {
	for (const ok of ['', 'nas.example.org', '203.0.113.5', '2001:db8::1'])
		assert.ok(validEndpoint(ok), ok);
	for (const bad of ['nas example', 'nas.example.org:51820x', '.nas', 'a..b', 'x;rm'])
		assert.ok(!validEndpoint(bad), bad);
});
