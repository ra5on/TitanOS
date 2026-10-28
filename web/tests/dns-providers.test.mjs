import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	fieldsFor,
	missingFields,
	parseNames,
	providerForm,
	providerPayload,
	providerTypes,
	sameAccount,
	validHostName,
} from '../src/lib/dns-providers.ts';

const de = JSON.parse(readFileSync(new URL('../src/lib/i18n/de.json', import.meta.url)));
const en = JSON.parse(readFileSync(new URL('../src/lib/i18n/en.json', import.meta.url)));

test('every provider has its texts in both languages', () => {
	for (const type of providerTypes('address')) {
		for (const key of [`dnsProvider.type.${type}`, `dnsProvider.help.${type}`]) {
			assert.ok(de[key] && en[key], key);
		}
		for (const purpose of ['challenge', 'address']) {
			for (const field of fieldsFor(type, purpose)) {
				assert.ok(de[`dnsProvider.field.${field.label}`] && en[`dnsProvider.field.${field.label}`], field.label);
			}
		}
	}
	assert.ok(!providerTypes('challenge').includes('dyndns2'));
	assert.ok(providerTypes('challenge').includes('ipv64'));
});

test('secrets are kept only for the same account and sent only when typed', () => {
	const stored = { type: 'netcup', customer: '4711' };
	const set = { token: true, password: true };
	assert.deepEqual(missingFields({ type: 'netcup', customer: '4711', token: '', password: '' }, stored, set, []), []);
	assert.deepEqual(missingFields({ type: 'netcup', customer: '9999', token: '', password: '' }, stored, set, []), ['token', 'password']);
	assert.deepEqual(missingFields({ type: 'netcup', customer: '4711', token: '', password: '' }, stored, set, ['password']), ['password']);
	assert.ok(sameAccount({ type: 'inwx', user: 'a' }, { type: 'inwx', user: 'a' }));
	assert.deepEqual(providerPayload({ type: 'cloudflare', token: '', user: 'leftover' }), { type: 'cloudflare' });
	assert.deepEqual(providerPayload({ type: 'cloudflare', token: ' t ' }), { type: 'cloudflare', token: 't' });
	assert.equal(providerForm({ type: 'rfc2136', secret: 'x' }).algorithm, 'hmac-sha256');
	assert.equal(providerForm({ type: 'rfc2136', secret: 'x' }).secret, '');
});

test('IPv64 needs the API key for the challenge, either key for DynDNS', () => {
	const none = {};
	assert.deepEqual(missingFields({ type: 'ipv64' }, { type: '' }, none, [], 'challenge'), ['token']);
	assert.deepEqual(missingFields({ type: 'ipv64', updateToken: 'u' }, { type: '' }, none, [], 'address'), []);
	assert.deepEqual(missingFields({ type: 'ipv64' }, { type: '' }, none, [], 'address'), ['token', 'updateToken']);
	assert.deepEqual(missingFields({ type: 'ipv64' }, { type: 'ipv64' }, { updateToken: true }, [], 'address'), []);
	// The update token and the prefix belong to DynDNS only.
	assert.deepEqual(providerPayload({ type: 'ipv64', token: 'k', updateToken: 'u', ipv6Prefix: 'auto' }, 'challenge'), { type: 'ipv64', token: 'k' });
	assert.deepEqual(providerPayload({ type: 'ipv64', updateToken: 'u', ipv6Prefix: 'auto' }, 'address'), { type: 'ipv64', updateToken: 'u', ipv6Prefix: 'auto' });
});

test('names, wildcards and records', () => {
	assert.deepEqual(parseNames(' NAS.Example.org. \n*.example.org, nas.example.org'), ['nas.example.org', '*.example.org']);
	assert.ok(validHostName('nas.example.org'));
	assert.ok(!validHostName('*.example.org'));
	assert.ok(validHostName('*.example.org', true));
	assert.ok(!validHostName('nas.local'));
	assert.ok(!validHostName('nas'));
});
