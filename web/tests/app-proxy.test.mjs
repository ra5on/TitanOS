import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	normalDomain,
	proxyAddresses,
	suggestedProxyName,
	suggestedProxyPort,
	validProxyDomain,
	validProxyName,
} from '../src/lib/app-proxy.ts';
import { dockerGroups, stackPrimary } from '../src/lib/docker-layout.ts';

test('HTTPS names follow the rules of the NAS', () => {
	for (const name of ['immich', 'a', 'media-2', 'x'.repeat(63)])
		assert.ok(validProxyName(name), name);
	for (const name of ['', 'Immich', '-a', 'a-', 'a.b', 'a_b', 'x'.repeat(64), 'ümlaut'])
		assert.ok(!validProxyName(name), name);
	for (const domain of ['', 'home.example.org', 'Home.Example.org.', 'nas-1.lan.example'])
		assert.ok(validProxyDomain(domain), domain);
	for (const domain of ['example', 'nas.local', 'a_b.org', '-a.org', 'a..org'])
		assert.ok(!validProxyDomain(domain), domain);
	assert.equal(normalDomain(' Home.Example.ORG. '), 'home.example.org');
});

test('suggested name and port of an app', () => {
	assert.equal(suggestedProxyName('immich'), 'immich');
	assert.equal(suggestedProxyName('open_webui'), 'open-webui');
	assert.equal(suggestedProxyName('_x_'), 'x');
	const db = { webui: '', ports: [{ host: 5432, protocol: 'tcp' }] };
	const web = { webui: '2283/photos', ports: [{ host: 2283, protocol: 'tcp' }] };
	assert.equal(suggestedProxyPort([db, web]), 2283);
	assert.equal(suggestedProxyPort([{ webui: 'https://[host]:8443/', ports: [] }]), 8443);
	assert.equal(
		suggestedProxyPort([
			{
				webui: 'https://vault.example.org',
				ports: [
					{ host: 53, protocol: 'udp' },
					{ host: 8080, protocol: 'tcp' },
				],
			},
		]),
		8080,
	);
	assert.equal(suggestedProxyPort([{ webui: '', ports: [] }]), 0);
});

test('addresses under every suffix, none for an invalid name', () => {
	assert.deepEqual(proxyAddresses('immich', ['nas.local', 'home.example.org']), [
		'https://immich.nas.local',
		'https://immich.home.example.org',
	]);
	assert.deepEqual(proxyAddresses('Bad Name', ['nas.local']), []);
});

test('a stack whose only link is the HTTPS name still has a main service', () => {
	const base = {
		image: 'x',
		state: 'running',
		since: '',
		ports: [],
		managed: true,
		cpuPercent: 0,
		memoryBytes: 0,
		cpuHistory: [],
		webui: '',
		publicUrl: '',
		icon: '',
		updateAvailable: false,
	};
	const members = [
		{ ...base, id: 'a', name: 'photos-db-1', app: 'photos', service: 'db' },
		{
			...base,
			id: 'b',
			name: 'photos-web-1',
			app: 'photos',
			service: 'web',
			proxyUrl: 'https://photos.nas.local',
		},
	];
	const [group] = dockerGroups(members);
	assert.equal(stackPrimary(group, {}).service, 'web');
});
