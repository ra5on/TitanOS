import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { addNetworks, parseNetworks, sameNetworks } from '../src/lib/ssh-networks.ts';

test('the network list is split, de-duplicated and extended by presets', () => {
	assert.deepEqual(
		parseNetworks(' 192.168.1.0/24\n\n10.0.0.1, 192.168.1.0/24 ;fd7a:115c:a1e0::/48 '),
		['192.168.1.0/24', '10.0.0.1', 'fd7a:115c:a1e0::/48'],
	);
	assert.deepEqual(parseNetworks(''), []);
	assert.equal(
		addNetworks('192.168.1.0/24', ['100.64.0.0/10', '192.168.1.0/24']),
		'192.168.1.0/24\n100.64.0.0/10',
	);
	assert.equal(addNetworks('', ['100.64.0.0/10']), '100.64.0.0/10');
	assert.ok(sameNetworks(['a', 'b'], ['a', 'b']));
	assert.ok(!sameNetworks(['a'], ['a', 'b']));
});

test('SSH login entries of the activity log are named in both languages', () => {
	for (const lang of ['de', 'en']) {
		const texts = JSON.parse(
			readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8'),
		);
		for (const key of [
			'activity.do.sshLogin',
			'activity.do.sshLoginBlocked',
			'activity.via.ssh',
			'access.sshNetworks',
			'error.ssh_network_excludes_you',
			'error.invalid_ssh_network',
		]) {
			assert.ok(texts[key], `${lang}: ${key}`);
		}
	}
});
