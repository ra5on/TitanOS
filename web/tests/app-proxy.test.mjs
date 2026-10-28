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

test('local name, networks and target addresses follow the rules of the NAS', async () => {
	const { normalLocalName, validLocalName, validNetwork, validTargetHost, splitList } =
		await import('../src/lib/app-proxy.ts');
	assert.equal(normalLocalName(' Apps.local. '), 'apps');
	for (const name of ['', 'apps', 'home.lan', 'a.b.c']) assert.ok(validLocalName(name), name);
	for (const name of ['a b', 'local', 'a.b.c.d', '-x', 'a_b'])
		assert.ok(!validLocalName(name), name);
	for (const net of ['192.168.1.0/24', '10.0.0.1', 'fd7a:115c:a1e0::/48', '::1', '0.0.0.0/0'])
		assert.ok(validNetwork(net), net);
	for (const net of ['192.168.1.0/33', '300.1.1.1', 'nonsense', '10.0.0.0/8/1', 'fd00::/129', ''])
		assert.ok(!validNetwork(net), net);
	for (const host of ['192.168.1.30', 'fritz.box', 'Fritz.Box.', 'fd00::5'])
		assert.ok(validTargetHost(host), host);
	for (const host of ['', 'bad host', 'a_b.lan']) assert.ok(!validTargetHost(host), host);
	assert.deepEqual(splitList(' fotos, bilder\nimmich ;; '), ['fotos', 'bilder', 'immich']);
});

test('the route form round-trips and reports what the NAS would refuse', async () => {
	const { proxyForm, proxyBody, proxyProblems } = await import('../src/lib/app-proxy.ts');
	const stored = {
		enabled: true,
		name: 'immich',
		aliases: ['fotos'],
		domains: ['fotos.example.org'],
		short: true,
		port: 2283,
		scheme: /** @type {const} */ ('https'),
		verifyTLS: true,
		localOnly: true,
		networks: ['10.0.0.0/8'],
	};
	const form = proxyForm(stored);
	assert.equal(form.access, 'networks');
	assert.deepEqual(proxyBody(form, false), {
		...stored,
		localOnly: true,
		tailscale: false,
		tailscaleName: '',
	});
	// A new route is local only; plain HTTP has no certificate to check.
	assert.equal(proxyForm(null, 'x').access, 'local');
	assert.equal(proxyBody({ ...form, scheme: 'http' }, false).verifyTLS, false);
	assert.equal(proxyForm({ ...stored, networks: [], localOnly: false }).access, 'all');

	const others = [{ app: 'jellyfin', name: 'media', aliases: ['filme'] }];
	assert.deepEqual(proxyProblems(form, false, others, 'nas'), []);
	const keys = (f, target = false, label = 'nas') =>
		proxyProblems(f, target, others, label).map((p) => p.key);
	assert.deepEqual(keys({ ...form, aliases: 'filme' }), ['proxy.nameTaken']);
	assert.deepEqual(keys({ ...form, aliases: 'a_b' }), ['proxy.aliasInvalid']);
	assert.deepEqual(keys({ ...form, aliases: 'a,b,c,d,e,f,g,h,i' }), ['proxy.aliasTooMany']);
	assert.deepEqual(keys({ ...form, networks: '' }), ['proxy.networksEmpty']);
	assert.deepEqual(keys({ ...form, networks: '10.0.0.0/40' }), ['proxy.networksInvalid']);
	assert.deepEqual(keys({ ...form, port: 0 }), ['proxy.portInvalid']);
	assert.deepEqual(keys(form, false, 'fotos'), ['proxy.shortIsNas']);
	assert.deepEqual(keys({ ...form, host: '' }, true), ['proxy.hostInvalid']);
	assert.equal(proxyBody({ ...form, host: 'HA.lan.' }, true).host, 'ha.lan');
	assert.deepEqual(proxyBody({ ...form, domains: ' Fotos.Example.org., bilder.example.org' }, false).domains, ['fotos.example.org', 'bilder.example.org']);
	assert.deepEqual(keys({ ...form, domains: 'nas.local' }), ['proxy.domainInvalid']);
	assert.deepEqual(keys({ ...form, domains: 'nur-ein-teil' }), ['proxy.domainInvalid']);
	assert.deepEqual(keys({ ...form, domains: 'a.de,b.de,c.de,d.de,e.de,f.de,g.de,h.de,i.de' }), ['proxy.domainTooMany']);
});

test('a name in the tailnet is a label of its own and unique among devices', async () => {
	const { proxyForm, proxyBody, proxyProblems, tailnetName } =
		await import('../src/lib/app-proxy.ts');
	const form = {
		...proxyForm(null, 'immich'),
		port: 2283,
		tailscale: true,
		tailscaleName: ' Photos ',
	};
	assert.deepEqual(
		[proxyBody(form, false).tailscale, proxyBody(form, false).tailscaleName],
		[true, 'photos'],
	);
	// Off: no name is sent along.
	assert.equal(proxyBody({ ...form, tailscale: false }, false).tailscaleName, '');
	assert.equal(tailnetName({ name: 'immich' }), 'immich');
	assert.equal(tailnetName({ name: 'immich', tailscaleName: 'photos' }), 'photos');
	const others = [{ app: 'other', name: 'other', tailscale: true, tailscaleName: 'photos' }];
	assert.deepEqual(
		proxyProblems(form, false, others, 'nas').map((p) => p.key),
		['proxy.tailnetNameTaken'],
	);
	// The same device name is fine when the other route is not in the tailnet.
	assert.deepEqual(proxyProblems(form, false, [{ ...others[0], tailscale: false }], 'nas'), []);
	assert.deepEqual(
		proxyProblems({ ...form, tailscaleName: 'a.b' }, false, [], 'nas').map((p) => p.key),
		['proxy.tailnetNameInvalid'],
	);
	// A stored route comes back into the form.
	const back = proxyForm({
		enabled: true,
		name: 'immich',
		port: 2283,
		scheme: 'http',
		tailscale: true,
		tailscaleName: 'photos',
	});
	assert.deepEqual([back.tailscale, back.tailscaleName], [true, 'photos']);
});
