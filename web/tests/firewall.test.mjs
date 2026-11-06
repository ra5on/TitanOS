import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import {
	appPorts,
	assignNetwork,
	cellOf,
	cellState,
	cloneConfig,
	countdown,
	defaultCell,
	destinations,
	editCell,
	exceptions,
	isHosted,
	newZoneId,
	policyProblem,
	removeZone,
	routable,
	samePolicies,
	servicePorts,
	tabFrom,
	validPorts,
	validSource,
	zoneOfNetwork,
	zoneProblem,
} from '../src/lib/firewall.ts';
import { settingsAreas, settingSections, matchesSetting } from '../src/lib/settings-search.ts';
import { activityLabel } from '../src/lib/activity.ts';

const services = [
	{
		id: 'webui',
		ports: [{ protocol: 'tcp', ports: ['80', '443'] }],
		default: { home: true, vpn: true, internet: false },
	},
	{
		id: 'discovery',
		ports: [{ protocol: 'udp', ports: ['5353'] }],
		default: { home: true, vpn: false, internet: false },
	},
	{
		id: 'vpn',
		ports: [{ protocol: 'udp', ports: ['41641'] }],
		default: { home: true, vpn: true, internet: true },
	},
];

const builtin = (id) => ({ id, builtin: true, networks: [], sources: [] });
const config = () => ({
	enabled: false,
	zones: [builtin('home'), builtin('vpn'), builtin('internet'), builtin('docker')],
	cells: [],
	autoBlock: {
		enabled: false,
		attempts: 10,
		within: 5,
		minutes: 60,
		permanent: false,
		exemptLocal: true,
		exempt: [],
	},
});

test('ports of services and apps are readable', () => {
	assert.equal(
		servicePorts({
			id: 'smb',
			ports: [
				{ protocol: 'tcp', ports: ['139', '445'] },
				{ protocol: 'udp', ports: ['137-138'] },
			],
			default: {},
		}),
		'TCP 139, 445 · UDP 137-138',
	);
	assert.equal(
		servicePorts({ id: 'ping', ports: [{ protocol: 'icmp', ports: null }], default: {} }),
		'ICMP',
	);
	assert.equal(
		appPorts({
			key: 'a',
			name: 'a',
			ports: [
				{ port: 8096, protocol: 'tcp' },
				{ port: 53, protocol: 'tcp' },
				{ port: 53, protocol: 'udp' },
				{ port: 53, protocol: 'tcp' },
			],
		}),
		'TCP 53, 8096 · UDP 53',
	);
});

test('default cells are those of the backend', () => {
	const cfg = config();
	const homeNas = defaultCell(cfg, services, 'home', 'nas');
	assert.deepEqual(
		homeNas.policies.map((p) => p.service),
		['webui', 'discovery', 'vpn'],
	);
	assert.equal(homeNas.default, 'deny');
	assert.deepEqual(
		defaultCell(cfg, services, 'internet', 'nas').policies.map((p) => p.service),
		['vpn'],
	);
	const dockerNas = defaultCell(cfg, services, 'docker', 'nas');
	assert.deepEqual(dockerNas.policies, [
		{ action: 'allow', protocol: 'any', service: 'apps' },
		{ action: 'allow', protocol: 'any', ports: '53', comment: 'DNS' },
	]);
	assert.equal(defaultCell(cfg, services, 'home', 'apps').default, 'allow');
	assert.equal(defaultCell(cfg, services, 'internet', 'apps').default, 'deny');
	assert.equal(defaultCell(cfg, services, 'docker', 'internet').default, 'allow');
	assert.equal(defaultCell(cfg, services, 'docker', 'docker').default, 'deny');
});

test('only zones with networks behind the NAS reach other zones', () => {
	const cfg = config();
	cfg.zones.push({ id: 'ziot', name: 'IoT', networks: ['net:iot'], sources: [] });
	cfg.zones.push({ id: 'zmedia', name: 'Media', networks: ['docker:media'], sources: [] });
	assert.ok(
		isHosted(cfg, 'docker') &&
			isHosted(cfg, 'zmedia') &&
			!isHosted(cfg, 'ziot') &&
			!isHosted(cfg, 'home'),
	);
	assert.ok(
		!routable(cfg, 'home', 'internet') &&
			routable(cfg, 'home', 'nas') &&
			routable(cfg, 'zmedia', 'home'),
	);
	assert.deepEqual(destinations(cfg, 'home'), ['nas', 'apps']);
	assert.equal(destinations(cfg, 'docker').length, 2 + cfg.zones.length);
	// Into another network behind the NAS: blocked, like Docker isolates them.
	assert.equal(defaultCell(cfg, services, 'docker', 'zmedia').default, 'deny');
	assert.equal(defaultCell(cfg, services, 'docker', 'ziot').default, 'allow');
});

test('cell states and editing', () => {
	const cfg = config();
	const cell = editCell(cfg, services, 'home', 'nas');
	assert.equal(cfg.cells.length, 1);
	assert.equal(cellState(cell), 'partial');
	assert.equal(exceptions(cell), 3);
	cell.policies = [];
	assert.equal(cellState(cellOf(cfg, services, 'home', 'nas')), 'deny');
	cell.default = 'allow';
	assert.equal(cellState(cell), 'allow');
	cell.policies.push({ action: 'deny', protocol: 'tcp', ports: '22', disabled: true });
	assert.equal(cellState(cell), 'allow');
	cell.policies[0].disabled = undefined;
	assert.equal(cellState(cell), 'partial');
});

test('networks go into one zone at a time', () => {
	const cfg = config();
	cfg.zones.push(
		{ id: 'za', name: 'A', networks: [], sources: [] },
		{ id: 'zb', name: 'B', networks: [], sources: [] },
	);
	assert.equal(zoneOfNetwork(cfg, 'docker:web'), 'docker');
	assert.equal(zoneOfNetwork(cfg, 'net:lan'), '');
	assignNetwork(cfg, 'docker:web', 'za');
	assignNetwork(cfg, 'docker:web', 'zb');
	assert.deepEqual(
		cfg.zones.map((z) => z.networks.length),
		[0, 0, 0, 0, 0, 1],
	);
	assert.equal(zoneOfNetwork(cfg, 'docker:web'), 'zb');
	assignNetwork(cfg, 'docker:web', '');
	assert.equal(zoneOfNetwork(cfg, 'docker:web'), 'docker');
	const id = newZoneId(cfg);
	assert.match(id, /^z[a-z0-9]{1,14}$/);
	editCell(cfg, services, 'zb', 'nas');
	editCell(cfg, services, 'docker', 'zb');
	removeZone(cfg, 'zb');
	removeZone(cfg, 'home');
	assert.equal(cfg.zones.length, 5);
	assert.equal(cfg.cells.length, 0);
});

test('changes are detected without the automatic blocking, key order or missing default cells', () => {
	const a = config();
	const b = cloneConfig(a);
	b.autoBlock.enabled = true;
	editCell(b, services, 'home', 'nas');
	assert.ok(samePolicies(a, b, services));
	b.cells[0].policies.reverse();
	assert.ok(!samePolicies(a, b, services));
	const c = cloneConfig(a);
	editCell(c, services, 'docker', 'internet').default = 'deny';
	assert.ok(!samePolicies(a, c, services));
	const d = cloneConfig(a);
	d.zones.push({ id: 'zx', name: 'X', networks: [], sources: [] });
	assert.ok(!samePolicies(a, d, services));
});

test('policies and zones are checked like the backend does', () => {
	for (const ok of ['22', '1-65535', '8000-8100']) assert.ok(validPorts(ok), ok);
	for (const bad of ['0', '70000', '90-80', '22,23', 'ssh']) assert.ok(!validPorts(bad), bad);
	for (const ok of ['192.168.1.5', '10.0.0.0/8', '2001:db8::/32', 'fd7a:115c:a1e0::5'])
		assert.ok(validSource(ok), ok);
	for (const bad of ['example.org', '300.1.1.1', '10.0.0.0/33', '2001:db8::/129', '1.2.3.4/8/1'])
		assert.ok(!validSource(bad), bad);
	assert.equal(policyProblem({ action: 'allow', protocol: 'tcp', ports: '22' }), '');
	assert.equal(
		policyProblem({ action: 'allow', protocol: 'tcp', ports: 'x' }),
		'firewall.rulePorts',
	);
	assert.equal(policyProblem({ action: 'allow', protocol: 'any', service: 'ssh', ports: 'x' }), '');
	assert.equal(
		policyProblem({ action: 'deny', protocol: 'any', source: 'nas' }),
		'firewall.ruleSource',
	);
	assert.equal(
		policyProblem({ action: 'deny', protocol: 'any', destination: 'lan' }),
		'firewall.policyDestination',
	);
	assert.equal(
		policyProblem({ action: 'deny', protocol: 'any', comment: 'say "hi"' }),
		'firewall.ruleComment',
	);
	assert.equal(
		zoneProblem({ id: 'z1', name: ' ', networks: [], sources: [] }),
		'firewall.zoneName',
	);
	assert.equal(
		zoneProblem({ id: 'z1', name: 'IoT', networks: [], sources: ['x'] }),
		'firewall.ruleSource',
	);
	assert.equal(zoneProblem({ id: 'z1', name: 'IoT', networks: [], sources: ['10.0.0.0/8'] }), '');
	assert.equal(countdown(119), '1:59');
	assert.equal(countdown(-3), '0:00');
});

test('tabs follow ?tab= and the anchors of the settings search', () => {
	assert.equal(tabFrom('networks'), 'networks');
	assert.equal(tabFrom('nope'), 'zones');
	assert.equal(tabFrom(null, '#firewall-autoblock'), 'autoblock');
	assert.equal(tabFrom(null, '#firewall-rules'), 'policies');
	assert.equal(tabFrom(null, '#firewall-recent'), 'recent');
});

test('the firewall page is found and named in both languages', () => {
	assert.ok(
		settingsAreas.some((area) => area.id === 'firewall' && area.admin && area.group === 'network'),
	);
	assert.ok(matchesSetting('nftables', 'Firewall', 'firewall'));
	for (const tab of ['zones', 'networks', 'policies', 'autoblock', 'recent'])
		assert.ok(
			settingSections.firewall.some((s) => s.href.startsWith(`/settings/firewall?tab=${tab}#`)),
			tab,
		);
	assert.deepEqual(activityLabel('firewall_block'), { kind: 'named', key: 'firewallBlock' });
	const dir = new URL('../src/routes/settings/firewall/', import.meta.url);
	const used = new Set();
	for (const file of readdirSync(dir).filter((f) => f.endsWith('.svelte'))) {
		const text = readFileSync(new URL(file, dir), 'utf8');
		for (const m of text.matchAll(/t\(\s*'((?:firewall|common|ui|nav)\.[A-Za-z.]+)'/g))
			used.add(m[1]);
	}
	for (const m of readFileSync(new URL('../src/lib/firewall.ts', import.meta.url), 'utf8').matchAll(
		/'(firewall\.[A-Za-z.]+)'/g,
	))
		used.add(m[1]);
	for (const id of [
		'webui',
		'apps',
		'smb',
		'nfs',
		'ssh',
		'discovery',
		'ping',
		'ups',
		'backuptarget',
		'vpn',
	])
		for (const prefix of ['firewall.svc.', 'firewall.svcText.']) used.add(prefix + id);
	for (const zone of ['home', 'vpn', 'internet', 'docker'])
		used.add(`firewall.zone.${zone}`).add(`firewall.zoneText.${zone}`);
	for (const tab of ['zones', 'networks', 'policies', 'autoblock', 'recent'])
		used.add(`firewall.tab.${tab}`);
	for (const state of ['allow', 'deny', 'partial']) used.add(`firewall.cell.${state}`);
	for (const kind of ['network', 'docker', 'vm', 'dockerlan'])
		used.add(`firewall.kind.${kind}`).add(`firewall.group.${kind}`);
	for (const source of ['web', 'ssh', 'smb']) used.add(`firewall.source.${source}`);
	for (const code of [
		'invalid_firewall',
		'firewall_excludes_you',
		'firewall_pending',
		'firewall_not_pending',
		'firewall_not_blocked',
		'firewall_failed',
	])
		used.add(`error.${code}`);
	for (const kind of ['firewall_blocked', 'firewall_reverted', 'firewall_failed'])
		for (const part of ['title', 'text']) used.add(`notify.kind.${kind}.${part}`);
	for (const action of [
		'firewallBlock',
		'firewallReverted',
		'firewallChange',
		'firewallConfirm',
		'firewallRevert',
		'firewallAutoBlock',
		'firewallUnblock',
	])
		used.add(`activity.do.${action}`);
	for (const lang of ['de', 'en']) {
		const texts = JSON.parse(
			readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8'),
		);
		for (const key of used) assert.ok(texts[key], `${lang}: ${key}`);
	}
});
